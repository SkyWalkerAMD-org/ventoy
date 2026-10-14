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
    if not isinstance(storage, dict) or set(storage) != {"layout"}:
        raise ValueError("Unexpected storage configuration")
    layout = storage["layout"]
    if layout != {"name": "direct", "match": {"path": "/dev/ventoy-center-unverified"}}:
        raise ValueError("Unexpected storage layout; refusing to select a disk")
    layout["match"] = {"path": "/dev/" + disk}

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
