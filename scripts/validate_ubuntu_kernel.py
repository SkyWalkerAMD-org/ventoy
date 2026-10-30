"""Test kernel selection before the destructive claim, using temporary YAML."""
import copy
from pathlib import Path
import re
import tempfile
from unittest.mock import patch

import yaml


def check_kernel_binding(config):
    early = config["early-commands"][0]
    source = re.search(r"<<'VC_UBUNTU_KERNEL'\n(.*?)\nVC_UBUNTU_KERNEL", early, re.S)
    assert source
    assert source.end() < early.index('--data \'{"state":"installing"}\'')
    namespace = {"__name__": "kernel_fixture"}
    exec(compile(source[1], "ubuntu-kernel.py", "exec"), namespace)
    lock = namespace["lock_installer_kernel"]
    media_metadata = namespace["media_kernel_metadata"]
    release = "5.15.0-78-generic"
    packages = (
        "linux-image-5.15.0-78-generic\t5.15.0-78.85\tinstalled\n"
        "linux-modules-5.15.0-78-generic\t5.15.0-78.85\tinstalled\n"
        "linux-generic\t5.15.0.78.75\tinstalled\n"
        "linux-image-5.15.0-79-generic\t5.15.0-79.86\tconfig-files\n"
        "linux-firmware\t20220329.git681281e4-0ubuntu3.14\tinstalled\n"
        "curl\t7.81.0-1ubuntu1.13\tinstalled\n"
    )
    with tempfile.TemporaryDirectory() as directory:
        path = Path(directory) / "autoinstall.yaml"
        pool = Path(directory) / 'pool'
        pool.mkdir()
        for name in ('linux-image-' + release, 'linux-modules-' + release,
                     'linux-image-5.15.0-99-generic'):
            (pool / (name + '_5.15.0-78.85_amd64.deb')).touch()

        def metadata(args, **kwargs):
            name = Path(args[2]).name.split('_')[0]
            return {'Package': name, 'Version': '5.15.0-78.85', 'Architecture': 'amd64'}[args[3]]

        with patch.object(namespace['subprocess'], 'check_output', side_effect=metadata):
            media = media_metadata(release, pool)
            assert 'linux-image-' + release + '\t5.15.0-78.85\tinstalled' in media
            assert '5.15.0-99' not in media
            (pool / ('linux-modules-' + release + '_5.15.0-78.85_amd64.deb')).unlink()
            try:
                media_metadata(release, pool)
            except ValueError:
                pass
            else:
                raise AssertionError('Accepted media missing the exact kernel modules')
        for wrapped in (False, True):
            original = copy.deepcopy(config)
            document = {"autoinstall": original} if wrapped else original
            path.write_text(yaml.safe_dump(document), encoding="utf-8")
            lock(path, release, packages)
            result = yaml.safe_load(path.read_text(encoding="utf-8"))
            actual = result["autoinstall"] if wrapped else result
            assert actual.pop("kernel") == {"package": "linux-image-" + release}
            pins = actual["apt"].pop("preferences")
            assert len(pins) == 4
            assert pins[0] == {"package": "linux-generic", "pin": "version 5.15.0.78.75", "pin-priority": 1001}
            assert pins[-1]["pin-priority"] == -1 and "linux-image*" in pins[-1]["package"]
            assert "linux-firmware" not in str(pins) and "5.15.0-79" not in str(pins)
            apt_conf = actual["apt"].pop("conf")
            assert 'Package-Blacklist { ".*"; }' in apt_conf
            original_conf = original["apt"].pop("conf", "")
            assert apt_conf.startswith(original_conf), "Existing signature/retry policy lost"
            assert result == document, "Unrelated installer settings changed"
        for bad_release, bad_packages in (("5.15.0-99-generic", packages), (release, ""), ("../bad", packages), (release, packages.replace("5.15.0-78.85", "*"))):
            path.write_text(yaml.safe_dump(config), encoding="utf-8")
            before = path.read_bytes()
            try:
                lock(path, bad_release, bad_packages)
            except ValueError:
                pass
            else:
                raise AssertionError("Accepted missing or invalid kernel metadata")
            assert path.read_bytes() == before
        for extra in ({"kernel": {"flavor": "hwe"}}, {"apt": {"preferences": []}}):
            path.write_text(yaml.safe_dump({**config, **extra}), encoding="utf-8")
            before = path.read_bytes()
            try:
                lock(path, release, packages)
            except ValueError:
                pass
            else:
                raise AssertionError("Overwrote an unexpected kernel policy")
            assert path.read_bytes() == before
