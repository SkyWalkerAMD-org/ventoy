"""Run only in a disposable root/container. Exercises the real package manager.

Usage: python3 validate_update_policy.py /policy.sh
The runner must create /run/ventoy-policy-disposable-root before invoking this.
"""
import os
from pathlib import Path
import subprocess
import sys
import tempfile


def run(*args, good=True):
    result = subprocess.run(args, universal_newlines=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    if good and result.returncode:
        raise AssertionError("Command failed: " + str(args) + "\n" + result.stdout)
    return result


def main():
    assert os.geteuid() == 0 and Path("/run/ventoy-policy-disposable-root").is_file(), "Disposable root marker required"
    policy = sys.argv[1]
    assert run("sh", policy, "--check", good=False).returncode != 0
    run("sh", policy, "--apply", "--offline")
    state = Path("/var/lib/ventoy-center/kernel-policy/kernel-packages.tsv")
    locked = state.read_bytes()
    if Path("/etc/apt").is_dir():
        managed = Path("/etc/apt/preferences.d/00-ventoy-center-kernel")
    else:
        managed = Path("/etc/dnf/dnf.conf") if Path("/etc/dnf/dnf.conf").is_file() else Path("/etc/yum.conf")
    before = managed.read_bytes()
    run("sh", policy, "--apply", "--offline")
    assert state.read_bytes() == locked and managed.read_bytes() == before, "Policy is not idempotent"
    run("sh", policy, "--check")
    if Path("/etc/apt").is_dir():
        # Test candidate selection with a newer version of the SAME package, a
        # new ABI package, and an unrelated package. No downloads/installations.
        kernels = dict(line.split("\t") for line in locked.decode().splitlines())
        image = next(name for name in kernels if name.startswith("linux-image-") and name[len("linux-image-")].isdigit())
        with tempfile.TemporaryDirectory(prefix="ventoy-apt-fixture-") as directory:
            repo = Path(directory)
            repo.chmod(0o755)
            records = []
            for package in (image, "linux-image-99.99.0-1-generic", "curl"):
                records.append("Package: " + package + "\nVersion: 99:99.99.99\nArchitecture: amd64\nMaintainer: Fixture <fixture@example.invalid>\nFilename: fixture.deb\nSize: 1\nDescription: resolver fixture only\n")
            (repo / "Packages").write_text("\n".join(records))
            (repo / "sources.list").write_text("deb [trusted=yes] file:" + str(repo) + " ./\n")
            options = ["-o", "Dir::Etc::sourcelist=" + str(repo / "sources.list"), "-o", "Dir::Etc::sourceparts=-"]
            run("apt-get", *options, "update")
            existing = run("apt-cache", *options, "policy", image).stdout
            assert "Candidate: " + kernels[image] in existing, existing
            new = run("apt-cache", *options, "policy", "linux-image-99.99.0-1-generic").stdout
            assert "Candidate: (none)" in new, new
            unrelated = run("apt-cache", *options, "policy", "curl").stdout
            assert "Candidate: 99:99.99.99" in unrelated, unrelated
            assert run("apt-get", *options, "-s", "install", "linux-image-99.99.0-1-generic", good=False).returncode != 0
        apt = run("apt-config", "dump").stdout
        assert 'Unattended-Upgrade::Package-Blacklist:: ".*";' in apt
        # Detect a removed pin even while the existing image remains held.
        managed.write_text(before.decode().replace("Pin-Priority: -1", "Pin-Priority: 500"))
        assert run("sh", policy, "--check", good=False).returncode != 0
        managed.write_bytes(before)
    else:
        assert "kernel*" in managed.read_text()
    run("sh", policy, "--check")
    print("PASS: apply/check, repeat application, persistent masks, kernel inventory and package policy")


if __name__ == "__main__":
    main()
