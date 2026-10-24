"""Exercise the rendered live-root guard without running package operations."""
import os
from pathlib import Path
import subprocess
import tempfile


def check_update_policy_guard(source):
    if os.name == "nt":
        return  # Linux validation runs this against /bin/sh.
    prefix, separator, _ = source.partition("state=/var/lib/ventoy-center/kernel-policy")
    assert separator, "Policy guard boundary is missing"
    with tempfile.TemporaryDirectory(prefix="ventoy-policy-guard-") as directory:
        root = Path(directory)
        release = root / "os-release"
        release.write_text('ID=ubuntu\n')
        media = root / "media-info"
        anaconda = root / "anaconda"
        script = prefix.replace("/etc/os-release", str(release)).replace(
            "/cdrom/.disk/info", str(media)).replace("/run/anaconda", str(anaconda))
        script += "printf 'guard passed\\n'\n"
        bindir = root / "bin"
        bindir.mkdir()
        for name, text in {"id": "echo 0", "stat": '''if [ "$1" = -Lc ]; then
  if [ "$3" = /proc/1/root ]; then echo live-root; else echo "$VC_TEST_ROOT_ID"; fi
else echo "$VC_TEST_ROOT_FSTYPE"; fi'''}.items():
            path = bindir / name
            path.write_text("#!/bin/sh\n" + text + "\n")
            path.chmod(0o755)

        def check(fstype, with_media, with_anaconda, offline, expected, root_id="live-root"):
            for path, present in ((media, with_media), (anaconda, with_anaconda)):
                if present:
                    path.touch()
                elif path.exists():
                    path.unlink()
            env = dict(os.environ, PATH=str(bindir) + os.pathsep + os.environ["PATH"],
                       VC_TEST_ROOT_FSTYPE=fstype, VC_TEST_ROOT_ID=root_id)
            command = ["/bin/sh", "-s", "--", "--apply"] + (["--offline"] if offline else [])
            result = subprocess.run(command, input=script, text=True, env=env, capture_output=True)
            assert (result.returncode == 0) == expected, (fstype, with_media, with_anaconda, offline, result.stderr)
            if not expected:
                assert "Do not apply to the live installer" in result.stderr

        for offline in (False, True):
            for fs in ("ext2/ext3", "xfs", "btrfs", "zfs"):
                check(fs, True, False, offline, True)
            for fs in ("overlayfs", "aufs", "squashfs", "tmpfs", "unknown"):
                check(fs, True, False, offline, False)
            check("ext2/ext3", False, True, offline, False)
            check("xfs", False, True, offline, offline, "installed-target-root")
            check("overlayfs", False, False, offline, True)
