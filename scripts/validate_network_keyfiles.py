"""Exercise the rendered Rocky 9 offline network fix against temporary files."""
import configparser
from pathlib import Path
import subprocess
import sys
import tempfile


def check_network_keyfiles(kickstart):
    source = kickstart.split("<<'VCNETWORK'\n", 1)[1].split("\nVCNETWORK", 1)[0]
    compile(source, "rendered-network-keyfiles.py", "exec")
    for mode in ("valid", "missing-selected", "malformed", "replace-failed"):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            selected = ("[connection]\nid=pxe\ntype=ethernet\nautoconnect-priority=-999\n"
                        "[ethernet]\nmac-address=02:00:00:00:01:01\n"
                        "[ipv4]\nmethod=auto\n")
            auto = ("[connection]\nid=usb0\ntype=ethernet\n"
                    "autoconnect-priority=-999\ninterface-name=usb0\n"
                    "[ethernet]\n[ipv4]\nmethod=auto\n")
            original = {"a-auto.nmconnection": auto,
                        "b-custom.nmconnection": auto.replace("-999", "50"),
                        "c-wifi.nmconnection": auto.replace("type=ethernet", "type=wifi"),
                        "d-disabled.nmconnection": auto + "\n[ipv6]\nmethod=disabled\n",
                        "z-selected.nmconnection": selected}
            original["d-disabled.nmconnection"] = original["d-disabled.nmconnection"].replace(
                "id=usb0\n", "id=usb0\nautoconnect=false\n")
            if mode == "missing-selected":
                del original["z-selected.nmconnection"]
            if mode == "malformed":
                original["z-selected.nmconnection"] = "invalid keyfile\n"
            for name, content in original.items():
                (root / name).write_text(content, encoding="utf-8")
            program = source
            if mode == "replace-failed":
                program = "import os\ndef fail(*args): raise OSError('fixture')\nos.replace = fail\n" + source
            result = subprocess.run([sys.executable, "-c", program, str(root)], capture_output=True, text=True)
            if mode != "valid":
                assert result.returncode != 0, mode
                assert {p.name: p.read_text(encoding="utf-8") for p in root.iterdir()} == original, mode
                continue
            assert result.returncode == 0, result.stderr
            for name in original:
                if name != "a-auto.nmconnection":
                    assert (root / name).read_text(encoding="utf-8") == original[name], name
            config = configparser.ConfigParser(interpolation=None)
            config.read(root / "a-auto.nmconnection")
            assert not config.getboolean("connection", "autoconnect")
            assert config["ipv4"]["method"] == "auto"
            before = {p.name: p.read_bytes() for p in root.iterdir()}
            result = subprocess.run([sys.executable, "-c", source, str(root)], capture_output=True, text=True)
            assert result.returncode == 0, result.stderr
            assert {p.name: p.read_bytes() for p in root.iterdir()} == before
