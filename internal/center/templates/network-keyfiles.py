# Anaconda copies automatically generated, low-priority connections for other
# NICs into the installed system. A BMC USB NIC with no DHCP server can then
# delay network-online. Keep those profiles available for manual activation.
import configparser
import io
import os
from pathlib import Path
import sys
import tempfile

selected_mac = {{sh .Machine.MAC}}.lower()
directory = Path(sys.argv[1])
profiles = []
selected_found = False
for path in sorted(directory.glob("*.nmconnection")):
    if path.is_symlink() or not path.is_file():
        raise RuntimeError("Expected a regular NetworkManager keyfile")
    config = configparser.ConfigParser(interpolation=None)
    config.optionxform = str
    config.read_string(path.read_text(encoding="utf-8"))
    if config.get("connection", "type", fallback="") not in ("ethernet", "802-3-ethernet"):
        continue
    mac = config.get("ethernet", "mac-address", fallback="").lower()
    selected_found |= mac == selected_mac
    if (mac != selected_mac
            and config.get("connection", "autoconnect-priority", fallback="") == "-999"
            and config.get("connection", "autoconnect", fallback="true") != "false"):
        config.set("connection", "autoconnect", "false")
        content = io.StringIO()
        config.write(content, space_around_delimiters=False)
        profiles.append((path, content.getvalue()))

# Validate the selected, MAC-bound connection before changing any other file.
if not selected_found:
    raise RuntimeError("Selected MAC-bound NetworkManager connection is missing")
for path, content in profiles:
    fd, temporary = tempfile.mkstemp(prefix=".ventoy-network-", dir=str(directory))
    try:
        with os.fdopen(fd, "w", encoding="utf-8", newline="\n") as stream:
            stream.write(content)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)
print("Retained %d installer-generated Ethernet profiles for manual activation" % len(profiles))
