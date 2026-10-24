"""Exercise actual read-only storage guard against isolated sysfs/LVM/MD fixtures."""
import os
from pathlib import Path
import subprocess
import tempfile


def check_raid_guard(source):
    start=source.index('# Read-only storage scope verification')
    end=source.index('echo "Ventoy storage: two-disk rebuild scope verified:')
    end=source.index('\n',end)+1
    source=source[start:end]
    for case in ('blank','both-pvs','third-pv','missing-pv','hidden-pv','vg-collision',
                 'mounted','swap','readonly','external','duplicate','md-third','md-both','md-no-uuid'):
        with tempfile.TemporaryDirectory(prefix='vc-raid-guard-') as directory:
            root=Path(directory)
            for name in ('dev','proc/self','sys/block','sys/class/block','bin','work'):
                (root/name).mkdir(parents=True,exist_ok=True)
            nodes={}
            for index,name in enumerate(('sda','sdb','sdc')):
                device=root/'sys/devices'/('usb1' if case=='external' and name=='sdb' else 'pci0000:00')/name
                device.mkdir(parents=True)
                (root/'sys/block'/name).symlink_to(device)
                (root/'sys/class/block'/name).symlink_to(device)
                nodes[name]=device
                for key,value in {'ro':'1' if case=='readonly' and name=='sdb' else '0','removable':'0',
                                  'size':'268435456','dev':'8:'+str(index*16),
                                  'serial':'QA-FIRST' if index==0 or case=='duplicate' and index==1 else 'QA-SECOND' if index==1 else 'KEEP'}.items():
                    (device/key).write_text(value+'\n')
                part=device/(name+'1');part.mkdir();nodes[name+'1']=part
                (root/'sys/class/block'/(name+'1')).symlink_to(part)
                (part/'partition').write_text('1\n');(part/'dev').write_text('8:'+str(index*16+1)+'\n')
                (root/'dev'/name).touch();(root/'dev'/(name+'1')).touch()
            (root/'proc/self/mountinfo').write_text('1 2 8:1 / /target rw - ext4 /dev/sda1 rw\n' if case=='mounted' else '')
            (root/'proc/swaps').write_text('Filename Type Size Used Priority\n'+(str(root/'dev/sdb1')+' partition 1024 0 -2\n' if case=='swap' else ''))
            pvs=[]
            if case in ('both-pvs','third-pv','missing-pv'):
                pvs=[str(root/'dev/sda1')+'|oldvg',str(root/('dev/sdc1' if case=='third-pv' else 'dev/sdb1'))+'|oldvg']
                if case=='missing-pv':pvs.append('[unknown]|oldvg')
            if case=='vg-collision':pvs=[str(root/'dev/sdc1')+'|vcvg']
            (root/'pv-list').write_text('\n'.join(pvs)+'\n')
            tools={
                'udevadm':'exit 0',
                'pvs':'cat "$ROOT/pv-list"',
                'blkid':'''case "$1" in
-c) printf '%s\\n' "$ROOT/dev/sda1" "$ROOT/dev/sdb1"; test "$CASE" != md-third || printf '%s\\n' "$ROOT/dev/sdc1"; exit 0;;
esac
for arg; do last=$arg; done
case "$CASE:$last" in
hidden-pv:*/sda1|both-pvs:*/sda1|third-pv:*/sda1|missing-pv:*/sda1) echo LVM2_member;;
md-*:*/sda1|md-*:*/sdb1|md-third:*/sdc1) echo linux_raid_member;;
esac''',
                'mdadm':'test "$CASE" = md-no-uuid || echo MD_UUID=fixture-array',
            }
            for name,body in tools.items():
                path=root/'bin'/name;path.write_text('#!/bin/sh\n'+body+'\n');path.chmod(0o700)
            # Substitute regular fixture files for device predicates only in this
            # sandbox. No real block device, partitioner, mount or network is used.
            script=source.replace('/sys/',str(root/'sys')+'/').replace('/proc/',str(root/'proc')+'/').replace('/dev/',str(root/'dev')+'/')
            script=script.replace('/tmp/ventoy-storage.XXXXXX',str(root/'work'/'guard.XXXXXX'))
            script=script.replace('[ -b ', '[ -f ')
            result=subprocess.run(['/bin/bash','-eu'],input=script,text=True,capture_output=True,
                env=dict(os.environ,ROOT=str(root),CASE=case,PATH=str(root/'bin')+':/usr/bin:/bin'),timeout=10)
            assert (result.returncode==0)==(case in ('blank','both-pvs','md-both')), (case,result.stdout,result.stderr)
    print('Storage guard: complete old LVM/MD membership, third/missing/hidden members, mounts, swap and disk identity verified')
