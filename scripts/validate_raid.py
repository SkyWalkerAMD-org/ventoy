"""Validate native RAID plans without invoking an installer or touching disks."""
import copy
import os
import io
import pathlib
import re
import subprocess
import sys
import tempfile
import yaml
from unittest.mock import patch
from pykickstart.parser import KickstartParser
from pykickstart.version import makeVersion
from kickstart_profiles import KICKSTART_VERSIONS
from validate_raid_guard import check_raid_guard

root = pathlib.Path(sys.argv[1]) / 'raid'
count = 0
for folder in sorted(root.glob('*/*')):
    guard = (folder/'preinstall.sh').read_text()
    installed = (folder/'raid-installed.sh').read_text()
    for script in (guard, installed):
        subprocess.run(['bash','-n'],input=script,text=True,check=True)
    assert guard.index('two-disk rebuild scope verified') < guard.index('"state":"installing"')
    assert guard.index('\nvc_storage_prepare\n') > guard.index('"state":"installing"')
    assert 'Existing storage spans an unselected disk' in guard
    assert 'Selected storage is mounted' in guard and 'Selected swap remains active' in guard
    if (folder/'user-data').exists():
        source = re.search(r"<<'VC_UBUNTU_STORAGE'\n(.*?)\nVC_UBUNTU_STORAGE",guard,re.S).group(1)
        namespace={'__name__':'raid_test'}
        exec(compile(source,'raid-storage','exec'),namespace)
        original=yaml.safe_load((folder/'user-data').read_text())
        for command in original['autoinstall']['late-commands']:
            subprocess.run(['bash','-n'],input=command,text=True,check=True)
        with tempfile.TemporaryDirectory() as directory:
            path=pathlib.Path(directory)/'config.yaml'
            path.write_text(yaml.safe_dump(original))
            with patch.dict(os.environ, VC_STORAGE_SIZES_MIB='5120 32768 65536 131072'):
                namespace['bind_install_disks'](str(path),'nvme0n1','sdb')
            result=yaml.safe_load(path.read_text())['autoinstall']['storage']['config']
            assert [x['path'] for x in result if x['type']=='disk']==['/dev/nvme0n1','/dev/sdb']
            assert len([x for x in result if x['type']=='raid' and x['raidlevel']==1])==2
            assert len([x for x in result if x['type']=='lvm_volgroup'])==1
            assert all(x.get('devices')==['vc-data-md'] for x in result if x['type']=='lvm_volgroup')
            unchanged=path.read_bytes()
            try:namespace['bind_install_disks'](str(path),'sdc','sdd')
            except ValueError:pass
            else:raise AssertionError('replaced an already bound layout')
            assert path.read_bytes()==unchanged
    elif (folder/'kickstart.cfg').exists():
        profile=folder.parent.name
        version=KICKSTART_VERSIONS[profile]
        parser=KickstartParser(makeVersion(version),followIncludes=False)
        parser.readKickstart(str(folder/'kickstart.cfg'))
        recipe=re.search(r'<<VC_RAID_KS\n(.*?)\nVC_RAID_KS',guard,re.S).group(1)
        recipe=recipe.replace('$disk2','sdb').replace('$disk','nvme0n1')
        for name,size in zip(('tmp','swap','var','root'),(5120,32768,65536,131072)):
            recipe=recipe.replace('$vc_'+name+'_mib',str(size))
        parser=KickstartParser(makeVersion(version),followIncludes=False)
        parser.readKickstartFromString(recipe)
        assert 'sdc' not in recipe and 'raid /boot' in recipe and 'volgroup vcvg pv.01' in recipe
    else:
        assert 'partman-auto/method string raid' in guard
        assert '${first}2#${second}2' in guard and '${first}3#${second}3' in guard
        assert 'lvmok' in guard and 'in_vg{ vcvg }' in guard
    count+=1
assert count>=30, count
check_raid_guard((root/'ubuntu-22.04/uefi/preinstall.sh').read_text())
print('Native RAID layout syntax, exact member binding, LVM placement, and refusal to rebind:', count, 'profiles/firmware combinations')

# A selected old VG confined to one disk uses the existing single-disk recipe,
# with the same read-only membership guard as a two-disk rebuild. Validate the
# generated files for every installer/firmware, not only the CentOS regression.
single_root = pathlib.Path(sys.argv[1]) / 'single-rebuild'
single_count = 0
for folder in sorted(single_root.glob('*/*')):
    guard = (folder/'preinstall.sh').read_text()
    subprocess.run(['bash','-n'],input=guard,text=True,check=True)
    assert '$disk2' not in guard and 'two-disk rebuild' not in guard
    assert guard.index('single-disk rebuild scope verified') < guard.index('"state":"installing"')
    assert guard.index('\nvc_storage_prepare\n') > guard.index('"state":"installing"')
    assert 'wipefs ' not in guard and 'vgremove ' not in guard
    if (folder/'kickstart.cfg').exists():
        version = KICKSTART_VERSIONS[folder.parent.name]
        parser = KickstartParser(makeVersion(version),followIncludes=False)
        parser.readKickstart(str(folder/'kickstart.cfg'))
        recipe = re.search(r'<<VC_SINGLE_KS\n(.*?)\nVC_SINGLE_KS',guard,re.S).group(1)
        recipe = recipe.replace('$disk','sda')
        for name,size in zip(('tmp','swap','var','root'),(1280,8192,16384,32768)):
            recipe=recipe.replace('$vc_'+name+'_mib',str(size))
        parser=KickstartParser(makeVersion(version),followIncludes=False)
        parser.readKickstartFromString(recipe)
        assert '--only-use=sda' in recipe and '--drives=sda' in recipe
    elif (folder/'user-data').exists():
        source=re.search(r"<<'VC_UBUNTU_STORAGE'\n(.*?)\nVC_UBUNTU_STORAGE",guard,re.S).group(1)
        namespace={'__name__':'single_rebuild_test'}
        exec(compile(source,'single-storage','exec'),namespace)
        with tempfile.TemporaryDirectory() as directory:
            path=pathlib.Path(directory)/'config.yaml'
            path.write_text((folder/'user-data').read_text())
            with patch.dict(os.environ,VC_STORAGE_SIZES_MIB='1280 8192 16384 32768'):
                namespace['bind_install_disk'](str(path),'sda')
            actions=yaml.safe_load(path.read_text())['autoinstall']['storage']['config']
            disks=[a for a in actions if a['type']=='disk']
            assert len(disks)==1 and disks[0]['path']=='/dev/sda'
            assert disks[0]['wipe']=='superblock-recursive'
            assert not any(a['type'] in ('raid','lvm_volgroup') for a in actions)
    else:
        assert 'partman-lvm/device_remove_lvm boolean true' in guard
        assert 'partman-md/device_remove_md boolean true' in guard
        assert 'partman-auto/method string regular' in guard
    single_count+=1
assert single_count>=50, single_count
check_raid_guard((single_root/'centos-7.9/bios/preinstall.sh').read_text(), single=True)
print('Single-disk old-storage rebuild recipes:',single_count,'profiles/firmware combinations')
