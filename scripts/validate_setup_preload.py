"""Parse answers with required setup preloading enabled; never run installers."""
from pathlib import Path
import subprocess
import sys
import tarfile

from pykickstart.parser import KickstartParser
from pykickstart.version import makeVersion
from kickstart_profiles import KICKSTART_VERSIONS
import yaml

root = Path(sys.argv[1]) / 'setup-preload'
count = 0
for profile in root.iterdir():
    job = profile / 'job'
    ks = job / 'kickstart.cfg'
    if ks.exists():
        handler = makeVersion(KICKSTART_VERSIONS[profile.name])
        KickstartParser(handler, followIncludes=False).readKickstart(str(ks))
        copies = [s for s in handler.scripts if 'copy-to-target.sh' in s.script]
        assert len(copies) == 1 and not copies[0].inChroot and copies[0].errorOnFail
        for script in handler.scripts:
            subprocess.run(['bash', '-n'], input=script.script, text=True, check=True)
    user = job / 'user-data'
    if user.exists():
        cfg = yaml.safe_load(user.read_text())['autoinstall']
        assert any('copy-to-target.sh /target' in c for c in cfg['late-commands'])
        for command in cfg['late-commands']:
            subprocess.run(['bash', '-n'], input=command, text=True, check=True)
    for mode in ('job', 'pxe'):
        archives = list((profile / mode).glob('*-iventoy.tar.gz'))
        assert len(archives) == 1
        with tarfile.open(archives[0]) as t:
            names = t.getnames()
            assert len(names) == len(set(names))
            # Ubuntu has a live boot hook to chain. Anaconda uses cmdline.d
            # and dracut, while Debian uses finish-install directly.
            if profile.name.startswith('ubuntu-'):
                assert 'ventoy-preload-chain/boot-hook.sh' in names
                assert 'ventoy-preload-chain/setup-hook.sh' in names
            elif not profile.name.startswith('debian-'):
                assert 'etc/cmdline.d/99-ventoy-center-ks.conf' in names
            assert 'ventoy-manual-preload/ventoy-linux-setup.run' in names
            for member in t.getmembers():
                if member.name.endswith('.sh'):
                    subprocess.run(['bash', '-n'], input=t.extractfile(member).read(), check=True)
    count += 1
assert count > 20
print('PASS: required setup preload answers and composed hooks:', count, 'Linux profiles')
