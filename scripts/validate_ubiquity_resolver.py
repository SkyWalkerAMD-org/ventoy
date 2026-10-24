"""Offline checks for Desktop chroot DNS; never changes host networking."""
from pathlib import Path
import sys

template = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(__file__).resolve().parents[1] / 'internal/center/templates/ubiquity-late.sh'
code = template.read_text(encoding='utf-8').split("<<'VC_RESOLVER_PY'\n", 1)[1].split('\nVC_RESOLVER_PY', 1)[0]
namespace = {'__name__': 'resolver_test'}
exec(compile(code, str(template), 'exec'), namespace)
resolver = namespace['resolver']
stub = 'nameserver 127.0.0.53\n'
interfaces = [{'ifname': 'ens3', 'addr_info': [{'local': '10.0.2.15'}]}]
routes = [{'dev': 'ens3', 'gateway': '10.0.2.2'}]
lease = "DEVICE='ens3'\nIPV4ADDR='10.0.2.15'\nIPV4DNS0='10.0.2.3'\n"

def check(name, files, expected, selected_routes=routes):
    actual = resolver(lambda path: files.get(path, ''), interfaces, selected_routes)
    assert actual == expected, (name, actual, expected)

base = {'/etc/resolv.conf': stub, '/run/net-ens3.conf': lease}
check('casper-dhcp-fallback', base, 'nameserver 10.0.2.3\n')
check('preserve-direct-dns', dict(base, **{'/etc/resolv.conf': 'nameserver 192.0.2.53\nsearch example.test\n'}), 'nameserver 192.0.2.53\nsearch example.test\n')
check('prefer-resolved-uplink', dict(base, **{'/run/systemd/resolve/resolv.conf': 'nameserver 2001:db8::53\n'}), 'nameserver 2001:db8::53\n')
check('reject-stale-lease', dict(base, **{'/run/net-ens3.conf': lease.replace('10.0.2.15', '10.0.2.99')}), stub)
check('reject-other-device', dict(base, **{'/run/net-ens3.conf': lease.replace("DEVICE='ens3'", "DEVICE='ens4'")}), stub)
check('no-guess-multiple-defaults', base, stub, routes + [{'dev': 'ens4'}])
check('reject-command-substitution', dict(base, **{'/run/net-ens3.conf': lease.replace('10.0.2.3', '$(touch /tmp/should-not-execute)')}), stub)
check('deduplicate-servers', dict(base, **{'/run/net-ens3.conf': lease + "IPV4DNS1='10.0.2.3'\nIPV4DNS2='0.0.0.0'\n"}), 'nameserver 10.0.2.3\n')
print('PASS: 8 Desktop DNS cases, including stale leases and literal-only DHCP parsing')
