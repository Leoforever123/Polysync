#!/usr/bin/env python3
"""Validate the AUR recipe with real makepkg; never install into the host.

Default uses the pinned local binary. --download requires license metadata and
retrieves the public release instead, for the final pre-push acceptance check.
"""
import argparse
import hashlib
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parents[1]
RECIPE = ROOT / 'packaging/aur/polysync-bin'


def run(*args, **kwargs):
    return subprocess.run([str(arg) for arg in args], check=True, **kwargs)


def metadata(text):
    result = {}
    for line in text.splitlines():
        if ' = ' in line:
            name, value = line.strip().split(' = ', 1)
            result.setdefault(name, []).append(value)
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--download', action='store_true')
    args = parser.parse_args()
    info_text = run('makepkg', '--printsrcinfo', cwd=RECIPE, capture_output=True, text=True).stdout
    assert info_text == (RECIPE / '.SRCINFO').read_text(), 'Regenerate .SRCINFO with makepkg --printsrcinfo'
    info = metadata(info_text)
    version = info['pkgver'][0]
    assert info['pkgname'] == ['polysync-bin']
    assert info['provides'] == [f'polysync={version}']
    assert info['conflicts'] == ['polysync']
    assert info['arch'] == ['x86_64']
    assert info['license'] == ['LicenseRef-Proprietary']
    notice = (ROOT / 'LICENSE').read_text()
    assert (RECIPE / 'copyright').read_text() == notice
    assert (ROOT / 'packaging/linux/copyright').read_text() == notice
    source_name = f'polysync-{version}-linux-amd64'
    assert info['source_x86_64'] == [f'{source_name}::https://github.com/Leoforever123/Polysync/releases/download/v{version}/polysync-linux-amd64']
    for key, values in info.items():
        if key.startswith('sha256sums'):
            assert all(len(value) == 64 and all(c in '0123456789abcdef' for c in value) for value in values)
    if args.download:
        assert info.get('license'), 'Public acceptance blocked: upstream license is not specified'
    # Local helper files are intentionally included in AUR, with LF on Windows.
    for name in info['source']:
        assert '/' not in name and ':' not in name
        assert b'\r' not in (RECIPE / name).read_bytes(), f'Non-Unix line ending: {name}'
    with tempfile.TemporaryDirectory(prefix='polysync-aur-test-') as temporary:
        directory = Path(temporary)
        recipe = directory / 'recipe'
        shutil.copytree(RECIPE, recipe)
        env = dict(os.environ, SRCDEST=str(recipe), PKGDEST=str(directory),
                   BUILDDIR=str(directory / 'build'), PKGEXT='.pkg.tar.zst')
        if not args.download:
            shutil.copyfile(ROOT / 'dist/polysync-linux-amd64', recipe / source_name)
        run('bash', '-n', recipe / 'PKGBUILD')
        run('makepkg', '--force', '--cleanbuild', '--noconfirm', cwd=recipe, env=env)
        package = directory / f'polysync-bin-{version}-{info["pkgrel"][0]}-x86_64.pkg.tar.zst'
        unpacked = directory / 'unpacked'
        unpacked.mkdir()
        run('bsdtar', '-xf', package, '-C', unpacked)
        binary = unpacked / 'usr/bin/polysync'
        assert hashlib.sha256(binary.read_bytes()).hexdigest() == info['sha256sums_x86_64'][0]
        assert os.access(binary, os.X_OK)
        assert run(binary, '-version', capture_output=True, text=True).stdout.strip() == f'PolySync {version}'
        run('desktop-file-validate', unpacked / 'usr/share/applications/polysync.desktop')
        for source, target in {
            'polysync-open': 'usr/bin/polysync-open',
            'polysync.service': 'usr/lib/systemd/user/polysync.service',
            'polysync.svg': 'usr/share/icons/hicolor/scalable/apps/polysync.svg',
        }.items():
            assert (recipe / source).read_bytes() == (unpacked / target).read_bytes()
        assert not (unpacked / 'etc').exists(), 'Unexpected system configuration'
        assert not list(unpacked.rglob('*.wants')), 'Service must not be auto-enabled'
        package_info = metadata((unpacked / '.PKGINFO').read_text())
        assert package_info['pkgname'] == ['polysync-bin']
        assert package_info['provides'] == [f'polysync={version}']
        assert package_info['conflict'] == ['polysync']
        assert package_info['license'] == ['LicenseRef-Proprietary']
        assert (unpacked / 'usr/share/licenses/polysync-bin/LICENSE').read_text() == notice
        assert (unpacked / 'usr/share/doc/polysync/THIRD_PARTY_NOTICES.txt').read_text() == (ROOT / 'THIRD_PARTY_NOTICES.txt').read_text()
        # AUR carries recipes and small helper files, never prebuilt packages.
        run('makepkg', '--source', '--force', cwd=recipe, env=env)
        source_archive = next(recipe.glob('*.src.tar.*'), None)
        if source_archive is None:
            source_archive = next(directory.glob('*.src.tar.*'))
        with tarfile.open(source_archive) as contents:
            assert not any(member.name.endswith(source_name) for member in contents.getmembers())
        with (recipe / source_name).open('ab') as stream:
            stream.write(b'tampered')
        rejected = subprocess.run(['makepkg', '--verifysource'], cwd=recipe, env=env, capture_output=True)
        assert rejected.returncode != 0, 'Corrupt binary unexpectedly accepted'
    mode = 'public download' if args.download else 'local pinned binary; public download NOT verified'
    print(f'PASS AUR: metadata, build, payload/version, desktop entry, source archive, tamper rejection ({mode})')


if __name__ == '__main__':
    main()
