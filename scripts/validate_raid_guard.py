"""Exercise storage scope and claimed preparation against isolated sysfs/LVM/MD fixtures."""
import os
from pathlib import Path
import subprocess
import tempfile


def check_raid_guard(source, single=False):
    full_source=source
    start=source.index('# Read-only storage scope verification')
    end=source.index('echo "Ventoy storage: '+('single-disk' if single else 'two-disk')+' rebuild scope verified:')
    end=source.index('\n',end)+1
    source=source[start:end]
    cases = ('blank','single-pv','unrelated-vg','both-pvs','third-pv','missing-pv','hidden-pv','vg-collision',
             'same-name-vgs','same-name-destination','cloned-vg','missing-vg-uuid','orphan-pv','pvs-failed',
             'mounted','swap','mounted-lv','swap-lv','holder-third','readonly','external','duplicate',
             'md-third','md-both','md-no-uuid','md-scan-failed',
             'prepare-allowed','claim-denied','storage-changed','rename-failed','rename-collision',
             'swap-prepare','swap-and-rename','swap-claim-denied','swap-failed','swap-still-active','swap-new-mount','swap-new-member','swap-device-changed')
    for case in cases:
        with tempfile.TemporaryDirectory(prefix='vc-raid-guard-') as directory:
            root=Path(directory)
            for name in ('dev','proc/self','sys/block','sys/class/block','bin','work'):
                (root/name).mkdir(parents=True,exist_ok=True)
            nodes={}
            for index,name in enumerate(('sda','sdb','sdc')):
                affected='sda' if single else 'sdb'
                device=root/'sys/devices'/('usb1' if case=='external' and name==affected else 'pci0000:00')/name
                device.mkdir(parents=True)
                (root/'sys/block'/name).symlink_to(device)
                (root/'sys/class/block'/name).symlink_to(device)
                nodes[name]=device
                for key,value in {'ro':'1' if case=='readonly' and name==affected else '0','removable':'0',
                                  'size':'268435456','dev':'8:'+str(index*16),
                                  'serial':'QA-FIRST' if index==0 or case=='duplicate' and index==1 else 'QA-SECOND' if index==1 else 'KEEP'}.items():
                    (device/key).write_text(value+'\n')
                part=device/(name+'1');part.mkdir();nodes[name+'1']=part
                (root/'sys/class/block'/(name+'1')).symlink_to(part)
                (part/'partition').write_text('1\n');(part/'dev').write_text('8:'+str(index*16+1)+'\n')
                (root/'dev'/name).touch();(root/'dev'/(name+'1')).touch()
            if case in ('mounted-lv','swap-lv','holder-third','swap-new-member'):
                device=root/'sys/devices/virtual/block/dm-0'
                (device/'slaves').mkdir(parents=True)
                (device/'dev').write_text('253:0\n')
                (root/'sys/class/block/dm-0').symlink_to(device)
                (root/'dev/dm-0').touch()
                for member in (('sda1','sdc1') if case=='holder-third' else ('sda1',)):
                    (nodes[member]/'holders').mkdir()
                    (nodes[member]/'holders/dm-0').symlink_to(device)
                    (device/'slaves'/member).symlink_to(nodes[member])
            mounted = '253:0' if case=='mounted-lv' else '8:1'
            (root/'proc/self/mountinfo').write_text('1 2 '+mounted+' / /target rw - ext4 /dev/test rw\n' if case in ('mounted','mounted-lv') else '')
            swap = 'dm-0' if case=='swap-lv' else affected+'1'
            has_swap=case=='swap' or case.startswith('swap-')
            swap_text='Filename Type Size Used Priority\n'
            if has_swap:
                swap_text+=str(root/('dev/'+swap))+' partition 1024 0 -2\n'
                swap_text+=str(root/'dev/sdc1')+' partition 1024 0 -3\n'
                # An unrelated swapfile can share a selected device's basename.
                # Only canonical /dev paths belong to this block-device plan.
                (root/'unselected').mkdir()
                (root/'unselected'/swap).touch()
                swap_text+=str(root/'unselected'/swap)+' file 1024 0 -4\n'
            (root/'proc/swaps').write_text(swap_text)
            pvs=[]
            if case in ('both-pvs','third-pv','missing-pv'):
                pvs=[str(root/'dev/sda1')+'|oldvg',str(root/('dev/sdc1' if case=='third-pv' else 'dev/sdb1'))+'|oldvg']
                if case=='missing-pv':pvs.append('[unknown]|oldvg')
            if case=='vg-collision':pvs=[str(root/'dev/sdc1')+'|vcvg']
            if case in ('single-pv','unrelated-vg'):
                pvs=[str(root/'dev/sda1')+'|oldvg']
                if case=='unrelated-vg':pvs.append(str(root/'dev/sdc1')+'|keepvg')
            # Real pvs can report independent VGs with identical names. Return
            # the requested fields, so the old name-only guard fails this test.
            pvs=[row+'|uuid-'+row.split('|')[1] for row in pvs]
            prepare_cases=('prepare-allowed','claim-denied','storage-changed','rename-failed','swap-and-rename')
            swap_prepare_cases=tuple(c for c in cases if c.startswith('swap-') and c!='swap-lv')
            if case in ('same-name-vgs','same-name-destination','cloned-vg','rename-collision')+prepare_cases:
                name='vcvg' if case=='same-name-destination' else 'oldvg'
                pvs=[str(root/'dev/sda1')+'|'+name+'|uuid-first',
                     str(root/'dev/sdc1')+'|'+name+('|uuid-first' if case=='cloned-vg' else '|uuid-keep')]
                if case=='rename-collision':pvs.append(str(root/'dev/sdb1')+'|vc_rebuild_uuidfirst|uuid-busy')
            if case=='missing-vg-uuid':pvs=[str(root/'dev/sda1')+'|oldvg|']
            if case=='orphan-pv':pvs=[str(root/'dev/sda1')+'||']
            (root/'pv-list').write_text('\n'.join(pvs)+'\n')
            tools={
                'udevadm':'exit 0',
                'pvs':'''[ "$CASE" != pvs-failed ] || exit 5
case "$*" in
*vg_uuid*) cat "$ROOT/pv-list";;
*) cut -d '|' -f 1,2 "$ROOT/pv-list";;
esac''',
                'blkid':'''case "$1" in
-c) [ "$CASE" != md-scan-failed ] || exit 2; printf '%s\\n' "$ROOT/dev/sda1" "$ROOT/dev/sdb1"; test "$CASE" != md-third || printf '%s\\n' "$ROOT/dev/sdc1"; exit 0;;
esac
for arg; do last=$arg; done
case "$CASE:$last" in
hidden-pv:*/sda1|single-pv:*/sda1|unrelated-vg:*/sda1|both-pvs:*/sda1|third-pv:*/sda1|missing-pv:*/sda1|same-name-*:*/sda1|cloned-vg:*/sda1|missing-vg-uuid:*/sda1|orphan-pv:*/sda1) echo LVM2_member;;
md-*:*/sda1|md-*:*/sdb1|md-third:*/sdc1) echo linux_raid_member;;
esac''',
                'mdadm':'test "$CASE" = md-no-uuid || echo MD_UUID=fixture-array',
                'curl':'''printf 'claim\\n' >> "$ROOT/events"
case "$CASE" in claim-denied|swap-claim-denied) exit 22;; esac
if [ "$CASE" = storage-changed ]; then printf '%s|newvg|uuid-new\\n' "$ROOT/dev/sdb1" >> "$ROOT/pv-list"; fi
if [ "$CASE" = swap-new-mount ]; then echo '1 2 8:1 / /target rw - ext4 test rw' > "$ROOT/proc/self/mountinfo"; fi
if [ "$CASE" = swap-new-member ]; then ln -s "$ROOT/sys/class/block/sdc1" "$ROOT/sys/class/block/dm-0/slaves/sdc1"; fi
if [ "$CASE" = swap-device-changed ]; then echo '8:99' > "$ROOT/sys/class/block/sda1/dev"; fi''',
                'swapoff':'''printf '%s\\n' "$1" >> "$ROOT/released"
printf 'swapoff\\n' >> "$ROOT/events"
[ "$CASE" != swap-failed ] || exit 1
if [ "$CASE" != swap-still-active ]; then
    sed "\\|^$1 |d" "$ROOT/proc/swaps" > "$ROOT/new-swaps"
    mv "$ROOT/new-swaps" "$ROOT/proc/swaps"
fi''',
                'vgrename':'''printf '%s|%s\\n' "$1" "$2" >> "$ROOT/renamed"
printf 'rename\\n' >> "$ROOT/events"
[ "$CASE" != rename-failed ]''',
            }
            for name,body in tools.items():
                path=root/'bin'/name;path.write_text('#!/bin/sh\n'+body+'\n');path.chmod(0o700)
            # Substitute regular fixture files for device predicates only in this
            # sandbox. No real block device, partitioner, mount or network is used.
            script=source
            if case in prepare_cases+swap_prepare_cases:
                # Execute the actual rendered one-use claim and prepare order.
                claim=full_source.index('# CentOS 7\'s installer omits base64')
                end=full_source.index('\nvc_storage_prepare\n',claim)+len('\nvc_storage_prepare\n')
                script+=full_source[claim:end]
            script=script.replace('/sys/',str(root/'sys')+'/').replace('/proc/',str(root/'proc')+'/').replace('/dev/',str(root/'dev')+'/')
            script=script.replace('/tmp/ventoy-storage.XXXXXX',str(root/'work'/'guard.XXXXXX'))
            script=script.replace('/tmp/ventoy-center-ca.crt',str(root/'work'/'ca.crt'))
            script=script.replace('[ -b ', '[ -f ')
            result=subprocess.run(['/bin/bash','-eu'],input=script,text=True,capture_output=True,
                env=dict(os.environ,ROOT=str(root),CASE=case,PATH=str(root/'bin')+':/usr/bin:/bin'),timeout=10)
            allowed=('blank','single-pv','unrelated-vg','vg-collision','same-name-vgs','same-name-destination','orphan-pv','prepare-allowed') if single else ('blank','single-pv','unrelated-vg','both-pvs','md-both','same-name-vgs','orphan-pv','prepare-allowed')
            allowed+=('swap','swap-lv','swap-prepare','swap-and-rename')
            assert (result.returncode==0)==(case in allowed), (single,case,result.stdout,result.stderr)
            reasons = {
                'missing-vg-uuid': 'The old volume group identity is missing',
                'pvs-failed': 'Cannot inspect existing volume groups',
                'mounted-lv': 'Selected storage is mounted',
                'holder-third': 'Existing storage spans an unselected disk',
                'cloned-vg': 'Existing storage spans an unselected disk',
                'md-scan-failed': 'Cannot enumerate RAID members',
                'storage-changed': 'Storage changed after verification',
                'rename-failed': 'Cannot disambiguate the selected old volume group',
                'rename-collision': 'Temporary rebuild volume group name is already in use',
                'swap-failed': 'Cannot release selected old swap',
                'swap-still-active': 'Selected swap remains active',
                'swap-new-mount': 'Selected storage is mounted',
                'swap-new-member': 'Existing storage spans an unselected disk',
                'swap-device-changed': 'Storage changed after verification',
            }
            if case in reasons:
                assert reasons[case] in result.stderr, (case,result.stderr)
            if case=='same-name-destination' and not single:
                assert 'destination volume group name belongs to unselected storage' in result.stderr
            if case in ('prepare-allowed','rename-failed','swap-and-rename'):
                assert (root/'renamed').read_text()=='uuid-first|vc_rebuild_uuidfirst\n'
                assert (root/'events').read_text()==('claim\nswapoff\nrename\n' if case=='swap-and-rename' else 'claim\nrename\n')
            else:
                assert not (root/'renamed').exists(), (case,'unexpected metadata write')
            if case in ('swap-prepare','swap-and-rename','swap-failed','swap-still-active'):
                assert (root/'released').read_text()==str(root/('dev/'+swap))+'\n'
                assert (root/'events').read_text()==('claim\nswapoff\nrename\n' if case=='swap-and-rename' else 'claim\nswapoff\n')
            else:
                assert not (root/'released').exists(), (case,'unexpected swap release')
            if has_swap:
                assert str(root/'dev/sdc1')+' partition' in (root/'proc/swaps').read_text(), 'Unselected swap was released'
                assert str(root/'unselected'/swap)+' file' in (root/'proc/swaps').read_text(), 'Unselected swapfile was released'
            if case in ('swap','swap-lv','swap-claim-denied','swap-failed'):
                assert (root/'proc/swaps').read_text()==swap_text, 'Preflight/failure changed active swap'
            if single and case in ('both-pvs','third-pv','md-both','md-third'):
                assert 'Existing storage spans an unselected disk:' in result.stderr
                assert 'no disk has been erased' in result.stderr
    print(('Single-disk' if single else 'RAID')+' storage guard: '+str(len(cases))+' cases; LVM/MD identities, duplicate VG names, missing/hidden members, holders, mounts and swap verified')
