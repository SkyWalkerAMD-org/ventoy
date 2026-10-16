import fnmatch
import os
from pathlib import Path
import re
import subprocess
import tempfile

import yaml


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
            yaml.safe_dump(document, stream, sort_keys=False)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(pending, path)
        pending = None
    finally:
        if pending is not None:
            os.unlink(pending)


if __name__ == "__main__":
    installed = subprocess.check_output(
        ["dpkg-query", "-W", "-f=${Package}\t${Version}\t${db:Status-Status}\n"], text=True,
    )
    lock_installer_kernel("/autoinstall.yaml", os.uname().release, installed)
