#!/bin/sh
# Read-only preflight; no package, network or service changes.
set -u
printf '\nOperating system\n'
cat /etc/os-release
uname -r
printf '\nNetwork\n'
ip -brief address
ip route
ss -lntup
printf '\nExisting services\n'
for unit in tftp.socket tftp.service dhcpd dnsmasq httpd nginx iventoy ventoy-center; do
    systemctl show "$unit" -p Id -p LoadState -p ActiveState -p UnitFileState --no-pager 2>/dev/null
done
printf '\nStorage and security\n'
df -h / /home /opt /var
command -v getenforce >/dev/null && getenforce
systemctl is-active firewalld 2>/dev/null || :
if [ "$#" -eq 1 ]; then
    for part in iso user/scripts data/iventoy.dat iventoy.sh; do
        ls -ld -- "$1/$part" 2>/dev/null || :
    done
fi
exit 0
