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
    for support in (ROOT / 'internal/center/assets/install-support').iterdir(): shutil.copy2(support, src / support.name)
    (src / 'support-version').write_text('validation-release\n')
    # Full setup is inert, but a separately tested APT policy now runs in the
    # target. Stub it here so this copy-hook test never configures its runner.
    (src / 'target-update-policy.sh').write_text('#!/bin/sh\ntest -f "$1/root/ventoy-linux-setup.run"\n')
    for name in ('linux-update-policy.sh', 'ubuntu-kernel.py'):
        (src / name).write_text('# rendered policy is supplied by Go\n')
    # Stub shutdown here: copy-hook tests must never reload the runner's systemd.
    shutdown = base / 'shutdown-prepared'
    helper = src / 'ubuntu-reboot.sh'
    helper.write_text('#!/bin/sh\ntest -f "' + str(base / 'installed/var/lib/ventoy-center/manual-setup-preloaded') + '" || exit 98\ntouch "' + str(shutdown) + '"\n')
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
    assert (target / 'root/README-ventoy-software.txt').read_bytes() == (src / 'software-README.txt').read_bytes()
    assert {name: (target / 'etc' / name).read_bytes() for name in baseline} == baseline
    assert not sentinel.exists()
    assert (target / 'var/lib/ventoy-center/manual-setup-preloaded').read_text().startswith('PRELOADED_ONLY\n')
    # Refresh a baked, unused managed package; never replace a user file or an
    # already-applied setup. Keep the exact former bytes for recovery.
    updated = payload + b'# updated managed setup\n'
    (src / 'ventoy-linux-setup.run').write_bytes(updated)
    (src / 'setup.sha256').write_text(hashlib.sha256(updated).hexdigest() + '  ventoy-linux-setup.run\n')
    for kind in ('managed', 'applied', 'missing-record', 'stale-marker'):
        upgrade = base / ('upgrade-' + kind)
        shutil.copytree(target, upgrade)
        if kind == 'applied':
            (upgrade / 'var/lib/ventoy-center/manual-init').mkdir()
        elif kind == 'missing-record':
            (upgrade / 'root/ventoy-linux-setup.run.sha256').unlink()
        elif kind == 'stale-marker':
            (upgrade / 'var/lib/ventoy-center/manual-setup-preloaded').write_text('PRELOADED_ONLY\nsha256=wrong\n')
        run(src / 'copy-to-target.sh', upgrade, ok=(kind == 'managed'))
        assert (upgrade / 'root/ventoy-linux-setup.run').read_bytes() == (updated if kind == 'managed' else payload)
        if kind == 'managed':
            backup = upgrade / ('root/ventoy-linux-setup.run.preload-backup.' + hashlib.sha256(payload).hexdigest())
            assert backup.read_bytes() == payload
            assert (upgrade / 'root/ventoy-linux-setup.run.sha256').read_bytes() == (src / 'setup.sha256').read_bytes()
            run(src / 'copy-to-target.sh', upgrade)  # Repeat remains idempotent.
    assert not sentinel.exists()
    (src / 'ventoy-linux-setup.run').write_bytes(payload)
    (src / 'setup.sha256').write_text(hashlib.sha256(payload).hexdigest() + '  ventoy-linux-setup.run\n')
    (src / 'ventoy-linux-setup.run').write_bytes(payload + b'corruption')
    assert shutdown.exists()
    shutdown.unlink()
    run(src / 'copy-to-target.sh', target, ok=False)
    assert not shutdown.exists()  # Failed staging must not prepare reboot.
    assert (target / 'root/ventoy-linux-setup.run').read_bytes() == payload
    (src / 'ventoy-linux-setup.run').write_bytes(payload)
    helper.write_text('#!/bin/sh\nexit 97\n')
    (target / 'root/ventoy-linux-setup.run').write_bytes(payload)
    result = run(src / 'copy-to-target.sh', target, ok=False)
    assert result.returncode == 97  # Shutdown preparation failures propagate.
    helper.write_text('#!/bin/sh\nexit 0\n')
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
        assert (live / 'usr/local/lib/ventoy-preload/software-README.txt').read_bytes() == (src / 'software-README.txt').read_bytes()
        assert (live / 'usr/local/lib/ventoy-preload/ubuntu-reboot.sh').read_bytes() == helper.read_bytes()
        assert not (live / 'root').exists()
        assert not sentinel.exists()
        dropins = list((live / 'etc/systemd/system').glob('*.service.d/*.conf'))
        assert bool(dropins) == (kind in ('subiquity', 'subiquity20', 'desktop26'))
        for dropin in dropins:
            assert '--autoinstall' not in dropin.read_text()
        if kind in ('subiquity', 'subiquity20'):
            guard = live / 'etc/systemd/system/snap.subiquity.subiquity-server.service.d/80-ventoy-manual-update-policy.conf'
            assert 'ExecStartPre=' in guard.read_text()
            # The automatic hook may have run first. Never replace its answers.
            guard.unlink()
            (live / 'ventoy-center-selected.cfg').write_text('existing automatic answers')
            run(src / 'prepare-live.sh', live)
            assert not guard.exists()
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
print('PASS: inert full setup, checksum, repeat, existing-file protection, separate update policy and interactive hook staging')
