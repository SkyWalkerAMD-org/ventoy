"""Validate menu wrappers and exercise the client with mocked hardware/network.

Only generated test fixtures are read. No installer, network or disk operation
is executed by these tests.
"""
import ast
import base64
import copy
import importlib.util
import io
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
from unittest.mock import patch

from pykickstart.parser import KickstartParser
from pykickstart.version import makeVersion
import yaml
from validate_pxe_debian import check_debian_client, debian_client_source


root = Path(sys.argv[1]) / "pxe"
bash = os.environ.get("VENTOY_BASH") or shutil.which("bash")
if not bash:
    raise RuntimeError("bash required")


def shell(source):
    subprocess.run([bash, "-n"], input=source, text=True, check=True)


for profile, version in [("rhel-7", "RHEL7"), ("rhel-8", "RHEL8"), ("rhel-9", "RHEL9"), ("almalinux-8", "RHEL8"), ("almalinux-9", "RHEL9"), ("centos-7.9", "RHEL7"), ("centos-8.5", "RHEL8"), ("centos-stream-9", "RHEL9"), ("centos-stream-10", "RHEL10"), ("rocky-8", "RHEL8"), ("rocky-9", "RHEL9"), ("rocky-10", "RHEL10")] + [(p, None) for p in ("ubuntu-20.04", "ubuntu-22.04", "ubuntu-24.04", "ubuntu-24.10", "ubuntu-25.04", "ubuntu-26.04", "ubuntu-26.04-desktop")]:
    folder = root / profile
    if version:
        for name in ("kickstart.cfg", "selected.cfg"):
            handler = makeVersion(version)
            parser = KickstartParser(handler, followIncludes=False)
            parser.readKickstart(str(folder / name))
            for script in handler.scripts:
                shell(script.script)
    else:
        wrapper = yaml.safe_load((folder / "user-data").read_text(encoding="utf-8"))["autoinstall"]
        selected = yaml.safe_load((folder / "selected.cfg").read_text(encoding="utf-8"))["autoinstall"]
        assert selected["identity"]["username"] == "deployadmin"
        assert "early-commands" not in selected
        assert not selected.get("interactive-sections") and selected["shutdown"] == "reboot"
        for command in wrapper["early-commands"] + wrapper["error-commands"] + selected["late-commands"] + selected["error-commands"]:
            shell(command)
    shell((folder / "preinstall.sh").read_text(encoding="utf-8"))

    source = (folder / "client.py").read_text(encoding="utf-8")
    ast.parse(source)
    # The shared entry is self-contained; the embedded client must compile too.
    wrapper_text = (folder / ("kickstart.cfg" if version else "user-data")).read_text(encoding="utf-8")
    encoded = re.search(r"b64decode\(.+?([A-Za-z0-9+/=]{300,})", wrapper_text).group(1)
    ast.parse(base64.b64decode(encoded).decode())

for profile in ("debian-10", "debian-11", "debian-12", "debian-13"):
    folder = root / profile
    for name in ("preseed.cfg", "selected.cfg"):
        text = (folder / name).read_text(encoding="utf-8").replace("\\\n", " ")
        for line in text.splitlines():
            if line.startswith(("d-i partman/early_command string", "d-i preseed/late_command string")):
                shell(line.split(" string ", 1)[1])
    decoded = debian_client_source((folder / "preseed.cfg").read_text(encoding="utf-8"))
    shell(decoded)
    assert 'passwd/root-password-crypted password *' in (folder / "preseed.cfg").read_text(encoding="utf-8")
    assert 'python' not in decoded.lower().replace('python runtime', '')
    shell((folder / "preinstall.sh").read_text(encoding="utf-8"))
    check_debian_client((folder / "preseed.cfg").read_text(encoding="utf-8"))

desktop = root / 'ubuntu-22.04-desktop'
wrapper = (desktop / 'preseed.cfg').read_text(encoding='utf-8')
assert 'ubiquity/early_command string /bin/false' in wrapper
assert '/dev/ventoy-center-not-authorized' in wrapper
ast.parse((desktop / 'client.py').read_text(encoding='utf-8'))
shell((desktop / 'preinstall.sh').read_text(encoding='utf-8'))
selected = (desktop / 'selected.cfg').read_text(encoding='utf-8')
assert 'ubiquity/reboot boolean false' in selected and '/dev/ventoy-center-unverified' in selected

spec = importlib.util.spec_from_file_location("pxe_client", root / "ubuntu-22.04" / "client.py")
client = importlib.util.module_from_spec(spec)
spec.loader.exec_module(client)
mac = "02:00:00:00:01:01"

# Rocky 8's Python 3.6 installer uses an ASCII locale. Exercise actual response
# decoding and configuration writes with non-ASCII profile labels in that mode.
with tempfile.TemporaryDirectory(prefix="vc-pxe-encoding-") as encoding_work:
    probe = r'''
import importlib.util,json,pathlib,sys
spec=importlib.util.spec_from_file_location('client',sys.argv[1])
c=importlib.util.module_from_spec(spec);spec.loader.exec_module(c)
work=pathlib.Path(sys.argv[2]);content='# Rocky '+chr(0xff08)+chr(0x4e2d)+chr(0xff09)+'\n'
def response(args):
    (work/'response.json').write_bytes(json.dumps({'config':content},ensure_ascii=False).encode('utf-8'))
    return '200'
c.output=response
result=c.api(str(work),'install',{})
c.write(str(work/'selected.cfg'),result['config'])
assert (work/'selected.cfg').read_bytes()==content.encode('utf-8')
assert c.read(str(work/'selected.cfg'))==content.strip()
'''
    subprocess.run([sys.executable, "-c", probe, str(root / "rocky-8/client.py"), encoding_work],
                   env=dict(os.environ, LC_ALL="C", PYTHONUTF8="0", PYTHONCOERCECLOCALE="0"), check=True)

# Subiquity 23.08 re-reads the file with yaml.safe_load and looks up each
# controller's key directly. A cloud-init wrapper here silently chooses the
# default mirror, LVM layout and kernel instead of the confirmed job settings.
selected_source = (root / "ubuntu-22.04" / "selected.cfg").read_text(encoding="utf-8")
direct = yaml.safe_load(client.selected_config("autoinstall", selected_source))
expected = yaml.safe_load(selected_source)["autoinstall"]
assert direct == expected and direct["version"] == 1 and "autoinstall" not in direct
assert direct["storage"]["layout"]["match"]["path"] == "/dev/ventoy-center-unverified"
assert direct["apt"]["primary"][0]["uri"] == "https://archive.ubuntu.com/ubuntu"
assert direct["identity"]["username"] == "deployadmin" and direct["late-commands"]
for invalid in ("{}", "[]", "autoinstall: null", "autoinstall: {version: 2}",
                "autoinstall: {version: 1}", "autoinstall: {version: 1}\nusers: []"):
    try:
        client.selected_config("autoinstall", invalid)
    except RuntimeError:
        pass
    else:
        raise AssertionError("invalid configuration could select installer defaults")
assert client.selected_config("kickstart", "# kickstart\n") == "# kickstart\n"

# admin is supplied through cloud-init because Subiquity reserves this name.
admin_source = (root.parent / "password-policy" / "ubuntu-22.04" / "user-data").read_text(encoding="utf-8")
admin_config = yaml.safe_load(client.selected_config("autoinstall", admin_source))
assert "identity" not in admin_config
assert admin_config["user-data"]["users"][0]["name"] == "admin"
for users in ([], ["default"], [{}], [{"name": "admin"}]):
    broken = yaml.safe_load(admin_source)
    broken["autoinstall"]["user-data"]["users"] = users
    try:
        client.selected_config("autoinstall", yaml.safe_dump(broken))
    except RuntimeError:
        pass
    else:
        raise AssertionError("unusable administrator accepted")


def fake_output(args):
    if args[:2] == ["lsblk", "-bdnP"]:
        return 'NAME="sda" TYPE="disk" SIZE="137438953472" SERIAL="DISK-ONE" MODEL="Test Disk" TRAN="sata" RO="0" RM="0"'
    if "MOUNTPOINT" in args or "FSTYPE" in args:
        return ""
    raise AssertionError(args)


with patch.object(client, "output", side_effect=fake_output), patch.object(client, "read", return_value=mac), patch.object(client.glob, "glob", return_value=["/mock/address"]), patch.object(client.subprocess, "check_call"), patch.object(client.os.path, "isdir", return_value=True):
    inv = client.inventory(mac)
    assert inv["disks"][0]["blocked"] == ""
    assert inv["disks"][0]["serial"] == "DISK-ONE"
    assert inv["version"] == 2 and inv["disks"][0]["location"] == "internal"
    for blocked_case in ("MOUNTPOINT", "FSTYPE"):
        def blocked(args):
            if blocked_case in args:
                return "/target" if blocked_case == "MOUNTPOINT" else "LVM2_member"
            return fake_output(args)
        with patch.object(client, "output", side_effect=blocked):
            assert client.inventory(mac)["disks"][0]["blocked"]

    # Real RHEL 9 installer inventory contains zram0 TYPE=disk. Ignore only
    # verified RAM-backed nodes; an NBD or an unrecognized disk still blocks
    # implicit single-disk installation.
    for name, path, ignored in (
            ("zram0", "/sys/devices/virtual/block/zram0", True),
            ("ram0", "/sys/devices/virtual/block/ram0", True),
            ("nbd0", "/sys/devices/virtual/block/nbd0", False),
            ("zram0", "/sys/devices/platform/unrecognized/block/zram0", False)):
        def ram_inventory(args):
            value = fake_output(args)
            if args[:2] == ["lsblk", "-bdnP"]:
                value += '\nNAME="%s" TYPE="disk" SIZE="3044016128" SERIAL="" MODEL="" TRAN="" RO="0" RM="0"' % name
            return value
        def device_path(value):
            return path if value.endswith("/" + name) else "/sys/devices/pci0000:00/host0/block/sda"
        with patch.object(client, "output", side_effect=ram_inventory), patch.object(client.os.path, "realpath", side_effect=device_path):
            actual = client.inventory(mac)
            assert len(actual["disks"]) == (1 if ignored else 2)
            if ignored:
                assert client.single_internal_disk(actual) == "DISK-ONE"
            else:
                try: client.single_internal_disk(actual)
                except RuntimeError: pass
                else: raise AssertionError("unknown virtual disk was silently ignored")

# A hardware RAID volume with empty TRAN is internal when attached through PCI.
# USB, BMC virtual media and remote block devices are never automatic targets.
for transport, removable, path, location in [
        ("", "0", "/sys/devices/pci0000:00/0000:00:01.0/host0/target0:0:0/block/sda", "internal"),
        ("nvme", "0", "/sys/devices/pci0000:00/0000:00:01.0/nvme/nvme0/nvme0n1", "internal"),
        ("usb", "0", "/sys/devices/pci0000:00/usb1/1-1/block/sda", "external"),
        ("", "0", "/sys/devices/pci0000:00/usb1/1-1/block/sda", "external"),
        ("sata", "1", "/sys/devices/pci0000:00/block/sda", "external"),
        ("", "0", "/sys/devices/virtual/block/nbd0", "unknown"),
        ("nvme", "0", "/sys/devices/virtual/nvme-fabrics/ctl/nvme0n1", "unknown"),
        ("", "0", "/sys/devices/pci0000:00/host0/session1/block/sda", "unknown"),
        ("fc", "0", "/sys/devices/pci0000:00/host0/block/sda", "unknown"),
        ("", "0", "/sys/devices/platform/unrecognized/block/sda", "unknown")]:
    with patch.object(client.os.path, "realpath", return_value=path):
        assert client.disk_location(dict(NAME="sda", TRAN=transport, RM=removable)) == location

assert client.single_internal_disk(inv) == "DISK-ONE"
external = dict(name="sdb",serial="USB",model="USB disk",size=1000,blocked="USB",location="external")
assert client.single_internal_disk(dict(inv, disks=inv["disks"]+[external])) == "DISK-ONE"
for disks in ([], [external],
              [dict(inv["disks"][0],blocked="Mounted disk")],
              [dict(inv["disks"][0],serial="")],
              [dict(inv["disks"][0],location="unknown")],
              inv["disks"]+[dict(external,serial="DISK-ONE")],
              inv["disks"]+[dict(external,location="internal",blocked="LVM")],
              inv["disks"]+[dict(external,location="unknown")]):
    try:
        client.single_internal_disk(dict(inv,disks=disks))
    except RuntimeError:
        pass
    else:
        raise AssertionError("unsafe single-disk selection accepted")

console = io.StringIO()
with patch.object(client, "prompt", side_effect=["1", "yes"]):
    assert client.choose(inv, {}, console) == ("DISK-ONE", "ERASE DISK-ONE", None)
for values in (["1", "", "yes"], ["1", "y", "YES"]):
    retry_console = io.StringIO()
    with patch.object(client, "prompt", side_effect=values):
        assert client.choose(inv, {}, retry_console) == ("DISK-ONE", "ERASE DISK-ONE", None)
    assert "Nothing has been erased" in retry_console.getvalue()
for values in (["q"], ["99", "", "-1"], ["1", "q"], ["1", "no"], ["1", "NO"], ["1", "", "", ""], ["1", "y", "true", "1"]):
    with patch.object(client, "prompt", side_effect=values):
        try:
            client.choose(inv, {}, io.StringIO())
        except RuntimeError:
            pass
        else:
            raise AssertionError("invalid confirmation accepted")

# Real tty input must remain readable when two lines arrive together, as with
# KVM paste or a duplicated Enter. A buffered reader can consume the second
# line while select() waits on an empty fd for ten minutes.
if os.name == "posix":
    import pty
    master, slave = pty.openpty()
    real_select = client.select.select
    try:
        with patch.object(client, "CONSOLE_PATH", os.ttyname(slave)):
            terminal = client.Console()
        try:
            os.write(master, b"1\n\nyes\n")
            with patch.object(client.select, "select", side_effect=lambda r,w,e,t: real_select(r,w,e,1)):
                assert client.prompt(terminal) == "1"
                assert client.prompt(terminal) == ""
                assert client.prompt(terminal) == "yes"
        finally:
            terminal.close()
    finally:
        os.close(master)
        os.close(slave)

# A failed confirmation must never issue or claim an install job.
with patch.dict(client.SETTINGS, disk_policy="console"), patch.object(client, "boot_mac", return_value=mac), patch.object(client, "inventory", return_value=inv), patch.object(client.tempfile, "mkdtemp", return_value="/mock"), patch.object(client, "write"), patch.object(client, "api", return_value=dict(ticket="ticket", preset_disk="", disk_policy="console")) as request, patch.object(client, "reserve_console"), patch.object(client, "Console", return_value=io.StringIO()), patch.object(client, "prompt", side_effect=["1", "", "", ""]), patch.object(client.subprocess, "check_call") as execute, patch.object(client.os, "umask"):
    try:
        client.main()
    except RuntimeError as exc:
        assert "3 attempts" in str(exc)
    else:
        raise AssertionError("failed confirmation allowed installation")
    assert request.call_count == 1 and request.call_args[0][1] == "discover"
    execute.assert_called_once_with(["chvt", client.CONSOLE_VT])

# Neither a valid preset nor the single-internal policy may open an input TTY.
# Changes before issuing or claiming the job must stop the installation.
for scenario in ("automatic", "preset", "changed-before-issue", "changed-before-guard", "policy-mismatch", "target-mismatch", "multi-disk"):
    calls, writes = [], {}
    def fake_api(work, endpoint, body):
        calls.append(endpoint)
        if endpoint == "discover":
            return dict(ticket="ticket", preset_disk="DISK-ONE" if scenario == "preset" else "",
                        disk_policy="console" if scenario == "policy-mismatch" else "single-internal",
                        automatic_disk="WRONG-DISK" if scenario == "target-mismatch" else "DISK-ONE")
        assert body["disk_serial"] == "DISK-ONE" and body["confirmation"] == ""
        return dict(installer="autoinstall", config=selected_source, preinstall="test-guard", install_failure="pass\n")
    different = copy.deepcopy(inv)
    different["disks"].append(dict(external,location="internal",blocked="LVM"))
    inventories = [inv, inv, inv]
    if scenario == "changed-before-issue": inventories[1] = different
    if scenario == "changed-before-guard": inventories[2] = different
    if scenario in ("multi-disk", "preset"): inventories = [different] * 3
    with patch.dict(client.SETTINGS, disk_policy="single-internal"), patch.object(client, "boot_mac", return_value=mac), patch.object(client, "inventory", side_effect=inventories), patch.object(client.tempfile, "mkdtemp", return_value="/mock"), patch.object(client, "write", side_effect=lambda p, s: writes.update({p:s})), patch.object(client, "api", side_effect=fake_api), patch.object(client.subprocess, "check_call") as execute, patch.object(client.os, "unlink"), patch.object(client.os, "umask"), patch.object(client, "Console") as console, patch.object(client, "prompt") as prompt, patch.object(client, "reserve_console") as reserve:
        if scenario not in ("automatic", "preset"):
            try:
                client.main()
            except RuntimeError:
                pass
            else:
                raise AssertionError("unsafe unattended installation accepted")
            assert calls == (["discover", "install"] if scenario == "changed-before-guard" else ["discover"])
            assert not execute.called
        else:
            client.main()
            assert calls == ["discover", "install"]
            execute.assert_called_once_with(["/bin/bash", "/mock/preinstall.sh"])
            assert yaml.safe_load(writes["/autoinstall.yaml"]) == expected
            assert writes["/run/ventoy-center-install-failed.py"] == "pass\n"
        console.assert_not_called()
        prompt.assert_not_called()
        reserve.assert_not_called()

# The new policy requires no registration or pre-created job. Multiple disks
# use the real chooser; single disks must not open a terminal. Confirming the
# second disk must propagate that exact serial to the server and final guard.
for scenario in ("single", "second", "blocked-first", "bad-choice", "cancel", "blank", "changed", "mismatch"):
    first = dict(inv['disks'][0])
    second = dict(first, name='sdb', serial='SECOND', size=200000000000)
    if scenario == 'blocked-first': first['blocked'] = 'Existing LVM'
    local_inv = dict(inv, disks=[first] if scenario == 'single' else [first, second])
    assert client.automatic_disk(local_inv, 'single-or-console') == ('DISK-ONE' if scenario == 'single' else '')
    calls, writes = [], {}
    def local_api(work, endpoint, body):
        calls.append(endpoint)
        if endpoint == 'discover':
            return dict(ticket='ticket', preset_disk='', disk_policy='single-or-console',
                        automatic_disk='DISK-ONE' if scenario in ('single', 'mismatch') else '')
        assert body['disk_serial'] == ('DISK-ONE' if scenario == 'single' else 'SECOND')
        assert body['confirmation'] == ('' if scenario == 'single' else 'ERASE SECOND')
        return dict(installer='autoinstall', config=selected_source, preinstall='test-guard', install_failure='pass\n')
    answers = ['2', 'yes']
    if scenario == 'bad-choice': answers = ['', '99', '2', 'yes']
    if scenario == 'blocked-first': answers = ['1', '2', 'yes']
    if scenario == 'cancel': answers = ['2', 'no']
    if scenario == 'blank': answers = ['2', '', '', '']
    inventories = [local_inv] * 3
    if scenario == 'changed': inventories[1] = dict(local_inv, disks=[second])
    success = scenario in ('single', 'second', 'blocked-first', 'bad-choice')
    with patch.dict(client.SETTINGS, disk_policy='single-or-console'), patch.object(client, 'boot_mac', return_value=mac), patch.object(client, 'inventory', side_effect=inventories), patch.object(client.tempfile, 'mkdtemp', return_value='/mock'), patch.object(client, 'write', side_effect=lambda p,s: writes.update({p:s})), patch.object(client, 'api', side_effect=local_api), patch.object(client.subprocess, 'check_call') as execute, patch.object(client.subprocess, 'call'), patch.object(client.os, 'unlink'), patch.object(client.os, 'umask'), patch.object(client, 'Console', return_value=io.StringIO()) as console, patch.object(client, 'prompt', side_effect=answers) as prompt, patch.object(client, 'reserve_console'):
        if success:
            client.main()
            assert calls == ['discover', 'install']
            assert execute.call_args_list[-1].args[0] == ['/bin/bash','/mock/preinstall.sh']
        else:
            try: client.main()
            except RuntimeError: pass
            else: raise AssertionError('unsafe local selection accepted: ' + scenario)
            assert calls == ['discover'], (scenario, calls)
        if scenario == 'single':
            console.assert_not_called(); prompt.assert_not_called()
raid_inv = dict(inv, disks=[dict(inv['disks'][0], size=128*1024**3),
                           dict(inv['disks'][0], name='sdb', serial='SECOND', size=128*1024**3),
                           dict(inv['disks'][0], name='sdc', serial='KEEP', size=128*1024**3)])
with patch.object(client, 'prompt', side_effect=['r','1 2','yes']):
    serial, confirmation, plan = client.choose(raid_inv, {'disk_policy':'single-or-console'}, io.StringIO())
    assert serial == 'DISK-ONE' and confirmation == 'ERASE RAID1 DISK-ONE SECOND'
    assert plan == {'mode':'raid1-lvm','disk_serials':['DISK-ONE','SECOND']}
for answers in (['1 1']*3, ['1 2','no'], ['1 2','','',''], ['1 2 3']*3):
    with patch.object(client, 'prompt', side_effect=answers):
        try: client.choose_raid(raid_inv, io.StringIO())
        except RuntimeError: pass
        else: raise AssertionError('unsafe RAID selection accepted')
for field, value in [('blocked','Mounted disk'), ('location','external'), ('size',1), ('serial','DISK-ONE')]:
    bad=copy.deepcopy(raid_inv);bad['disks'][1][field]=value
    with patch.object(client,'prompt',side_effect=['1 2']*3):
        try: client.choose_raid(bad,io.StringIO())
        except RuntimeError: pass
        else: raise AssertionError('unsafe RAID member accepted: '+field)
print('PXE wrappers, automatic single disk, local disk/RAID selection, exact confirmation, cancellation and changed hardware verified')
