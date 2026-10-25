#!/usr/bin/env python3
"""Package existing, versioned static Go binaries without installing anything.

Ubuntu/Debian: dpkg-deb. Arch: makepkg (run as a regular user).
The Arch source bundle contains its local payload and can be built offline.
"""
import argparse
import gzip
import hashlib
import os
from pathlib import Path
import platform
import re
import shutil
import struct
import subprocess
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parents[1]
PACKAGING = ROOT / "packaging/linux"


def run(*args, **kwargs):
    return subprocess.run([str(a) for a in args], check=True, **kwargs)


def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def version(value):
    if not re.fullmatch(r"(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)", value):
        raise argparse.ArgumentTypeError("version must be major.minor.patch")
    return value


def check_binary(binary, arch, expected_version):
    with binary.open("rb") as stream:
        header = stream.read(64)
        if len(header) != 64 or header[:6] != b"\x7fELF\x02\x01":
            raise ValueError(f"Not a 64-bit little-endian ELF binary: {binary}")
        if struct.unpack_from("<H", header, 18)[0] != {"amd64": 62, "arm64": 183}[arch]:
            raise ValueError(f"Binary architecture does not match {arch}: {binary}")
        # No interpreter means the package does not depend on a host glibc.
        offset = struct.unpack_from("<Q", header, 32)[0]
        size, count = struct.unpack_from("<HH", header, 54)
        for i in range(count):
            stream.seek(offset + i * size)
            if struct.unpack("<I", stream.read(4))[0] == 3:  # PT_INTERP
                raise ValueError("Build Linux binaries with CGO_ENABLED=0 before packaging")
    # -trimpath strips linker flags from Go build info. The build scripts record
    # the supplied application version and the exact output hash instead.
    info = Path(str(binary) + ".build-info").read_text(encoding="utf-8").split()
    if info != [expected_version, digest(binary)]:
        raise ValueError(f"Binary/version mismatch; rebuild version {expected_version}: {binary}")


def copy(source, destination, executable=False, text=False):
    destination.parent.mkdir(parents=True, exist_ok=True)
    if text:
        destination.write_bytes(source.read_bytes().replace(b"\r\n", b"\n"))
    else:
        shutil.copyfile(source, destination)
    destination.chmod(0o755 if executable else 0o644)


def payload(directory, binary):
    copy(binary, directory / "usr/bin/polysync", True)
    copy(PACKAGING / "polysync-open", directory / "usr/bin/polysync-open", True, text=True)
    copy(PACKAGING / "polysync.service", directory / "usr/lib/systemd/user/polysync.service")
    copy(PACKAGING / "polysync.desktop", directory / "usr/share/applications/polysync.desktop")
    copy(ROOT / "site/favicon.svg", directory / "usr/share/icons/hicolor/scalable/apps/polysync.svg")
    copy(PACKAGING / "copyright", directory / "usr/share/doc/polysync/copyright")
    copy(ROOT / "THIRD_PARTY_NOTICES.txt", directory / "usr/share/doc/polysync/THIRD_PARTY_NOTICES.txt")
    copy(ROOT / "docs/linux-installation.md", directory / "usr/share/doc/polysync/README.md")


def archive(output, directory, names):
    # Stable ownership and timestamps; no developer cache paths in the payload.
    epoch = int(os.environ.get("SOURCE_DATE_EPOCH", "0"))
    with output.open("wb") as raw, gzip.GzipFile(fileobj=raw, mode="wb", mtime=epoch, filename="") as compressed:
        with tarfile.open(fileobj=compressed, mode="w") as tar:
            def normalize(info):
                info.uid = info.gid = 0
                info.uname = info.gname = "root"
                info.mtime = epoch
                return info
            for name in names:
                tar.add(directory / name, arcname=name, filter=normalize)


def deb(output, binary, arch, release, revision, work):
    stage = work / f"deb-{arch}"
    payload(stage, binary)
    control_dir = stage / "DEBIAN"
    control_dir.mkdir()
    size = sum(p.stat().st_size for p in stage.rglob("*") if p.is_file())
    (control_dir / "control").write_text(
        f"Package: polysync\nVersion: {release}-{revision}\nArchitecture: {arch}\n"
        "Maintainer: PolySync maintainers <noreply@github.com>\n"
        "Section: net\nPriority: optional\n"
        f"Installed-Size: {(size + 1023) // 1024}\n"
        "Homepage: https://github.com/Leoforever123/Polysync\n"
        "Recommends: systemd, xdg-utils, curl\nSuggests: zenity | kdialog\n"
        "Description: Personal LAN file synchronization and transfer\n"
        " Encrypted device pairing, folder synchronization and direct file transfer.\n"
        " Includes an optional systemd user service and a local browser console.\n",
        encoding="utf-8")
    sums = [f"{hashlib.md5(p.read_bytes()).hexdigest()}  {p.relative_to(stage)}\n"
            for p in sorted((stage / "usr").rglob("*")) if p.is_file()]
    (control_dir / "md5sums").write_text("".join(sums), encoding="utf-8")
    target = output / f"polysync_{release}-{revision}_{arch}.deb"
    run("dpkg-deb", "--root-owner-group", "-Zxz", "--build", stage, target)
    return [target]


def arch_package(output, binary, release, revision, work):
    stage = work / "arch-payload"
    payload(stage, binary)
    recipe = work / "arch-recipe"
    recipe.mkdir()
    source = f"polysync-{release}-linux-amd64.tar.gz"
    archive(recipe / source, stage, ["usr"])
    template = (PACKAGING / "PKGBUILD.in").read_text(encoding="utf-8")
    for name, value in {"VERSION": release, "REVISION": str(revision), "SOURCE": source,
                        "SHA256": digest(recipe / source)}.items():
        template = template.replace(f"@{name}@", value)
    (recipe / "PKGBUILD").write_text(template, encoding="utf-8")
    env = os.environ.copy()
    env["PKGDEST"] = str(output)
    env["SRCDEST"] = str(recipe)
    env["BUILDDIR"] = str(work / "makepkg-build")
    env["PKGEXT"] = ".pkg.tar.zst"
    run("makepkg", "--force", "--cleanbuild", "--noconfirm", cwd=recipe, env=env)
    result = run("makepkg", "--printsrcinfo", cwd=recipe, env=env, capture_output=True, text=True)
    (recipe / ".SRCINFO").write_text(result.stdout, encoding="utf-8")
    target = output / f"polysync-{release}-{revision}-x86_64.pkg.tar.zst"
    if not target.is_file():
        raise ValueError("makepkg must use PKGEXT='.pkg.tar.zst'; expected package not found")
    bundle = output / f"polysync-{release}-{revision}-arch-build.tar.gz"
    archive(bundle, recipe, ["PKGBUILD", ".SRCINFO", source])
    return [target, bundle]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", required=True, type=version)
    parser.add_argument("--revision", type=int, default=1)
    parser.add_argument("--format", choices=["deb", "arch", "all"], default="all")
    parser.add_argument("--binary-dir", type=Path, default=ROOT / "dist")
    parser.add_argument("--output-dir", type=Path, default=ROOT / "dist")
    args = parser.parse_args()
    if platform.system() != "Linux":
        parser.error("Run in Linux (or WSL); use dpkg-deb and makepkg")
    if args.revision < 1:
        parser.error("revision must be positive")
    output = args.output_dir.resolve()
    binary_dir = args.binary_dir.resolve()
    architectures = ["amd64", "arm64"] if args.format in ("deb", "all") else ["amd64"]
    for arch in architectures:
        check_binary(binary_dir / f"polysync-linux-{arch}", arch, args.version)
    for tool in (["dpkg-deb"] if args.format == "deb" else ["makepkg"] if args.format == "arch" else ["dpkg-deb", "makepkg"]):
        if not shutil.which(tool):
            parser.error(f"Missing packaging tool: {tool}")
    if args.format in ("arch", "all") and os.geteuid() == 0:
        parser.error("makepkg must run as a regular user, not root")
    output.mkdir(parents=True, exist_ok=True)
    artifacts = []
    with tempfile.TemporaryDirectory(prefix="polysync-packaging-") as temporary:
        work = Path(temporary)
        if args.format in ("deb", "all"):
            for arch in architectures:
                artifacts += deb(output, binary_dir / f"polysync-linux-{arch}", arch, args.version, args.revision, work)
        if args.format in ("arch", "all"):
            artifacts += arch_package(output, binary_dir / "polysync-linux-amd64", args.version, args.revision, work)
    manifest = output / f"SHA256SUMS-linux-{args.version}-{args.revision}-{args.format}.txt"
    manifest.write_text("".join(f"{digest(p)}  {p.name}\n" for p in artifacts), encoding="utf-8")
    print(f"Packages verified and checksummed: {manifest}")


if __name__ == "__main__":
    main()
