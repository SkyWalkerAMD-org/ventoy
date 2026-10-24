"""Exercise installer copy hooks using disposable roots; never configure the host."""
from pathlib import Path
import hashlib
import shutil
import subprocess
import tempfile
import asyncio
import importlib.util
import sys
import types

ROOT = Path(__file__).resolve().parents[1]
ASSETS = ROOT / 'internal/center/assets/manual-preload'

def run(script, *args, ok=True):
    p = subprocess.run(['sh', str(script), *map(str, args)], capture_output=True, text=True)
    assert (p.returncode == 0) == ok, (script, p.returncode, p.stdout, p.stderr)
    return p

with tempfile.TemporaryDirectory(prefix='ventoy-preload-test-') as temp:
    base = Path(temp)
    src = base / 'payload'
    shutil.copytree(ASSETS, src)
    sentinel = base / 'must-not-execute'
    payload = ('#!/bin/sh\ntouch ' + str(sentinel) + '\nexit 99\n').encode()
    (src / 'ventoy-linux-setup.run').write_bytes(payload)
    (src / 'setup.sha256').write_text(hashlib.sha256(payload).hexdigest() + '  ventoy-linux-setup.run\n')
    target = base / 'installed'
    (target / 'etc').mkdir(parents=True)
    (target / 'etc/os-release').write_text('ID=ubuntu\nVERSION_ID=22.04\n')
    (target / 'etc/shadow').write_text('existing password data\n')
    (target / 'etc/hostname').write_text('existing-host\n')
    baseline = {p.name: p.read_bytes() for p in (target / 'etc').iterdir()}
    run(src / 'copy-to-target.sh', target)
    run(src / 'copy-to-target.sh', target)
    assert (target / 'root/ventoy-linux-setup.run').read_bytes() == payload
    assert (target / 'root/ventoy-linux-setup.run').stat().st_mode & 0o777 == 0o700
    assert {p.name: p.read_bytes() for p in (target / 'etc').iterdir()} == baseline
    assert not sentinel.exists()
    assert (target / 'var/lib/ventoy-center/manual-setup-preloaded').read_text().startswith('PRELOADED_ONLY\n')
    (src / 'ventoy-linux-setup.run').write_bytes(payload + b'corruption')
    run(src / 'copy-to-target.sh', target, ok=False)
    assert (target / 'root/ventoy-linux-setup.run').read_bytes() == payload
    (src / 'ventoy-linux-setup.run').write_bytes(payload)
    (target / 'root/ventoy-linux-setup.run').write_bytes(b'custom setup')
    run(src / 'copy-to-target.sh', target, ok=False)
    assert (target / 'root/ventoy-linux-setup.run').read_bytes() == b'custom setup'
    run(src / 'copy-to-target.sh', '/', ok=False)
    bad = base / 'symlink-target'
    (bad / 'etc').mkdir(parents=True)
    (bad / 'etc/os-release').write_text('ID=debian\n')
    (bad / 'root').symlink_to(target / 'root')
    run(src / 'copy-to-target.sh', bad, ok=False)
    for kind in ('anaconda', 'ubiquity', 'subiquity', 'subiquity20', 'desktop26'):
        live = base / kind
        (live / 'etc').mkdir(parents=True)
        (live / 'etc/os-release').write_text('ID=ubuntu\nVERSION_ID="20.04"\n' if kind == 'subiquity20' else 'ID=ubuntu\n')
        if kind == 'anaconda':
            (live / 'usr/share/anaconda/post-scripts').mkdir(parents=True)
            hook = live / 'usr/share/anaconda/post-scripts/99-ventoy-preload.ks'
        elif kind == 'ubiquity':
            (live / 'usr/bin').mkdir(parents=True)
            exe = live / 'usr/bin/ubiquity'
            exe.write_text('#!/bin/sh\n'); exe.chmod(0o755)
            hook = live / 'usr/lib/ubiquity/target-config/99ventoy-preload'
        else:
            hook = live / 'etc/subiquity/postinst.d/99-ventoy-preload'
            if kind == 'desktop26':
                snap = live / 'var/lib/snapd/snaps/ubuntu-desktop-bootstrap_589.snap'
                snap.parent.mkdir(parents=True)
                snap.touch()
        run(src / 'prepare-live.sh', live)
        assert hook.is_file(), kind
        assert (live / 'usr/local/lib/ventoy-preload/ventoy-linux-setup.run').read_bytes() == payload
        assert not (live / 'root').exists()
        assert not sentinel.exists()
        dropins = list((live / 'etc/systemd/system').glob('*.service.d/*.conf'))
        assert bool(dropins) == (kind in ('subiquity20', 'desktop26'))
        for dropin in dropins:
            assert '--autoinstall' not in dropin.read_text()
    assert not sentinel.exists()

    # A pre-postinst.d installer must not announce completion/reboot readiness
    # until the copy finishes. A copy failure must propagate, not become success.
    events = []
    class Late:
        async def run(self):
            events.append('original-complete')
            return 'unchanged-result'
    async def command(args, **kwargs):
        assert args[-1] == '/target' and kwargs == {'check': True}
        events.append('copy-complete')
    modules = {
        'subiquity.server.controllers.cmdlist': types.SimpleNamespace(LateController=Late),
        'subiquitycore.utils': types.SimpleNamespace(arun_command=command),
    }
    saved = {name: sys.modules.get(name) for name in modules}
    try:
        sys.modules.update(modules)
        spec = importlib.util.spec_from_file_location('preload_legacy_test', ASSETS / 'subiquity-legacy.py')
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        instance = Late()
        instance.app = types.SimpleNamespace(base_model=types.SimpleNamespace(target='/target'))
        assert asyncio.run(instance.run()) == 'unchanged-result'
        assert events == ['copy-complete', 'original-complete']
        async def failed_command(*args, **kwargs):
            raise RuntimeError('copy failed')
        module.arun_command = failed_command
        events.clear()
        try:
            asyncio.run(instance.run())
            raise AssertionError('copy failure was hidden')
        except RuntimeError as error:
            assert str(error) == 'copy failed'
        assert not events
    finally:
        for name, old in saved.items():
            if old is None:
                sys.modules.pop(name, None)
            else:
                sys.modules[name] = old
print('PASS: inert preload, checksum, repeat, existing-file and symlink protection; installer hook staging')
