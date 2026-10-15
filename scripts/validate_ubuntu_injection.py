"""Run the injection hooks against a fake initramfs; never touch a live root."""

import hashlib
import os
from pathlib import Path
import subprocess
import tempfile


def check_injection(archive, user_data):
    if os.name != "posix":
        return  # Linux CI exercises execution; Windows still checks shell syntax.
    prefix = "live_injection_7ed136ec_7a61_4b54_adc3_ae494d5106ea/"
    hook = archive.extractfile(prefix + "hook.sh").read().decode()
    live = archive.extractfile("ventoy-center/ubuntu-live.sh").read().decode()
    assert hashlib.sha256(user_data).hexdigest() in hook
    with tempfile.TemporaryDirectory() as directory:
        root = Path(directory)
        for name in ("ventoy", "scripts", "ventoy-center", "sys/class/net/eth0"):
            (root / name).mkdir(parents=True)
        (root / "scripts/casper").touch()
        init = root / "init"
        selected = root / "ventoy/autoinstall"
        original = "#!/bin/sh\nmountroot\nmaybe_break init\nexec /sbin/init\n"

        def run(source, *args):
            # Only rewrite absolute fixture paths, including quoted shell paths.
            for path in ("/ventoy/", "/ventoy-center/", "/scripts/", "/sys/class/net/", "/init"):
                source = source.replace(path, str(root) + path)
            return subprocess.run(["sh", "-s", "--", *args], input=source, text=True,
                                  stdout=subprocess.PIPE, stderr=subprocess.PIPE)

        for data, init_text, should_enable in (
            (None, original, False),
            (user_data + b"\n# wrong task", original, False),
            (user_data, original.replace("maybe_break init", "different_init"), False),
            (user_data, original + "maybe_break init\n", False),
            (user_data, original, True),
        ):
            init.write_text(init_text)
            if data is None:
                selected.unlink(missing_ok=True)
            else:
                selected.write_bytes(data)
            result = run(hook)
            if should_enable:
                assert result.returncode == 0, result.stderr
                assert "ubuntu-live.sh" in init.read_text()
            else:
                assert init.read_text() == init_text, "Nonmatching input activated injection"

        live_root = root / "live"
        service = live_root / "etc/systemd/system/snap.subiquity.subiquity-server.service"
        service.parent.mkdir(parents=True)
        service.write_text("[Service]\nExecStart=/usr/bin/snap run subiquity.subiquity-server\n")
        (live_root / "var/log").mkdir(parents=True)
        release = live_root / "etc/os-release"
        mac = root / "sys/class/net/eth0/address"
        (root / "ventoy-center/ubuntu-subiquity.sh").write_bytes(
            archive.extractfile("ventoy-center/ubuntu-subiquity.sh").read())
        dropin = Path(str(service) + ".d/ventoy-center.conf")
        selected.write_bytes(user_data)
        for version, address, should_enable in (
            ("24.04", "02:00:00:00:01:01", False),
            ("22.04", "02:00:00:00:02:02", False),
            ("22.04", "02:00:00:00:01:01", True),
        ):
            release.write_text("ID=ubuntu\nVERSION_ID=" + version + "\n")
            mac.write_text(address + "\n")
            result = run(live, str(live_root))
            assert dropin.exists() == should_enable
            if should_enable:
                assert result.returncode == 0, result.stderr
                assert "ExecStart=/usr/local/sbin/ventoy-center-subiquity" in dropin.read_text()
            else:
                assert result.returncode != 0
