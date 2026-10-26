import os
from pathlib import Path
import re
import sys
import tempfile
import yaml

{{.UbuntuSizes}}

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
    sizes = storage_sizes()
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
    ])
    for name, mib in sizes:
        volume = {"id": "vc-" + name + "-lv", "type": "lvm_partition", "name": name,
                  "volgroup": "vc-vg", "preserve": False}
        # Curtin consumes the VG's remaining extents only when size is omitted.
        if mib is not None:
            volume["size"] = mib * 1048576
        actions.extend([
            volume,
            {"id": "vc-" + name + "-fs", "type": "format", "volume": volume["id"],
             "fstype": "swap" if name == "swap" else {{yaml .DataFilesystem}}, "preserve": False},
            {"id": "vc-" + name + "-mount", "type": "mount", "device": "vc-" + name + "-fs",
             "path": "none" if name == "swap" else "/" if name == "root" else "/" + name},
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
