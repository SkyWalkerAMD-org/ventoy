"""Compare two read-only installed-system snapshots; never connects or reboots."""
import argparse
from pathlib import Path
import re


def parse(path):
    result = {}
    for line in Path(path).read_text(encoding='utf-8').splitlines():
        if line.startswith('['):
            break
        if '=' in line:
            key, value = line.split('=', 1)
            if key in result:
                raise ValueError('Duplicate field: ' + key)
            result[key] = value
    if result.get('format') != 'ventoy-acceptance-v1':
        raise ValueError('Not a Ventoy acceptance snapshot')
    return result


def check(first, second, require_setup=False):
    problems = []
    if not first.get('boot_id') or not second.get('boot_id') or first.get('boot_id') == second.get('boot_id'):
        problems.append('Two different boot IDs are required')
    for key in ('hostname', 'kernel', 'rootfs', 'mode', 'setup_sha256'):
        if not first.get(key) or first[key] != second.get(key):
            problems.append('Changed or missing ' + key)
    for i, row in enumerate((first, second), 1):
        if not re.fullmatch(r'[a-f0-9]{64}', row.get('setup_sha256', '')):
            problems.append('Boot %s: missing setup SHA-256 evidence' % i)
        for key, wanted in (('disk_boot', 'yes'), ('ssh_active', 'yes'), ('permitrootlogin', 'yes'), ('passwordauthentication', 'yes'), ('dhcp_observed', 'yes'), ('update_policy', 'verified')):
            if row.get(key) != wanted:
                problems.append('Boot %s: %s=%s (expected %s)' % (i, key, row.get(key), wanted))
        if row.get('raid') not in ('healthy-or-syncing', 'not-present'):
            problems.append('Boot %s: RAID needs attention' % i)
        if require_setup and not row.get('setup', '').lower().startswith('verified'):
            problems.append('Boot %s: setup verification incomplete' % i)
    return problems


if __name__ == '__main__':
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('first')
    p.add_argument('second')
    p.add_argument('--require-setup', action='store_true')
    args = p.parse_args()
    errors = check(parse(args.first), parse(args.second), args.require_setup)
    print('\n'.join(errors) if errors else 'Two-boot software checks PASS. Real SSH/KVM login and storage/driver acceptance remain separate.')
    raise SystemExit(bool(errors))
