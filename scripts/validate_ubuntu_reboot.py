"""Exercise live-media guards without mounting, rebooting, or changing this host."""
import os
from pathlib import Path
import subprocess
import tempfile


def check_ubuntu_reboot(source):
    if os.name == "nt":
        return  # The Linux CI job runs the shell and filesystem checks.
    with tempfile.TemporaryDirectory(prefix="ventoy-reboot-") as directory:
        root = Path(directory)
        run = root / "run"
        run.mkdir()
        release = root / "os-release"
        trace = root / "calls"
        bindir = root / "bin"
        bindir.mkdir()
        script = source.replace("/run/", str(run) + "/").replace("/etc/os-release", str(release))
        for name, body in {
            "findmnt": '''case "$*" in
  "-n -r -o FSTYPE -M /") echo "$TEST_ROOT" ;;
  "-n -r -o FSTYPE -M /cdrom") echo "$TEST_MEDIA" ;;
  "-n -r -o OPTIONS -M /cdrom") echo "$TEST_OPTIONS" ;;
  *) exit 99 ;;
esac''',
            "systemctl": '''printf '%s\\n' "$*" >> "$TEST_TRACE"
case "$*" in
  daemon-reload) exit "$TEST_RELOAD_RC" ;;
  "show cdrom.mount -p LazyUnmount --value") echo "$TEST_LAZY" ;;
  *) exit 99 ;;
esac''',
        }.items():
            exe = bindir / name
            exe.write_text("#!/bin/sh\nset -eu\n" + body + "\n")
            exe.chmod(0o755)

        dropin = run / "systemd/system/cdrom.mount.d/90-ventoy-center-reboot.conf"
        marker = run / "casper-no-prompt"
        casper = run / ".casper-boot"

        def execute(casper_boot=True, os_id="ubuntu", root_fs="overlay", media="iso9660",
                    options="ro,noatime", reload_rc="0", lazy="yes"):
            release.write_text("ID=" + os_id + "\n")
            if casper_boot:
                casper.touch()
            elif casper.exists():
                casper.unlink()
            env = dict(os.environ, PATH=str(bindir) + os.pathsep + os.environ["PATH"],
                       TEST_TRACE=str(trace), TEST_ROOT=root_fs, TEST_MEDIA=media,
                       TEST_OPTIONS=options, TEST_RELOAD_RC=reload_rc, TEST_LAZY=lazy)
            return subprocess.run(["/bin/sh"], input=script, env=env, text=True, capture_output=True)

        # Installed systems, other installers, missing mount, and writable media
        # must be no-ops, including no daemon-reload or no-prompt marker.
        for args in ({"casper_boot": False}, {"os_id": "debian"}, {"root_fs": "ext4"},
                     {"media": ""}, {"media": "ext4"}, {"options": "rw,noatime"}):
            result = execute(**args)
            assert result.returncode == 0, result.stderr
            assert not dropin.exists() and not trace.exists() and not marker.exists()

        result = execute()
        assert result.returncode == 0, result.stderr
        assert dropin.read_text() == "[Mount]\nLazyUnmount=yes\n"
        assert marker.exists() and dropin.stat().st_mode & 0o777 == 0o644
        assert execute().returncode == 0  # repeatable, no duplicate configuration
        assert trace.read_text().splitlines() == ["daemon-reload", "show cdrom.mount -p LazyUnmount --value"] * 2

        marker.unlink()
        assert execute(reload_rc="1").returncode != 0
        assert not marker.exists()
        assert execute(lazy="no").returncode != 0
        assert not marker.exists()
        dropin.unlink()
        protected = root / "protected"
        protected.write_text("unchanged")
        dropin.symlink_to(protected)
        assert execute().returncode != 0
        assert protected.read_text() == "unchanged"
        assert not marker.exists()


if __name__ == "__main__":
    check_ubuntu_reboot(Path("internal/center/templates/ubuntu-reboot.sh").read_text())
    print("Ubuntu reboot: live-only guards, read-only ISO scope, idempotence, reload verification, symlink refusal passed")
