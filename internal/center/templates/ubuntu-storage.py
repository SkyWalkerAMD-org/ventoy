import os
from pathlib import Path
import re
import sys
import tempfile

import yaml


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
    layout["match"] = {"path": "/dev/" + disk}
{{if eq .Target.ID "ubuntu-22.04"}}
    # Subiquity 23.08.1 ignores storage.grub when using a guided layout.
    # Use explicit actions for this verified disk so the EFI policy reaches
    # Curtin. This only changes YAML; the one-use claim still precedes writes.
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
        {"id": "vc-root", "type": "partition", "device": "vc-disk",
         "number": 2, "size": -1, "wipe": "superblock", "preserve": False},
        {"id": "vc-root-format", "type": "format", "volume": "vc-root",
         "fstype": "ext4", "preserve": False},
        {"id": "vc-root-mount", "type": "mount", "device": "vc-root-format",
         "path": "/"},
    ])
    storage.pop("layout")
    storage["config"] = actions
{{end}}

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
            yaml.safe_dump(document, stream, sort_keys=False)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(pending, config_path)
        pending = None
    finally:
        if pending is not None:
            os.unlink(pending)


if __name__ == "__main__":
    bind_install_disk("/autoinstall.yaml", sys.argv[1])
