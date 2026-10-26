"""Execute sizing/recipe generation against fixture sysfs, never real disks.

Compare native Kickstart, Curtin and partman allocations with user-facing tiers.
Only known sizing functions and recipe fragments are executed; installer guards,
claims, partition tools and network calls are never executed by this validator.
"""
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
from unittest.mock import patch
import yaml
from pykickstart.parser import KickstartParser
from pykickstart.version import makeVersion
from kickstart_profiles import KICKSTART_VERSIONS

MIB = 1048576
TIERS = [
    (120, (1280, 8192, 16384, 32768)),
    (256, (2560, 16384, 32768, 65536)),
    (480, (5120, 32768, 65536, 131072)),
    (1024, (10240, 65536, 131072, 262144)),
    (1920, (20480, 131072, 262144, 524288)),
    (4000, (40960, 262144, 524288, 1048576)),
]


def recipe(guard, directory, capacity, raid):
    start = guard.index('# Capacity is read only after serial/scope verification.')
    end = guard.index('\n', guard.index('vc_storage_sizes "$disk"', start))
    sizing = guard[start:end].replace('/sys/class/block', str(directory/'sys'))
    for disk, size in [('nvme0n1', capacity), ('sdb', capacity*2)]:
        folder = directory/'sys'/disk; folder.mkdir(parents=True, exist_ok=True)
        (folder/'size').write_text(str(size*10**9//512))
    script = 'set -eu\ndisk=nvme0n1\ndisk2=sdb\n' + sizing + '\n'
    if 'VC_RAID_KS' in guard or 'VC_SINGLE_KS' in guard:
        marker = 'VC_RAID_KS' if raid else 'VC_SINGLE_KS'
        fragment = re.search(r'cat > /tmp/ventoy-center-partition.ks <<'+marker+r'\n.*?\n'+marker, guard, re.S)[0]
    elif 'VC_RAID_PRESEED' in guard or 'VC_SINGLE_PRESEED' in guard:
        marker = 'VC_RAID_PRESEED' if raid else 'VC_SINGLE_PRESEED'
        start = guard.index('vc_efi_mb=$(vc_mib_to_mb 1024)')
        finish = guard.index('\n'+marker, start)+len(marker)+1
        fragment = guard[start:finish]
    else:
        fragment = ''
    script += fragment.replace('/tmp/ventoy-center-', str(directory/'ventoy-center-'))
    script += '\nprintf "SIZES=%s\\n" "$VC_STORAGE_SIZES_MIB"\n'
    result = subprocess.run(['/bin/sh','-eu'],input=script,text=True,capture_output=True,check=True)
    sizes = result.stdout.split('SIZES=')[1].strip()
    native = list(directory.glob('ventoy-center-*'))
    return sizes, native[0].read_text() if native else None


def validate(folder, capacity, expected, raid, filesystem='ext4'):
    guard = (folder/'preinstall.sh').read_text()
    with tempfile.TemporaryDirectory(prefix='vc-layout-') as tmp:
        work = Path(tmp)
        sizes, native = recipe(guard, work, capacity, raid)
        assert tuple(map(int, sizes.split())) == expected, (folder, capacity, sizes)
        mounts = dict(zip(('/tmp','swap','/var','/'), expected))
        mounts['/boot'] = 2048
        if folder.name == 'uefi': mounts['/boot/efi'] = 1024
        if (folder/'user-data').exists():
            source = re.search(r"<<'VC_UBUNTU_STORAGE'\n(.*?)\nVC_UBUNTU_STORAGE", guard, re.S)[1]
            ns = {'__name__':'storage_test'}; exec(compile(source,'storage-binder','exec'),ns)
            original = (folder/'user-data').read_bytes(); path = work/'autoinstall.yaml'
            path.write_bytes(original)
            bind = ns['bind_install_disks'] if raid else ns['bind_install_disk']
            args = (path,'nvme0n1','sdb') if raid else (path,'nvme0n1')
            with patch.dict(os.environ, VC_STORAGE_SIZES_MIB=sizes): bind(*args)
            document = yaml.safe_load(path.read_text()); storage=document['autoinstall']['storage']
            actions=storage['config']; ids={a['id']:a for a in actions}
            for mount in (x for x in actions if x['type']=='mount'):
                key = 'swap' if mount['path']=='none' else mount['path']
                expected_fs = 'swap' if key == 'swap' else 'ext4' if key == '/boot' else 'fat32' if key in ('/boot/efi', '/boot/efi2') else filesystem
                assert ids[mount['device']]['fstype'] == expected_fs, (folder, key, ids[mount['device']])
                volume=ids[ids[mount['device']]['volume']]
                if key=='/boot' and raid:
                    assert volume['raidlevel']==1
                    for member in volume['devices']: assert ids[member]['size']==2048*MIB
                elif key in mounts: assert volume['size']==mounts[key]*MIB, (key, volume)
                elif key=='/home': assert ('size' not in volume if raid else volume['size']==-1)
            paths={a['path'] for a in actions if a['type']=='mount'}
            assert {'/boot','/tmp','none','/var','/','/home'} <= paths
            # No missing/bad environment or second binding may select a default.
            unchanged=path.read_bytes()
            with patch.dict(os.environ, VC_STORAGE_SIZES_MIB=sizes):
                try: bind(*args)
                except ValueError: pass
                else: raise AssertionError('Rebinding accepted')
            assert path.read_bytes()==unchanged
            for bad in ('', '1 2 3 4', '0 0 0 0', '5120 32768 65536 131072;false'):
                path.write_bytes(original)
                with patch.dict(os.environ, VC_STORAGE_SIZES_MIB=bad):
                    try: bind(*args)
                    except ValueError: pass
                    else: raise AssertionError('Bad tier accepted')
                assert path.read_bytes()==original
        elif (folder/'kickstart.cfg').exists():
            handler = makeVersion(KICKSTART_VERSIONS[folder.parent.name])
            KickstartParser(handler,followIncludes=False).readKickstartFromString(native)
            allocations={p.mountpoint:p for p in handler.partition.partitions+handler.logvol.lvList}
            for key,mib in mounts.items():
                if raid and key=='/boot': continue
                assert allocations[key].size==mib and not allocations[key].grow, (key,native)
            assert allocations['/home'].grow
            for key in ('/tmp','/var','/','/home'):
                assert allocations[key].fstype == filesystem, (folder,key,native)
            if raid:
                assert next(x for x in handler.raid.raidList if x.mountpoint=='/boot').fstype=='ext4'
            else:
                assert allocations['/boot'].fstype == 'ext4'
            assert handler.clearpart.drives==(['nvme0n1','sdb'] if raid else ['nvme0n1'])
            assert handler.clearpart.disklabel=='gpt'
        else:
            preseed = (folder/'preseed.cfg').read_text().replace('\\\n', ' ')
            for hook in preseed.splitlines():
                if hook.startswith('d-i preseed/late_command string '):
                    assert len(hook.encode()) < 120 * 1024, 'Debian preseed logger/exec argument too large'
            line=next(x.split(' string ',1)[1] for x in native.splitlines() if x.startswith('d-i partman-auto/expert_recipe string '))
            parts=[x.strip() for x in line.split('::',1)[1].split(' .') if x.strip()]
            found=set()
            for part in parts:
                lo, priority, hi = map(int,part.split()[:3])
                m=re.search(r'mountpoint\{ ([^}]+) \}',part)
                key=m[1].strip() if m else 'swap' if 'method{ swap }' in part else '/boot/efi' if 'method{ efi }' in part else None
                if key in ('/tmp','/var','/','/home','/boot'):
                    expected_fs = 'ext4' if key == '/boot' else filesystem
                    assert part.split()[3] == expected_fs, (folder,key,part)
                    assert 'filesystem{ '+expected_fs+' }' in part, (folder,key,part)
                if key in mounts:
                    mb=(mounts[key]*MIB+999999)//1000000
                    assert lo==priority==hi==mb,(key,part)
                    found.add(key)
                if key=='/home': assert hi==-1;found.add(key)
            assert {'/tmp','swap','/var','/','/home'}<=found
            assert '/dev/nvme0n1p2#/dev/sdb2' in native if raid else 'method string regular' in native
            if raid: assert 'recipe string 1 2 0 ext4 /boot ' in native


root=Path(sys.argv[1]); count=0
for group,raid in [('storage-layout',False),('raid',True)]:
    folders=sorted((root/group).glob('*/*')); assert len(folders)>=40
    for folder in folders:
        for capacity,expected in TIERS:
            validate(folder,capacity,expected,raid);count+=1
print('Capacity tiers, native layouts, remaining /home, mixed-size RAID1 and binder refusal:',count,'checks passed')
choices=sorted((root/'filesystems').glob('*/*/*/*/case.json'))
assert len(choices)>=100, 'Missing filesystem fixtures'
for case in choices:
    meta=json.loads(case.read_text())
    validate(case.parent, 480, (5120,32768,65536,131072), meta['mode']=='raid', meta['filesystem'])
print('ext4/XFS generated layouts across all Linux profiles, firmware and single/RAID modes:',len(choices),'passed')
