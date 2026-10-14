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
                assert yaml.safe_load(path.read_text(encoding="utf-8")) == expected
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
