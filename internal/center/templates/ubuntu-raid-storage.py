import os
from pathlib import Path
import re
import sys
import tempfile
import yaml


def bind_install_disks(config_path, first, second):
    if first == second or not all(re.fullmatch(r"[a-zA-Z0-9_-]+", name) for name in (first, second)):
        raise ValueError("Two different verified disk names are required")
    path = Path(config_path)
    document = yaml.safe_load(path.read_text(encoding="utf-8"))
    config = document.get("autoinstall", document)
    if config.get("version") != 1:
        raise ValueError("Unsupported installation configuration")
    storage = config.get("storage")
    expected = {"layout": {"name": "direct", "match": {"path": "/dev/ventoy-center-unverified"}},
                "grub": {"reorder_uefi": False, "remove_duplicate_entries": False}}
    if storage != expected:
        raise ValueError("Storage changed before verified RAID binding")
    uefi = {{if eq .Machine.Firmware "uefi"}}True{{else}}False{{end}}
    actions = []
    for index, name in enumerate((first, second)):
        disk = "vc-disk-%d" % index
        actions.extend([
            {"id": disk, "type": "disk", "path": "/dev/" + name, "ptable": "gpt",
             "wipe": "superblock-recursive", "preserve": False, "grub_device": not uefi},
            {"id": disk + "-efi", "type": "partition", "device": disk, "number": 1,
             "size": 1073741824 if uefi else 1048576, "flag": "boot" if uefi else "bios_grub",
             "grub_device": uefi, "preserve": False},
            {"id": disk + "-boot", "type": "partition", "device": disk, "number": 2,
             "size": 2147483648, "flag": "raid", "wipe": "superblock", "preserve": False},
            {"id": disk + "-data", "type": "partition", "device": disk, "number": 3,
             "size": -1, "flag": "raid", "wipe": "superblock", "preserve": False},
        ])
        if uefi:
            actions.extend([
                {"id": disk + "-efi-fs", "type": "format", "volume": disk + "-efi", "fstype": "fat32", "preserve": False},
                {"id": disk + "-efi-mount", "type": "mount", "device": disk + "-efi-fs",
                 "path": "/boot/efi" if index == 0 else "/boot/efi2",
                 "options": "defaults,nofail,x-systemd.device-timeout=5s"},
            ])
    actions.extend([
        {"id": "vc-boot-md", "type": "raid", "name": "md0", "raidlevel": 1,
         "devices": ["vc-disk-0-boot", "vc-disk-1-boot"], "spare_devices": [], "preserve": False},
        {"id": "vc-data-md", "type": "raid", "name": "md1", "raidlevel": 1,
         "devices": ["vc-disk-0-data", "vc-disk-1-data"], "spare_devices": [], "preserve": False},
        {"id": "vc-boot-fs", "type": "format", "volume": "vc-boot-md", "fstype": "ext4", "preserve": False},
        {"id": "vc-boot-mount", "type": "mount", "device": "vc-boot-fs", "path": "/boot"},
        {"id": "vc-vg", "type": "lvm_volgroup", "name": "vcvg", "devices": ["vc-data-md"], "preserve": False},
        {"id": "vc-swap-lv", "type": "lvm_partition", "name": "swap", "volgroup": "vc-vg", "size": 2147483648, "preserve": False},
        {"id": "vc-root-lv", "type": "lvm_partition", "name": "root", "volgroup": "vc-vg", "preserve": False},
        {"id": "vc-swap-fs", "type": "format", "volume": "vc-swap-lv", "fstype": "swap", "preserve": False},
        {"id": "vc-swap-mount", "type": "mount", "device": "vc-swap-fs", "path": "none"},
        {"id": "vc-root-fs", "type": "format", "volume": "vc-root-lv", "fstype": "ext4", "preserve": False},
        {"id": "vc-root-mount", "type": "mount", "device": "vc-root-fs", "path": "/"},
    ])
    storage.pop("layout")
    storage["config"] = actions
    pending = None
    try:
        with tempfile.NamedTemporaryFile(mode="w", encoding="utf-8", dir=path.parent,
                                         prefix=".ventoy-raid-", delete=False) as stream:
            pending = stream.name
            os.fchmod(stream.fileno(), 0o600)
            yaml.safe_dump(document, stream, default_flow_style=False)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(pending, path)
        pending = None
    finally:
        if pending is not None:
            os.unlink(pending)


if __name__ == "__main__":
    bind_install_disks("/autoinstall.yaml", sys.argv[1], sys.argv[2])
