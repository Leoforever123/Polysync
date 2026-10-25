#!/usr/bin/env python3
"""Real dpkg/pacman lifecycle tests in disposable roots, never the host database.

Run as a regular Linux user with unshare -Ur support. Supply two independently
built package directories containing application versions 0.4.0 and 0.4.1.
"""
import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import tarfile
import tempfile
import time
import urllib.error
import urllib.request


def run(*args, **kwargs):
    return subprocess.run([str(a) for a in args], check=True, **kwargs)


def port():
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        return listener.getsockname()[1]


def stop(process):
    if process.poll() is None:
        process.send_signal(signal.SIGTERM)
        try:
            process.wait(timeout=15)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait()
            raise AssertionError("Application did not stop after SIGTERM")
    assert process.returncode == 0, f"Application exit: {process.returncode}"


def start(binary, data, cache, ui, tcp, log):
    env = os.environ.copy()
    env["XDG_CACHE_HOME"] = str(cache)
    process = subprocess.Popen([str(binary), "-data-dir", str(data), "-ui", f"127.0.0.1:{ui}",
                                "-listen", f"127.0.0.1:{tcp}", "-open=false", "-tray=false"],
                               env=env, stdout=log, stderr=log)
    local_http = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    last_error = None
    try:
        for _ in range(100):
            if process.poll() is not None:
                raise AssertionError(f"Application exited during startup: {process.returncode}")
            try:
                with local_http.open(f"http://127.0.0.1:{ui}/api/status", timeout=.5) as response:
                    assert response.status == 200
                    return process
            except (urllib.error.URLError, TimeoutError) as error:
                last_error = error
                time.sleep(.05)
        raise AssertionError(f"Installed app did not become ready: {last_error}; {Path(log.name).read_text()}")
    except BaseException:
        stop(process)
        raise


def check_released(ui, tcp):
    for number in (ui, tcp):
        with socket.socket() as listener:
            # Match Go's listener behavior: an HTTP connection in TIME_WAIT is
            # not a live listener and must not prevent a legitimate restart.
            listener.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
            listener.bind(("127.0.0.1", number))


def verify_payload(root):
    expected = ["usr/bin/polysync", "usr/bin/polysync-open", "usr/lib/systemd/user/polysync.service",
                "usr/share/applications/polysync.desktop", "usr/share/icons/hicolor/scalable/apps/polysync.svg",
                "usr/share/doc/polysync/copyright", "usr/share/doc/polysync/README.md",
                "usr/share/doc/polysync/THIRD_PARTY_NOTICES.txt"]
    for name in expected:
        assert (root / name).is_file(), f"Package missing {name}"
    for name in expected[:2]:
        assert os.access(root / name, os.X_OK), f"Not executable: {name}"
    assert b"\r" not in (root / "usr/bin/polysync-open").read_bytes()
    assert not (root / "etc/systemd/system/polysync.service").exists(), "System service unexpectedly installed"
    assert not list(root.glob("**/default.target.wants/polysync.service")), "Package auto-enabled service"
    run("desktop-file-validate", root / "usr/share/applications/polysync.desktop")
    # WSL has no running user manager. Validate the real unit using its isolated
    # installation path; runtime stop/restart is tested against the installed app.
    unit = root / "verify/polysync.service"
    unit.parent.mkdir(exist_ok=True)
    unit.write_text((root / expected[2]).read_text().replace("/usr/bin/polysync", str(root / "usr/bin/polysync")))
    run("systemd-analyze", "--user", "verify", unit)


def verify_artifacts(directory):
    for line in (directory / "SHA256SUMS-linux-0.4.0-1-all.txt").read_text().splitlines():
        expected, name = line.split()
        assert hashlib.sha256((directory / name).read_bytes()).hexdigest() == expected, f"Checksum: {name}"
    for arch, machine in [("amd64", 62), ("arm64", 183)]:
        package = directory / f"polysync_0.4.0-1_{arch}.deb"
        assert run("dpkg-deb", "-f", package, "Architecture", capture_output=True, text=True).stdout.strip() == arch
        data = run("dpkg-deb", "--fsys-tarfile", package, capture_output=True).stdout
        with tarfile.open(fileobj=io.BytesIO(data)) as contents:
            for member in contents.getmembers():
                assert member.name in (".", "./usr") or member.name.startswith("./usr/"), member.name
                assert member.uid == member.gid == 0, member.name
            binary = contents.extractfile("./usr/bin/polysync").read()
            assert int.from_bytes(binary[18:20], "little") == machine
            assert hashlib.sha256(binary).digest() == hashlib.sha256((directory / f"polysync-linux-{arch}").read_bytes()).digest(), "Packaged binary differs from the final build"
    arch_package = directory / "polysync-0.4.0-1-x86_64.pkg.tar.zst"
    binary = run("bsdtar", "-xOf", arch_package, "usr/bin/polysync", capture_output=True).stdout
    assert hashlib.sha256(binary).digest() == hashlib.sha256((directory / "polysync-linux-amd64").read_bytes()).digest()
    with tempfile.TemporaryDirectory(prefix="polysync-arch-recipe-") as temporary:
        recipe = Path(temporary)
        with tarfile.open(directory / "polysync-0.4.0-1-arch-build.tar.gz") as bundle:
            bundle.extractall(recipe, filter="data")
        run("makepkg", "--verifysource", cwd=recipe)
        source = recipe / "polysync-0.4.0-linux-amd64.tar.gz"
        with source.open("ab") as stream:
            stream.write(b"tampered")
        rejected = subprocess.run(["makepkg", "--verifysource"], cwd=recipe, capture_output=True)
        assert rejected.returncode != 0, "Arch recipe accepted a corrupted payload"
    print("PASS artifacts: manifests, both DEB architectures/content/ownership, Arch binary, offline recipe integrity and tamper rejection")


def lifecycle(kind, old, new, root):
    root.mkdir()
    db = root / ("var/lib/dpkg" if kind == "deb" else "var/lib/pacman")
    db.mkdir(parents=True)
    if kind == "deb":
        (db / "status").touch()
        def install(package):
            run("unshare", "-Ur", "dpkg", f"--root={root}", f"--log={root}/dpkg.log", "-i", package)
        def remove():
            run("unshare", "-Ur", "dpkg", f"--root={root}", f"--log={root}/dpkg.log", "--purge", "polysync")
        def version():
            return run("dpkg-query", f"--admindir={db}", "-W", "-f=${Version}", "polysync", capture_output=True, text=True).stdout
    else:
        cache = root / "var/cache/pacman/pkg"
        cache.mkdir(parents=True)
        hooks = root / "empty-hooks"
        hooks.mkdir()
        conf = root / "pacman.conf"
        conf.write_text("[options]\nArchitecture = x86_64\nSigLevel = Required DatabaseOptional\nLocalFileSigLevel = Optional\n")
        base = ["unshare", "-Ur", "pacman", "--root", str(root), "--dbpath", str(db), "--config", str(conf),
                "--cachedir", str(cache), "--logfile", str(root / "pacman.log"), "--hookdir", str(hooks), "--noconfirm"]
        def install(package):
            run(*base, "-U", package)
        def remove():
            run(*base, "-R", "polysync")
        def version():
            return run(*base, "-Q", "polysync", capture_output=True, text=True).stdout.split()[1]
    install(old)
    verify_payload(root)
    assert version().strip() == "0.4.0-1"
    binary = root / "usr/bin/polysync"
    assert run(binary, "-version", capture_output=True, text=True).stdout.strip() == "PolySync 0.4.0"
    old_hash = hashlib.sha256(binary.read_bytes()).hexdigest()
    data, cache = root / "personal/config/Polysync", root / "personal/cache"
    ui, tcp = port(), port()
    with (root / "app.log").open("wb") as log:
        process = start(binary, data, cache, ui, tcp, log)
        stop(process)
        check_released(ui, tcp)
        config = data / "config.json"
        before = json.loads(config.read_text())
        assert before["identityPrivateKey"] and before["deviceId"]
        user_folder = root / "personal/files"
        user_folder.mkdir()
        (user_folder / "keep.txt").write_text("must survive upgrade and uninstall")
        before["pairedDevices"] = [{"id": "fixture-peer", "name": "Packaging test", "publicKey": before["identityPublicKey"]}]
        before["shares"] = [{"id": "fixture-share", "name": "Local test", "path": str(user_folder), "peerDeviceId": "fixture-peer", "state": "active", "autoSync": False, "intervalSeconds": 30}]
        config.write_text(json.dumps(before))
        history = data / "history/fixture-share/old.txt"
        history.parent.mkdir(parents=True)
        history.write_text("historical data")
        # Let the OLD app persist its normal JSON defaults before comparing
        # upgrade results; omitted fixture fields are not a migration failure.
        process = start(binary, data, cache, ui, tcp, log)
        stop(process)
        check_released(ui, tcp)
        before = json.loads(config.read_text())
        install(new)
        assert version().strip() == "0.4.1-1"
        assert hashlib.sha256(binary.read_bytes()).hexdigest() != old_hash
        assert run(binary, "-version", capture_output=True, text=True).stdout.strip() == "PolySync 0.4.1"
        process = start(binary, data, cache, ui, tcp, log)
        stop(process)
        check_released(ui, tcp)
        after = json.loads(config.read_text())
        for key in ["deviceId", "identityPrivateKey", "identityPublicKey", "pairedDevices", "shares"]:
            assert after[key] == before[key], f"Upgrade changed {key}"
        remove()
        assert not binary.exists()
        assert not (root / "usr/lib/systemd/user/polysync.service").exists()
        assert not (root / "usr/share/applications/polysync.desktop").exists()
        assert json.loads(config.read_text())["identityPrivateKey"] == before["identityPrivateKey"]
        assert history.read_text() == "historical data"
        assert (user_folder / "keep.txt").read_text() == "must survive upgrade and uninstall"
    print(f"PASS {kind}: install, run, upgrade, restart/port release, identity/pairing/share retention, uninstall preserves files")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--old-dir", type=Path, required=True)
    parser.add_argument("--new-dir", type=Path, required=True)
    args = parser.parse_args()
    verify_artifacts(args.old_dir.resolve())
    run("unshare", "-Ur", "true")
    with tempfile.TemporaryDirectory(prefix="polysync-package-test-") as temporary:
        for kind, suffix in [("deb", "_{version}-1_amd64.deb"), ("arch", "-{version}-1-x86_64.pkg.tar.zst")]:
            old = args.old_dir.resolve() / ("polysync" + suffix.format(version="0.4.0"))
            new = args.new_dir.resolve() / ("polysync" + suffix.format(version="0.4.1"))
            lifecycle(kind, old, new, Path(temporary) / kind)


if __name__ == "__main__":
    main()
