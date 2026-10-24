#!/bin/sh
set -eu
umask 077
test -f "/target/boot/vmlinuz-$(uname -r)"
test -d /target/usr/share/xsessions
printf '%s' {{sh (b64 .UpdatePolicy)}} | base64 -d > /target/usr/local/sbin/ventoy-center-update-policy
chmod 0700 /target/usr/local/sbin/ventoy-center-update-policy
in-target /bin/sh /usr/local/sbin/ventoy-center-update-policy --apply --offline
# The target's resolved symlink can be dangling while in-target exposes the
# live Desktop's NetworkManager /run. Supply the live resolver temporarily and
# restore the target's original file/symlink even if package installation fails.
test -d /target/etc && test ! -L /target/etc
test -r /etc/resolv.conf
resolver_backup=$(mktemp -d /tmp/ventoy-desktop-resolver.XXXXXXXX)
resolver_present=0
if [ -e /target/etc/resolv.conf ] || [ -L /target/etc/resolv.conf ]; then
    cp -a /target/etc/resolv.conf "$resolver_backup/original"
    resolver_present=1
fi
restore_resolver() {
    rm -f /target/etc/resolv.conf
    if [ "$resolver_present" = 1 ]; then cp -a "$resolver_backup/original" /target/etc/resolv.conf; fi
    rm -rf "$resolver_backup"
}
trap restore_resolver EXIT
python3 - "$resolver_backup/live" <<'VC_RESOLVER_PY'
import ipaddress
import json
from pathlib import Path
import re
import subprocess
import sys

def upstreams(text):
    result = []
    for line in text.splitlines():
        fields = line.split()
        if len(fields) < 2 or fields[0] != 'nameserver':
            continue
        try:
            address = ipaddress.ip_address(fields[1])
        except ValueError:
            continue
        if not address.is_loopback and not address.is_unspecified and not address.is_multicast:
            result.append(str(address))
    return result

def resolver(read, interfaces, routes):
    current = read('/etc/resolv.conf')
    if upstreams(current):
        return current
    resolved = read('/run/systemd/resolve/resolv.conf')
    if upstreams(resolved):
        return resolved
    # Casper can configure an interface before NetworkManager starts. Its
    # DHCP DNS remains in net-IFACE.conf while resolved has no uplink servers.
    # Only consume the lease of a unique default interface whose IP still
    # matches. Parse literal fields; never source a DHCP-generated shell file.
    devices = {route.get('dev') for route in routes if route.get('dev')}
    if len(devices) == 1:
        device = next(iter(devices))
        if re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9_.-]{0,14}', device):
            values = {}
            for line in read('/run/net-' + device + '.conf').splitlines():
                match = re.fullmatch(r"(DEVICE|IPV4ADDR|IPV4DNS[0-9]+)=(?:'([^']*)'|\"([^\"]*)\"|([^\s]+))", line)
                if match:
                    values[match[1]] = next(value for value in match.groups()[1:] if value is not None)
            addresses = {address.get('local') for interface in interfaces
                         if interface.get('ifname') == device
                         for address in interface.get('addr_info', [])}
            if values.get('DEVICE') == device and values.get('IPV4ADDR') in addresses:
                servers = upstreams('\n'.join('nameserver ' + value for key, value in sorted(values.items())
                                               if key.startswith('IPV4DNS')))
                if servers:
                    return ''.join('nameserver ' + address + '\n' for address in dict.fromkeys(servers))
    # A working local resolver (for example dnsmasq) may have no exported
    # upstream file. Preserve it when no verified DHCP fallback is available.
    return current

if __name__ == '__main__':
    def read(path):
        try:
            return Path(path).read_text()
        except FileNotFoundError:
            return ''
    interfaces = json.loads(subprocess.check_output(['ip', '-j', '-4', 'address', 'show']))
    routes = json.loads(subprocess.check_output(['ip', '-j', '-4', 'route', 'show', 'default']))
    Path(sys.argv[1]).write_text(resolver(read, interfaces, routes))
VC_RESOLVER_PY
rm -f /target/etc/resolv.conf
cp "$resolver_backup/live" /target/etc/resolv.conf
chmod 0644 /target/etc/resolv.conf
in-target env DEBIAN_FRONTEND=noninteractive apt-get update
in-target env DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends openssh-server curl sudo ca-certificates
{{if .Request.RootPasswordHash}}in-target usermod --password {{sh .Request.RootPasswordHash}} root
{{end}}printf '%s' {{sh (b64 .DebianLate)}} | base64 -d > /target/root/ventoy-center-late.sh
in-target /bin/sh /root/ventoy-center-late.sh
rm -f /target/root/ventoy-center-late.sh
uname -r > /target/var/lib/ventoy-center/kernel-policy/expected-release
test -f "/target/boot/vmlinuz-$(uname -r)"
touch /run/ventoy-center-ubiquity-complete
