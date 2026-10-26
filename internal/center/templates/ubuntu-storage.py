import os
from pathlib import Path
import re
import sys
import tempfile

import yaml

{{.UbuntuSizes}}

def bind_install_disk(config_path, disk):
    if not re.fullmatch(r"[a-zA-Z0-9_-]+", disk):
        raise ValueError("Invalid verified disk name")
    config_path = Path(config_path)
    document = yaml.safe_load(config_path.read_text(encoding="utf-8"))
    if not isinstance(document, dict):
        raise ValueError("Invalid autoinstall configuration")
    config = document.get("autoinstall", document)
    if not isinstance(config, dict) or config.get("version") != 1:
        raise ValueError("Unsupported autoinstall configuration")
    storage = config.get("storage", {})
    if not isinstance(storage, dict) or set(storage) != {"layout", "grub"}:
        raise ValueError("Unexpected storage configuration")
    if storage["grub"] != {"reorder_uefi": False, "remove_duplicate_entries": False}:
        raise ValueError("Unexpected boot order configuration")
    layout = storage["layout"]
    if layout != {"name": "direct", "match": {"path": "/dev/ventoy-center-unverified"}}:
        raise ValueError("Unexpected storage layout; refusing to select a disk")
    sizes = storage_sizes()
    # Explicit actions preserve the same layout on every Subiquity version.
    # This only rewrites YAML; the one-use claim still precedes disk writes.
    uefi = {{if eq .Machine.Firmware "uefi"}}True{{else}}False{{end}}
    actions = [
        {"id": "vc-disk", "type": "disk", "path": "/dev/" + disk,
         "ptable": "gpt", "wipe": "superblock-recursive", "preserve": False,
         "grub_device": not uefi},
        {"id": "vc-boot", "type": "partition", "device": "vc-disk",
         "number": 1, "size": 1073741824 if uefi else 1048576,
         "flag": "boot" if uefi else "bios_grub", "grub_device": uefi,
         "wipe": "superblock", "preserve": False},
    ]
    if uefi:
        actions.extend([
            {"id": "vc-boot-format", "type": "format", "volume": "vc-boot",
             "fstype": "fat32", "preserve": False},
            {"id": "vc-boot-mount", "type": "mount", "device": "vc-boot-format",
             "path": "/boot/efi"},
        ])
    actions.extend([
        {"id": "vc-linux-boot", "type": "partition", "device": "vc-disk",
         "number": 2, "size": 2147483648, "wipe": "superblock", "preserve": False},
        {"id": "vc-linux-boot-format", "type": "format", "volume": "vc-linux-boot",
         "fstype": "ext4", "preserve": False},
        {"id": "vc-linux-boot-mount", "type": "mount", "device": "vc-linux-boot-format",
         "path": "/boot"},
    ])
    for number, (name, mib) in enumerate(sizes, 3):
        volume = "vc-" + name
        actions.extend([
            {"id": volume, "type": "partition", "device": "vc-disk", "number": number,
             "size": -1 if mib is None else mib * 1048576, "wipe": "superblock", "preserve": False},
            {"id": volume + "-format", "type": "format", "volume": volume,
             "fstype": "swap" if name == "swap" else {{yaml .DataFilesystem}}, "preserve": False},
            {"id": volume + "-mount", "type": "mount", "device": volume + "-format",
             "path": "none" if name == "swap" else "/" if name == "root" else "/" + name},
        ])
    storage.pop("layout")
    storage["config"] = actions

    # Subiquity re-reads this file after early-commands. Preserve other settings
    # and replace atomically, so a failed rewrite cannot select an arbitrary disk.
    pending = None
    try:
        with tempfile.NamedTemporaryFile(
            mode="w", encoding="utf-8", dir=config_path.parent,
            prefix=".ventoy-autoinstall-", delete=False,
        ) as stream:
            pending = stream.name
            os.fchmod(stream.fileno(), 0o600)
            # Keep compatibility with the PyYAML bundled in Ubuntu 20.04.3.
            yaml.safe_dump(document, stream, default_flow_style=False)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(pending, config_path)
        pending = None
    finally:
        if pending is not None:
            os.unlink(pending)


if __name__ == "__main__":
    bind_install_disk("/autoinstall.yaml", sys.argv[1])
