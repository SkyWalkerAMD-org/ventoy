#!/bin/sh
# Read-only acceptance snapshot. Does not install, restart, wipe or test passwords.
set -eu
export LC_ALL=C
umask 077
[ "$(id -u)" = 0 ] || { echo 'Run as root to inspect SSH and update policy.' >&2; exit 1; }
state=/var/lib/ventoy-center
mkdir -p "$state/acceptance"
boot=$(cat /proc/sys/kernel/random/boot_id)
out=$(mktemp "$state/acceptance/$boot.XXXXXX")
policy=missing
if [ -f /usr/local/sbin/ventoy-center-update-policy ]; then
    if sh /usr/local/sbin/ventoy-center-update-policy --check > "$out.policy" 2>&1; then policy=verified; else policy=failed; fi
fi
sshd_config=$(/usr/sbin/sshd -T -C user=root,host=localhost,addr=127.0.0.1 2>/dev/null || true)
ssh_active=no
for unit in ssh.service sshd.service ssh.socket; do
    if systemctl is-active --quiet "$unit"; then ssh_active=yes; fi
done
dhcp=no
if ip -4 -o addr show scope global | grep -q ' dynamic '; then dhcp=yes; fi
rootfs=$(findmnt -n -o FSTYPE /)
root_source=$(findmnt -n -o SOURCE /)
disk_boot=yes
case "$rootfs" in overlay|squashfs|iso9660|tmpfs|'') disk_boot=no;; esac
raid=not-present
if grep -q '^md' /proc/mdstat; then
    if grep -Eq '\[[U_]*_[U_]*\]|inactive' /proc/mdstat; then raid=degraded; else raid=healthy-or-syncing; fi
fi
{
    printf 'format=ventoy-acceptance-v1\n'
    printf 'time=%s\nboot_id=%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" "$boot"
    printf 'hostname=%s\nkernel=%s\n' "$(hostname)" "$(uname -r)"
    printf 'system=%s\n' "$(sed -n 's/^PRETTY_NAME=//p' /etc/os-release | tr -d '\"')"
    printf 'cpu_vendor=%s\n' "$(sed -n 's/^vendor_id[[:space:]]*: //p' /proc/cpuinfo | head -n 1)"
    printf 'mode=%s\nrelease=%s\n' "$(cat "$state/install-mode" 2>/dev/null || echo unknown)" "$(cat "$state/install-release" 2>/dev/null || echo unknown)"
    printf 'disk_boot=%s\nroot_source=%s\nrootfs=%s\n' "$disk_boot" "$root_source" "$rootfs"
    printf 'ssh_active=%s\n' "$ssh_active"
    printf '%s\n' "$sshd_config" | awk '$1=="permitrootlogin" || $1=="passwordauthentication" {print $1 "=" $2}'
    printf 'dhcp_observed=%s\nupdate_policy=%s\nraid=%s\n' "$dhcp" "$policy" "$raid"
    printf 'setup=%s\n' "$(head -n 1 "$state/manual-init/status" 2>/dev/null || echo PRELOADED_ONLY)"
    printf 'setup_sha256=%s\n' "$(cut -d ' ' -f 1 /root/ventoy-linux-setup.run.sha256 2>/dev/null || echo unknown)"
    printf '\n[storage]\n'; lsblk -o NAME,SIZE,TYPE,FSTYPE,MOUNTPOINT
    printf '\n[raid]\n'; cat /proc/mdstat
    printf '\n[addresses]\n'; ip -4 -o addr show scope global
} > "$out"
cat "$out"
printf '\nEvidence saved: %s\n' "$out"
printf 'Also verify a real SSH login, KVM login, filesystem sizes and RAID members. This snapshot does not prove password authentication or disk-failure recovery.\n'
