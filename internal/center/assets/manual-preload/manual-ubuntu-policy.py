"""Policy-only Subiquity defaults; disks and accounts remain interactive."""
import importlib.util
import json
import os
from pathlib import Path
import shlex
import subprocess
import tempfile


def ubuntu_version(path=Path('/etc/os-release')):
    for line in path.read_text().splitlines():
        key, separator, value = line.partition('=')
        if key == 'VERSION_ID' and separator:
            values = shlex.split(value, comments=True)
            if len(values) == 1:
                return values[0]
    raise ValueError('Missing Ubuntu VERSION_ID; cannot select the installation mirror')


def prepare(path, release, packages, lock, version):
    path = Path(path)
    marker = path.parent / 'run/ventoy-manual-policy-config.json'
    if path.exists() or path.is_symlink():
        if not path.is_symlink() and marker.is_file() and path.read_bytes() == marker.read_bytes():
            return
        raise RuntimeError('Existing installation answers require review; not replacing them')
    archived = version in ('24.10', '25.04')
    config = {
        'version': 1,
        # '*' would also make refresh-installer interactive and override its
        # update:false default: Jammy would still contact the Snap Store. Keep
        # the normal operator choices interactive but exclude that one step.
        'interactive-sections': [
            'locale', 'keyboard', 'source', 'network', 'proxy', 'apt', 'storage',
            'identity', 'ssh', 'snaps', 'ubuntu-pro', 'drivers', 'codecs', 'oem',
            'active-directory',
        ],
        'refresh-installer': {'update': False},
        'apt': {
            'geoip': False,
            # Match the automatic installer: HTTP country mirrors can serve
            # their indexes but reject bootloader deb downloads with 403.
            # The mirror page stays interactive for explicitly chosen mirrors.
            'preserve_sources_list': False,
            'primary': [{'arches': ['default'], 'uri':
                         'https://%s.ubuntu.com/ubuntu' % ('old-releases' if archived else 'archive')}],
            'security': [{'arches': ['default'], 'uri':
                          'https://%s.ubuntu.com/ubuntu' % ('old-releases' if archived else 'security')}],
            'conf': 'APT::Periodic::Enable "0";\n',
        },
    }
    # Share the automatic installer's media-kernel pin and UU blacklist.
    # No disk, identity, network, or automatic reboot answers are supplied.
    with tempfile.TemporaryDirectory(prefix='ventoy-manual-kernel-') as tmp:
        pending = Path(tmp) / 'policy.json'
        pending.write_text(json.dumps(config))
        lock(pending, release, packages)
        data = pending.read_bytes()
    with path.open('xb') as stream:
        os.fchmod(stream.fileno(), 0o600)
        stream.write(data)
    marker.parent.mkdir(parents=True, exist_ok=True)
    marker.write_bytes(data)
    marker.chmod(0o600)


if __name__ == '__main__':
    source = Path(__file__).with_name('ubuntu-kernel.py')
    spec = importlib.util.spec_from_file_location('ventoy_media_kernel', str(source))
    kernel = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(kernel)
    release = os.uname().release
    packages = subprocess.check_output(
        ['dpkg-query', '-W', '-f=${Package}\t${Version}\t${db:Status-Status}\n'],
        universal_newlines=True)
    if not any(line.startswith('linux-image-' + release + '\t') and line.endswith('\tinstalled')
               for line in packages.splitlines()):
        packages += kernel.media_kernel_metadata(release)
    prepare('/autoinstall.yaml', release, packages, kernel.lock_installer_kernel, ubuntu_version())
    print('Ventoy manual install: interactive screens retained; media kernel pinned; automatic package upgrades blocked.')
