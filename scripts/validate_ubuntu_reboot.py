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
        (run / "systemd/system").mkdir(parents=True)
        release = root / "os-release"
        trace = root / "calls"
        bindir = root / "bin"
        bindir.mkdir()
        script = source.replace("/run/", str(run) + "/").replace("/etc/os-release", str(release))
        finalrd = bindir / 'finalrd'
        hook_functions = root / 'hook-functions'
        script = script.replace('/usr/bin/finalrd', str(finalrd)).replace(
            '/usr/share/initramfs-tools/hook-functions', str(hook_functions))
        for name, body in {
            "findmnt": '''case "$*" in
  "-n -r -o FSTYPE -M /") echo "$TEST_ROOT" ;;
  "-n -r -o FSTYPE -M /cdrom") echo "$TEST_MEDIA" ;;
  "-n -r -o OPTIONS -M /cdrom") echo "$TEST_OPTIONS" ;;
  "-n -r -o SOURCE -M /cdrom") printf '%s\\n' "$TEST_DEVICE" ;;
  *) exit 99 ;;
esac''',
            "systemctl": '''printf '%s\\n' "$*" >> "$TEST_TRACE"
case "$*" in
  daemon-reload) exit "$TEST_RELOAD_RC" ;;
  "show cdrom.mount -p LoadState --value")
    if [ "$TEST_LOAD_STATE" = masked-always ]; then echo masked
    elif [ "$TEST_LOAD_STATE" = masked ] && [ ! -f "$TEST_UNIT" ]; then echo masked
    else echo loaded; fi ;;
  "show cdrom.mount -p FragmentPath --value") echo "$TEST_FRAGMENT" ;;
  "show cdrom.mount -p LazyUnmount --value") echo "$TEST_LAZY" ;;
  *) exit 99 ;;
esac''',
            "readlink": '''case "$*" in
  /lib/systemd/system/cdrom.mount|/usr/lib/systemd/system/cdrom.mount) echo "$TEST_MASK_TARGET" ;;
  *) exit 99 ;;
esac''',
        }.items():
            exe = bindir / name
            exe.write_text("#!/bin/sh\nset -eu\n" + body + "\n")
            exe.chmod(0o755)

        dropin = run / "systemd/system/cdrom.mount.d/90-ventoy-center-reboot.conf"
        unit = run / "systemd/system/cdrom.mount"
        marker = run / "casper-no-prompt"
        casper = run / ".casper-boot"

        def execute(casper_boot=True, os_id="ubuntu", root_fs="overlay", media="iso9660",
                    options="ro,noatime", reload_rc="0", lazy="yes", load_state="loaded",
                    fragment="/usr/lib/systemd/system/cdrom.mount", device="/dev/sr7",
                    mask_target="/dev/null"):
            release.write_text("ID=" + os_id + "\n")
            if casper_boot:
                casper.touch()
            elif casper.exists():
                casper.unlink()
            env = dict(os.environ, PATH=str(bindir) + os.pathsep + os.environ["PATH"],
                       TEST_TRACE=str(trace), TEST_ROOT=root_fs, TEST_MEDIA=media,
                       TEST_OPTIONS=options, TEST_RELOAD_RC=reload_rc, TEST_LAZY=lazy,
                       TEST_LOAD_STATE=load_state, TEST_UNIT=str(unit), TEST_FRAGMENT=fragment,
                       TEST_DEVICE=device, TEST_MASK_TARGET=mask_target)
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
        assert trace.read_text().splitlines() == ["show cdrom.mount -p LoadState --value", "daemon-reload", "show cdrom.mount -p LoadState --value", "show cdrom.mount -p LazyUnmount --value"] * 2

        # finalrd's runtime hook must carry the dynamically loaded mount library
        # into the shutdown root. It must not run on reboot/poweroff hook calls.
        finalrd.write_text('#!/bin/sh\nexit 0\n')
        finalrd.chmod(0o755)
        library = root / 'libmount.so.1'
        library.write_text('fixture')
        (bindir / 'ldconfig').write_text('#!/bin/sh\nprintf "libmount.so.1 => %s\\n" "' + str(library) + '"\n')
        (bindir / 'ldconfig').chmod(0o755)
        copied = root / 'copied-library'
        hook_functions.write_text('copy_exec() { printf "%s\\n" "$1" > "' + str(copied) + '"; }\n')
        assert execute().returncode == 0
        hook = run / 'finalrd/ventoy-libmount.finalrd'
        assert hook.is_file() and hook.stat().st_mode & 0o777 == 0o755
        env = dict(os.environ, PATH=str(bindir) + os.pathsep + os.environ['PATH'],
                   DESTDIR=str(run / 'initramfs'))
        for action in ('reboot', 'poweroff'):
            assert subprocess.run(['/bin/sh', str(hook), action], env=env).returncode == 0
            assert not copied.exists()
        assert subprocess.run(['/bin/sh', str(hook), 'setup'], env=env).returncode == 0
        assert copied.read_text().strip() == str(library)
        assert subprocess.run(['/bin/sh', str(hook), 'setup'], env=dict(env, DESTDIR='/')).returncode != 0
        finalrd.unlink()  # remaining fixture cases exercise the mount guards

        # A vendor Casper mask needs a complete live-only unit; a drop-in alone
        # is ignored. Describe the actual existing device, never hard-code sr0.
        trace.unlink()
        for _ in range(2):
            result = execute(load_state="masked")
            assert result.returncode == 0, result.stderr
            assert unit.read_text() == ("# Ventoy Center live installer only\n[Mount]\n"
                                        "What=/dev/sr7\nWhere=/cdrom\nType=iso9660\nOptions=ro\n")
            assert dropin.read_text() == "[Mount]\nLazyUnmount=yes\n" and marker.exists()
        assert trace.read_text().count("daemon-reload\n") == 2
        assert unit.stat().st_mode & 0o777 == 0o644
        unit.unlink()
        dropin.unlink()
        marker.unlink()

        # Never override administrator masks or expand arbitrary mount source
        # strings as systemd directives/specifiers. No unit is written on error.
        for args in ({"fragment": "/etc/systemd/system/cdrom.mount"},
                     {"mask_target": "/some/other/mask"}, {"device": "server:/iso"},
                     {"device": "/dev/sr0%v"}, {"device": "/dev/sr0\n[Unit]"},
                     {"device": "/dev/../tmp/iso"}):
            assert execute(load_state="masked", **args).returncode != 0
            assert not unit.exists() and not dropin.exists() and not marker.exists()
        protected = root / "protected"
        protected.write_text("unchanged")
        unit.symlink_to(protected)
        assert execute(load_state="masked-always").returncode != 0
        assert protected.read_text() == "unchanged" and unit.is_symlink()
        unit.unlink()
        assert execute(load_state="masked-always").returncode != 0
        assert not marker.exists()  # reload must actually make the unit usable
        unit.unlink()

        assert execute(reload_rc="1").returncode != 0
        assert not marker.exists()
        assert execute(lazy="no").returncode != 0
        assert not marker.exists()
        dropin.unlink()
        dropin.symlink_to(protected)
        assert execute().returncode != 0
        assert protected.read_text() == "unchanged"
        assert not marker.exists()


if __name__ == "__main__":
    check_ubuntu_reboot(Path("internal/center/templates/ubuntu-reboot.sh").read_text())
    print("Ubuntu reboot: live-only guards, vendor-mask replacement, exact device binding, idempotence, reload verification, symlink refusal passed")
