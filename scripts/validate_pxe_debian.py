"""Run the actual generated d-i shell client against disposable hardware and API fixtures."""
import base64
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile


def debian_client_source(wrapper):
    encoded = re.search(r"printf '%s' '([A-Za-z0-9+/=]+)'", wrapper)
    if encoded:
        return base64.b64decode(encoded.group(1)).decode()
    literal = re.search(r"printf '%b' '([^']*)'", wrapper)
    assert literal, 'Missing embedded native Debian client'
    # Debian 10 uses BusyBox printf instead of its unavailable base64 applet.
    return re.sub(r"\\0([0-7]{3})", lambda m: chr(int(m.group(1), 8)), literal.group(1))


def check_debian_client(wrapper):
    source = debian_client_source(wrapper)
    if os.name == "nt":
        return
    for scenario in ("single", "xfs", "bad-fs", "cancel-fs", "usb", "multiple", "second-blocked", "unknown", "mounted", "swap", "readonly", "raid", "preset", "changed", "mismatch", "select-second", "bad-choice", "cancel", "blank", "blocked-choice", "local-changed", "local-mismatch", "legacy-multiple", "no-stty", "raid-pair", "raid-duplicate", "raid-cancel", "raid-small", "raid-readonly"):
        with tempfile.TemporaryDirectory(prefix="vc-debian-") as directory:
            root = Path(directory)
            devices = root / "devices/pci0000:00"
            blocks = root / "sys/block"
            blocks.mkdir(parents=True)
            proc = root / "proc/self"
            proc.mkdir(parents=True)
            (proc / "mountinfo").write_text("")
            (root / "proc/swaps").write_text("Filename Type Size Used Priority\n")
            (root / "proc/cmdline").write_text("BOOTIF=01-02-00-00-00-01-01\n")
            net = root / "sys/class/net/eth0"
            net.mkdir(parents=True)
            (net / "address").write_text("02:00:00:00:01:01\n")
            def disk(name, serial, external=False):
                p = devices / ("usb1/1-1" if external else name) / name
                p.mkdir(parents=True)
                for field, value in {"size":"200000", "dev":"8:0" if name == "sda" else "8:16", "removable":"1" if external else "0", "ro":"0", "serial":serial}.items():
                    (p / field).write_text(value+"\n")
                (blocks / name).symlink_to(p)
                return p
            first = disk("sda", "FIRST")
            multi = scenario.startswith('raid-') or scenario in ("multiple", "second-blocked", "preset", "usb", "select-second", "bad-choice", "cancel", "blank", "blocked-choice", "local-changed", "local-mismatch", "legacy-multiple", "no-stty")
            if multi:
                second = disk("sdb", "SECOND", scenario == "usb")
                if scenario in ("second-blocked", "blocked-choice"): (second / "ro").write_text("1\n")
            if scenario.startswith('raid-'):
                (first/'size').write_text('268435456\n')
                if scenario!='raid-small': (second/'size').write_text('268435456\n')
                if scenario=='raid-readonly': (second/'ro').write_text('1\n')
            if scenario == "readonly": (first / "ro").write_text("1\n")
            if scenario == "unknown":
                (blocks / "sda").unlink()
                unknown = root / "unknown/sda";unknown.parent.mkdir()
                first.rename(unknown); (blocks / "sda").symlink_to(unknown)
            if scenario == "mounted": (proc / "mountinfo").write_text("1 2 8:0 / /target rw - ext4 /dev/sda rw\n")
            if scenario == "swap": (root / "proc/swaps").write_text("Filename Type Size Used Priority\n/dev/sda partition 1024 0 -2\n")
            bindir = root / "bin";bindir.mkdir()
            commands = {
                "anna-install":"exit 0",
                "udevadm":'case "$1" in settle) exit 0;; esac\nprintf \'ID_MODEL=Model "quoted"\\nID_BUS=ata\\n\'',
                "blkid":'test "$SCENARIO" != raid || echo linux_raid_member',
                # cdebconf sends protocol commands on inherited fd 3 and keeps
                # original stdio on 4-6. The local chooser must preserve these.
                "debconf-set-selections":'''set -eu
printf 'SET partman-auto/disk\\n' >&3
IFS= read -r original <&4
test "$original" = preserved-input
printf 'preserved-output\\n' >&5
printf 'preserved-error\\n' >&6
cp "$1" "$ROOT/applied.cfg"''',
                "debconf-get":'printf \'touch "%s/guard-passed"\\n\' "$ROOT"',
                "stty":"exit 0",
                "chvt":'test "$SCENARIO" != local-changed || echo 1 > "$ROOT/sys/block/sda/ro"',
                "wget":'''if [ "$1" = --version ]; then echo 'GNU Wget'; exit 0; fi
for arg; do
  case "$arg" in --post-file=*) request=${arg#--post-file=};; */discover) endpoint=discover;; */install) endpoint=install;; esac
done
while [ "$1" != -O ]; do shift; done; response=$2
cp "$request" "$ROOT/$endpoint.json"
if [ "$endpoint" = discover ]; then
  printf 'ticket=%s\\npreset_disk=%s\\nautomatic_disk=%s\\ndisk_policy=%s\\n' aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa "$PRESET" "$AUTOMATIC" "$POLICY" > "$response"
  if [ "$SCENARIO" = changed ]; then echo 1 > "$ROOT/sys/block/sda/ro"; fi
else
  printf 'd-i partman-auto/disk string /dev/ventoy-center-not-authorized\\n' > "$response"
fi''',
            }
            for name, content in commands.items():
                p=bindir/name;p.write_text("#!/bin/sh\n"+content+"\n");p.chmod(0o755)
            script = source.replace("/sys/", str(root/"sys")+"/").replace("/proc/",str(root/"proc")+"/")
            # Keep all temporary files in the disposable fixture; no disks or network are used.
            script=script.replace("/tmp/ventoy-pxe.XXXXXX",str(root/"work.XXXXXX"))
            script=script.replace("test -s \"$work/ca.crt\"", ":")
            if scenario == 'no-stty':
                # The real Debian 10 installer BusyBox omits this applet.
                script=script.replace('command -v stty', 'command -v missing-ssi-stty')
                (bindir/'stty').write_text('#!/bin/sh\nexit 89\n')
                script=script.replace('command -v chvt', 'command -v missing-ssi-chvt')
                (bindir/'chvt').write_text('#!/bin/sh\nexit 89\n')
            answers = '2\nyes\n' if scenario in ('select-second','local-changed','local-mismatch') else '1\nyes\n'
            if scenario == 'bad-choice': answers='\n99\n2\nyes\n'
            if scenario == 'cancel': answers='2\nno\n'
            if scenario == 'blank': answers='2\n\n\n\n'
            if scenario == 'blocked-choice': answers='2\n2\n2\n'
            if scenario=='raid-pair':answers='r\n1 2\nyes\n'
            if scenario=='raid-cancel':answers='r\n1 2\nno\n'
            if scenario=='raid-duplicate':answers='r\n1 1\n1 1\n1 1\n'
            if scenario in ('raid-small','raid-readonly'):answers='r\n1 2\n1 2\n1 2\n'
            answers = '\n' + answers
            if scenario == 'xfs': answers='2\n'
            if scenario == 'bad-fs': answers='ntfs\next4;reboot\nbogus\n'
            if scenario == 'cancel-fs': answers='q\n'
            (root/'console-in').write_text(answers)
            script=script.replace('8</dev/tty9 9>/dev/tty9', '8<"%s" 9>"%s"' % (root/'console-in',root/'console-out'))
            automatic = '' if multi and scenario not in ('usb','preset','local-mismatch') else 'FIRST'
            if scenario == 'mismatch': automatic='WRONG'
            env=dict(os.environ,ROOT=str(root),SCENARIO=scenario,PRESET="FIRST" if scenario=="preset" else "",AUTOMATIC=automatic,POLICY='single-internal' if scenario=='legacy-multiple' else 'single-or-console',PATH=str(bindir)+os.pathsep+os.environ['PATH'])
            (root/'client.sh').write_text(script+'\nprintf "CLIENT_DONE\\n" >&3\n')
            (root/'original-input').write_text('preserved-input\n')
            runner='exec 3>"$ROOT/protocol" 4<"$ROOT/original-input" 5>"$ROOT/original-output" 6>"$ROOT/original-error"; exec /bin/sh "$ROOT/client.sh"'
            result=subprocess.run(['/bin/sh','-c',runner],text=True,capture_output=True,env=env,timeout=15)
            inv=json.loads((root/'discover.json').read_text())
            assert inv['disks'][0]['serial']=='FIRST' and inv['disks'][0]['model']=='Model "quoted"', inv
            success=scenario in ('single','xfs','usb','preset','multiple','second-blocked','select-second','bad-choice','no-stty','raid-pair')
            assert (result.returncode==0)==success, (scenario,result.stdout,result.stderr)
            assert (root/'guard-passed').exists()==success, scenario
            if success:
                assert (root/'protocol').read_text()=='SET partman-auto/disk\nCLIENT_DONE\n', scenario
                assert (root/'original-output').read_text()=='preserved-output\n', scenario
                assert (root/'original-error').read_text()=='preserved-error\n', scenario
                # Inventory runs multiple times; the last disk must never overwrite the preset serial.
                installed=json.loads((root/'install.json').read_text())
                assert installed['filesystem'] == ('xfs' if scenario == 'xfs' else 'ext4')
                assert installed['disk_serial']==('SECOND' if scenario in ('select-second','bad-choice') else 'FIRST')
                if scenario=='raid-pair':
                    assert installed['confirmation']=='ERASE RAID1 FIRST SECOND'
                    assert installed['storage_plan']=={'mode':'raid1-lvm','disk_serials':['FIRST','SECOND']}
                else:
                    assert installed['confirmation']==('ERASE '+installed['disk_serial'] if multi and scenario not in ('usb','preset') else '')
            else:
                assert not (root/'install.json').exists(), scenario
    print('Debian native client: unattended single disk, local multi-disk choices, yes/no, protected disks and changed hardware verified')
