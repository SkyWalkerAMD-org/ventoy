#!/bin/sh
set -eu
umask 077
{{.FailureShell}}
# d-i may continue after a failed hook. Block the hook instead of returning on error.
trap 'vc_install_failed; echo "Ventoy Center: installation blocked; inspect /var/log/ventoy-center-guard.log" >&2; while :; do sleep 60; done' EXIT
. /etc/lsb-release
[ "$DISTRIB_ID" = Debian ] || { echo 'Debian Installer required' >&2; exit 1; }
case "$DISTRIB_RELEASE" in {{.Target.Version}}\ *) ;; *) echo 'Debian Installer version mismatch' >&2; exit 1;; esac
matched=0
for nic in /sys/class/net/*/address; do
    if [ "$(cat "$nic")" = {{sh .Machine.MAC}} ]; then matched=1; fi
done
[ "$matched" -eq 1 ] || { echo 'MAC mismatch' >&2; exit 1; }
udevadm settle
command -v blkid >/dev/null
{{if .StorageGuard}}anna-install lvm2-udeb mdadm-udeb
{{.StorageGuard}}
{{else}}disk=
count=0
for block in /sys/block/*; do
    name=${block##*/}
    case "$name" in sd*|hd*|vd*|xvd*|nvme*n*) ;; *) continue;; esac
    properties=$(udevadm info --query=property --path="$block")
    serial=$(printf '%s\n' "$properties" | sed -n 's/^ID_SERIAL_SHORT=//p')
    for fallback in "$block/device/serial" "$block/serial"; do
        if [ -z "$serial" ] && [ -r "$fallback" ]; then
            serial=$(sed 's/^[[:space:]]*//;s/[[:space:]]*$//' "$fallback")
        fi
    done
    if [ "$serial" = {{sh .Machine.DiskSerial}} ]; then
        disk=$name
        count=$((count + 1))
    fi
done
[ "$count" -eq 1 ] && [ -b "/dev/$disk" ] || { echo 'Target disk not uniquely identified' >&2; exit 1; }
case "$(readlink -f "/sys/block/$disk")" in */usb*/*) echo 'USB disk rejected' >&2; exit 1;; esac
[ "$(cat "/sys/block/$disk/ro")" = 0 ] && [ "$(cat "/sys/block/$disk/removable")" = 0 ] || { echo 'Read-only or removable target rejected' >&2; exit 1; }
for node in "/sys/block/$disk" /sys/block/"$disk"/"$disk"*; do
    [ -r "$node/dev" ] || continue
    minor=$(cat "$node/dev")
    while read -r mount_id parent_id mounted_dev mount_rest; do
        if [ "$mounted_dev" = "$minor" ]; then
            echo 'Mounted target rejected' >&2; exit 1
        fi
    done < /proc/self/mountinfo
    dev="/dev/${node##*/}"
    for swap in $(sed '1d;s/[[:space:]].*//' /proc/swaps); do
        [ "$(readlink -f "$swap")" != "$dev" ] || { echo 'Active swap rejected' >&2; exit 1; }
    done
done
for holder in /sys/block/"$disk"/holders/* /sys/block/"$disk"/"$disk"*/holders/*; do
    [ ! -e "$holder" ] || { echo 'Active RAID/LVM rejected' >&2; exit 1; }
done
for device in "/dev/$disk" /dev/"$disk"[0-9]* /dev/"$disk"p[0-9]*; do
    [ -b "$device" ] || continue
    fs_type=$(blkid -o value -s TYPE "$device" || true)
    case "$fs_type" in LVM2_member|linux_raid_member) echo 'Existing LVM/RAID requires manual preparation' >&2; exit 1;; esac
done
{{end}}{{if eq .Machine.Firmware "uefi"}}[ -d /sys/firmware/efi ] || { echo 'UEFI boot required' >&2; exit 1; }
{{else}}[ ! -d /sys/firmware/efi ] || { echo 'Legacy BIOS boot required' >&2; exit 1; }
{{end}}
{{.StorageSizing}}
# BusyBox wget is not sufficient: require the signed installer package with TLS.
anna-install wget-udeb
wget --version | grep -q 'GNU Wget'
printf '%s' {{sh .CA}} > /tmp/ventoy-center-ca.crt
test -s /tmp/ventoy-center-ca.crt
wget --ca-certificate=/tmp/ventoy-center-ca.crt --tries=1 --timeout=30 \
    --header={{sh (printf "Authorization: Bearer %s" .Token)}} --header='Content-Type: application/json' \
    --post-data='{"state":"installing"}' -O /tmp/ventoy-center-claim.json {{sh (printf "%s/event" .Base)}}
printf '%s\n' "/dev/$disk" > /tmp/ventoy-center-disk
# Only now may partman select a real disk and accept destructive confirmations.
# Some partitioner questions have not been registered at this early hook.
# The preseed loader registers them with their types, retaining the answers
# when the corresponding partman component loads later.
cat > /tmp/ventoy-center-target.cfg <<VC_TARGET
d-i partman-auto/disk string /dev/$disk
d-i grub-installer/bootdev string /dev/$disk
d-i partman/confirm_write_new_label boolean true
d-i partman/choose_partition select finish
d-i partman/confirm boolean true
d-i partman/confirm_nooverwrite boolean true
VC_TARGET
debconf-set-selections /tmp/ventoy-center-target.cfg
rm -f /tmp/ventoy-center-target.cfg
{{.StorageRecipe}}
trap - EXIT
