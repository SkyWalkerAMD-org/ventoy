import fnmatch
import os
from pathlib import Path
import re
import subprocess
import tempfile

import yaml


def media_kernel_metadata(release, media_pool=Path('/cdrom/pool')):
    """Old Server media keeps its kernel as debs, outside the live dpkg DB."""
    if not re.fullmatch(r"[0-9][a-zA-Z0-9.+-]*", release):
        raise ValueError("Invalid installer kernel release")
    names = {prefix + release for prefix in
             ('linux-image-', 'linux-modules-', 'linux-modules-extra-', 'linux-headers-')}
    names.add('linux-headers-' + release.rsplit('-', 1)[0])
    packages = {}
    for path in sorted(media_pool.rglob('linux-*.deb')):
        if path.name.split('_', 1)[0] not in names:
            continue
        package, version, arch = [subprocess.check_output(
            ['dpkg-deb', '-f', str(path), field], universal_newlines=True).strip()
            for field in ('Package', 'Version', 'Architecture')]
        if package not in names or arch not in ('amd64', 'all'):
            raise ValueError('Unexpected kernel package on the installation media')
        if package in packages and packages[package] != version:
            raise ValueError('Conflicting kernel versions on the installation media')
        packages[package] = version
    if 'linux-image-' + release not in packages or 'linux-modules-' + release not in packages:
        raise ValueError('Media lacks the exact boot kernel and modules')
    return ''.join('%s\t%s\tinstalled\n' % item for item in sorted(packages.items()))


def lock_installer_kernel(config_path, release, package_output):
    """Pin the live media's kernel BEFORE Curtin installs a kernel or runs UU."""
    if not re.fullmatch(r"[0-9][a-zA-Z0-9.+-]*", release):
        raise ValueError("Invalid installer kernel release")
    patterns = "{{.KernelPackages}}".split()
    packages = {}
    for line in package_output.splitlines():
        fields = line.split("\t")
        if len(fields) != 3 or fields[2] != "installed":
            continue
        name, version, _ = fields
        if not any(fnmatch.fnmatchcase(name, pattern) for pattern in patterns):
            continue
        if not re.fullmatch(r"[a-z0-9][a-z0-9+.-]*", name) or not re.fullmatch(r"[0-9A-Za-z.+:~_-]+", version):
            raise ValueError("Invalid kernel package metadata")
        packages[name] = version
    image = "linux-image-" + release
    if image not in packages:
        raise ValueError("Live media does not contain its booted kernel package; refusing to substitute a newer kernel")
    path = Path(config_path)
    document = yaml.safe_load(path.read_text(encoding="utf-8"))
    if not isinstance(document, dict):
        raise ValueError("Invalid autoinstall configuration")
    config = document.get("autoinstall", document)
    if not isinstance(config, dict) or config.get("version") != 1:
        raise ValueError("Unsupported autoinstall configuration")
    apt = config.get("apt")
    if not isinstance(apt, dict) or "preferences" in apt or "kernel" in config:
        raise ValueError("Unexpected custom kernel/apt preferences; review before installation")
    config["kernel"] = {"package": image}
    apt["preferences"] = [
        {"package": name, "pin": "version " + version, "pin-priority": 1001}
        for name, version in sorted(packages.items())
    ] + [{"package": " ".join(patterns), "pin": "version *", "pin-priority": -1}]
    # Subiquity only accepts updates=security/all. A blacklist prevents its
    # mandatory postinstall unattended-upgrades pass from changing packages.
    apt["conf"] = apt.get("conf", "") + '\nUnattended-Upgrade::Package-Blacklist { ".*"; };\n'
    pending = None
    try:
        with tempfile.NamedTemporaryFile(mode="w", encoding="utf-8", dir=path.parent,
                                         prefix=".ventoy-kernel-", delete=False) as stream:
            pending = stream.name
            os.fchmod(stream.fileno(), 0o600)
            # Ubuntu 20.04.3 ships PyYAML before the sort_keys keyword existed.
            yaml.safe_dump(document, stream, default_flow_style=False)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(pending, path)
        pending = None
    finally:
        if pending is not None:
            os.unlink(pending)


if __name__ == "__main__":
    installed = subprocess.check_output(
        ["dpkg-query", "-W", "-f=${Package}\t${Version}\t${db:Status-Status}\n"], universal_newlines=True,
    )
    release = os.uname().release
    if not any(line.startswith('linux-image-' + release + '\t') and line.endswith('\tinstalled')
               for line in installed.splitlines()):
        installed += media_kernel_metadata(release)
    lock_installer_kernel("/autoinstall.yaml", release, installed)
