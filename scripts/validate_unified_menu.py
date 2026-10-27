"""Exercise optional automatic boot hooks against isolated filesystem fixtures."""
import os
from pathlib import Path
import re
import subprocess
import sys
import tarfile
import tempfile

fixtures = Path(sys.argv[1]) / 'setup-preload'
count = 0
for profile in sorted(fixtures.iterdir()):
    archive, = (profile / 'pxe').glob('*-iventoy.tar.gz')
    with tarfile.open(archive) as tar:
        names = tar.getnames()
        if 'ventoy-center/selected-kickstart.cfg' not in names:
            continue
        assert 'etc/cmdline.d/99-ventoy-center-ks.conf' not in names
        answer = tar.extractfile('ventoy-center/selected-kickstart.cfg').read()
        script = tar.extractfile('usr/lib/dracut/hooks/cmdline/01-ventoy-center-ks.sh').read().decode()
    for selection in ('manual', 'empty', 'other', 'automatic'):
        for cached in (False, True):
            with tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp)
                (root / 'ventoy').mkdir()
                (root / 'ventoy-center').mkdir()
                (root / 'ventoy-center/selected-kickstart.cfg').write_bytes(answer)
                if selection != 'manual':
                    (root / 'ventoy/autoinstall').write_bytes({'empty': b'', 'other': b'foreign answer', 'automatic': answer}[selection])
                body = re.sub(r'/(?:ventoy-center|ventoy|etc/cmdline.d)(?=[/\s\"\'])', lambda m: tmp+m[0], script)
                hook = root / 'hook.sh'
                hook.write_text(body)
                shell = ('CMDLINE="root=live:CDLABEL=TEST rd.neednet=1"\n' if cached else 'unset CMDLINE\n')
                # Run twice to verify that the live injection and dracut hooks
                # compose without appending duplicate args or enabling manual.
                shell += '. "$1"\n. "$1"\nprintf "%s" "${CMDLINE-unset}"\n'
                result = subprocess.run(['sh', '-eu', '-c', shell, 'test', str(hook)], check=True, text=True, capture_output=True)
                conf = root / 'etc/cmdline.d/99-ventoy-center-ks.conf'
                assert conf.exists() == (selection == 'automatic'), (profile, selection)
                if cached:
                    assert result.stdout.startswith('root=live:CDLABEL=TEST rd.neednet=1')
                    assert result.stdout.count('inst.ks=') == int(selection == 'automatic')
                else:
                    assert result.stdout == 'unset', 'do not replace the real kernel command line before dracut reads it'
    count += 1
assert count >= 10
print('PASS: manual/empty/foreign/automatic Kickstart selection, cached and uncached dracut:', count, 'profiles')
