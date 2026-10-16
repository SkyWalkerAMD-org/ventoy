"""Exercise the embedded binder using temporary YAML files, never block devices."""

import copy
import os
from pathlib import Path
import re
import tempfile
from unittest.mock import patch

import yaml


def check_storage_binding(config):
    early = config["early-commands"][0]
    source = re.search(r"<<'VC_UBUNTU_STORAGE'\n(.*?)\nVC_UBUNTU_STORAGE", early, re.S)
    assert source, "Ubuntu guard is missing the storage binder"
    assert early.index("Target disk not uniquely identified") < source.start()
    assert source.end() < early.index('--data \'{"state":"installing"}\'')
    namespace = {"__name__": "storage_fixture"}
    exec(compile(source[1], "ubuntu-storage.py", "exec"), namespace)
    bind = namespace["bind_install_disk"]
    with tempfile.TemporaryDirectory() as directory:
        path = Path(directory) / "autoinstall.yaml"

        def write(document):
            path.write_text(yaml.safe_dump(document), encoding="utf-8")
            return path.read_bytes()

        # Both cloud-config and the flattened form used by Subiquity 5004 work.
        for wrapped in (False, True):
            for disk in ("sda", "nvme0n1", "vda"):
                document = {"autoinstall": config} if wrapped else config
                expected = copy.deepcopy(document)
                target = expected["autoinstall"] if wrapped else expected
                target["storage"]["layout"]["match"] = {"path": "/dev/" + disk}
                write(document)
                bind(path, disk)
                result = yaml.safe_load(path.read_text(encoding="utf-8"))
                actual = result["autoinstall"] if wrapped else result
                if "config" in actual["storage"]:
                    # Validate the resulting disk and partition graph, including
                    # boot mode, without opening a block device.
                    actions = actual["storage"]["config"]
                    ids = {a["id"]: a for a in actions}
                    assert len(ids) == len(actions)
                    disks = [a for a in actions if a["type"] == "disk"]
                    assert len(disks) == 1 and disks[0]["path"] == "/dev/" + disk
                    assert disks[0]["ptable"] == "gpt"
                    assert ids["vc-root"]["device"] == disks[0]["id"]
                    assert ids["vc-root"]["size"] == -1
                    assert ids["vc-root-format"]["fstype"] == "ext4"
                    assert ids["vc-root-mount"]["path"] == "/"
                    uefi = "UEFI boot required" in early
                    assert disks[0]["grub_device"] == (not uefi)
                    assert ids["vc-boot"]["flag"] == ("boot" if uefi else "bios_grub")
                    assert ids["vc-boot"]["grub_device"] == uefi
                    if uefi:
                        assert ids["vc-boot-format"]["fstype"] == "fat32"
                        assert ids["vc-boot-mount"]["path"] == "/boot/efi"
                    else:
                        assert "vc-boot-format" not in ids and "vc-boot-mount" not in ids
                    for action in actions:
                        for reference in ("device", "volume"):
                            if reference in action:
                                assert action[reference] in ids
                    target["storage"].pop("layout")
                    target["storage"]["config"] = actions
                assert result == expected
                if os.name == "posix":
                    assert path.stat().st_mode & 0o777 == 0o600

        for disk in ("", "../sda", "/dev/sda", "sd*", "sda;false", "sda\nsdb"):
            before = write(config)
            try:
                bind(path, disk)
            except ValueError:
                pass
            else:
                raise AssertionError("Unsafe disk name accepted: " + repr(disk))
            assert path.read_bytes() == before

        for document in (
            None, [], {"version": 2}, {"autoinstall": None},
            {"version": 1, "storage": []},
            {"version": 1, "storage": {"config": [{"type": "disk"}]}},
            {"version": 1, "storage": {"layout": {"name": "direct"}}},
            {"version": 1, "storage": {"layout": {"name": "lvm", "match": {"size": "largest"}}}},
            {**config, "storage": {**config["storage"], "grub": {"reorder_uefi": True}}},
        ):
            before = write(document)
            try:
                bind(path, "sda")
            except ValueError:
                pass
            else:
                raise AssertionError("Unexpected configuration accepted")
            assert path.read_bytes() == before

        before = write(config)
        with patch.object(namespace["os"], "replace", side_effect=OSError("fixture write failure")):
            try:
                bind(path, "sda")
            except OSError:
                pass
            else:
                raise AssertionError("Failed write was ignored")
        assert path.read_bytes() == before
        assert sorted(p.name for p in Path(directory).iterdir()) == [path.name]
