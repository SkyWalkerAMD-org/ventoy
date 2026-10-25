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
    assert 'Existing storage spans an unselected disk' in guard
    assert 'Selected storage is mounted' in guard and 'Selected storage has active swap' in guard
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
