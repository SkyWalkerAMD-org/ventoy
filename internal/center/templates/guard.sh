set -eu
umask 077
test -d /sys/class/net
matched=0
for nic in /sys/class/net/*/address; do
    if [ "$(cat "$nic")" = {{sh .Machine.MAC}} ]; then matched=1; fi
done
[ "$matched" -eq 1 ] || { echo 'MAC mismatch; installation stopped' >&2; exit 1; }
disk=$(lsblk -dn -o NAME,SERIAL | awk -v serial={{sh .Machine.DiskSerial}} '$2 == serial {print $1}')
[ "$(printf '%s\n' "$disk" | wc -l)" -eq 1 ] && [ -n "$disk" ] || { echo 'Target disk not uniquely identified' >&2; exit 1; }
case "$disk" in *[!a-zA-Z0-9_-]*) echo 'Invalid disk name' >&2; exit 1;; esac
[ -b "/dev/$disk" ] || exit 1
[ "$(lsblk -dn -o TRAN "/dev/$disk" | tr -d ' ')" != usb ] || { echo 'USB disk rejected' >&2; exit 1; }
{{if eq .Machine.Firmware "uefi"}}[ -d /sys/firmware/efi ] || { echo 'UEFI boot required' >&2; exit 1; }
{{else}}[ ! -d /sys/firmware/efi ] || { echo 'Legacy BIOS boot required' >&2; exit 1; }
{{end}}
printf '%s' {{sh (b64 .CA)}} | base64 -d > /tmp/ventoy-center-ca.crt
{{if .CA}}curl --cacert /tmp/ventoy-center-ca.crt{{else}}curl{{end}} --fail --silent --show-error --connect-timeout 10 --max-time 30 \
    -H {{sh (printf "Authorization: Bearer %s" .Token)}} -H 'Content-Type: application/json' \
    --data '{"state":"installing"}' {{sh (printf "%s/event" .Base)}}
