#!/bin/sh
set -eu
umask 077
# d-i may continue after a failed hook. Block the hook instead of returning on error.
trap 'echo "Ventoy Center: installation blocked; inspect /var/log/ventoy-center-guard.log" >&2; while :; do sleep 60; done' EXIT
matched=0
for nic in /sys/class/net/*/address; do
    if [ "$(cat "$nic")" = {{sh .Machine.MAC}} ]; then matched=1; fi
done
[ "$matched" -eq 1 ] || { echo 'MAC mismatch' >&2; exit 1; }
udevadm settle
command -v blkid >/dev/null
disk=
count=0
for block in /sys/block/*; do
    name=${block##*/}
    case "$name" in sd*|hd*|vd*|xvd*|nvme*n*) ;; *) continue;; esac
    properties=$(udevadm info --query=property --path="$block")
    serial=$(printf '%s\n' "$properties" | sed -n 's/^ID_SERIAL_SHORT=//p')
    if [ -z "$serial" ] && [ -r "$block/device/serial" ]; then
        serial=$(sed 's/^[[:space:]]*//;s/[[:space:]]*$//' "$block/device/serial")
    fi
    if [ "$serial" = {{sh .Machine.DiskSerial}} ]; then
        disk=$name
        count=$((count + 1))
    fi
done
[ "$count" -eq 1 ] && [ -b "/dev/$disk" ] || { echo 'Target disk not uniquely identified' >&2; exit 1; }
case "$(readlink -f "/sys/block/$disk")" in */usb*/*) echo 'USB disk rejected' >&2; exit 1;; esac
for holder in /sys/block/"$disk"/holders/* /sys/block/"$disk"/"$disk"*/holders/*; do
    [ ! -e "$holder" ] || { echo 'Active RAID/LVM rejected' >&2; exit 1; }
done
for device in "/dev/$disk" /dev/"$disk"[0-9]* /dev/"$disk"p[0-9]*; do
    [ -b "$device" ] || continue
    fs_type=$(blkid -o value -s TYPE "$device" || true)
    case "$fs_type" in LVM2_member|linux_raid_member) echo 'Existing LVM/RAID requires manual preparation' >&2; exit 1;; esac
done
{{if eq .Machine.Firmware "uefi"}}[ -d /sys/firmware/efi ] || { echo 'UEFI boot required' >&2; exit 1; }
{{else}}[ ! -d /sys/firmware/efi ] || { echo 'Legacy BIOS boot required' >&2; exit 1; }
{{end}}
# BusyBox wget is not sufficient: require the signed installer package with TLS.
anna-install wget-udeb
wget --version | grep -q 'GNU Wget'
printf '%s' {{sh (b64 .CA)}} | base64 -d > /tmp/ventoy-center-ca.crt
test -s /tmp/ventoy-center-ca.crt
wget --ca-certificate=/tmp/ventoy-center-ca.crt --tries=1 --timeout=30 \
    --header={{sh (printf "Authorization: Bearer %s" .Token)}} --header='Content-Type: application/json' \
    --post-data='{"state":"installing"}' -O /tmp/ventoy-center-claim.json {{sh (printf "%s/event" .Base)}}
printf '%s\n' "/dev/$disk" > /tmp/ventoy-center-disk
# Only now may partman select a real disk and accept destructive confirmations.
debconf-set partman-auto/disk "/dev/$disk"
debconf-set grub-installer/bootdev "/dev/$disk"
debconf-set partman/confirm_write_new_label true
debconf-set partman/choose_partition finish
debconf-set partman/confirm true
debconf-set partman/confirm_nooverwrite true
trap - EXIT
