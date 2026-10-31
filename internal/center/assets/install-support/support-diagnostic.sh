#!/bin/sh
# Only named logs are collected. Credentials/configuration files are excluded.
set -u
export LC_ALL=C
umask 077
src=$(CDPATH= cd -- "${0%/*}" && pwd)
kind=${1:-failure}
case "$kind" in failure|observed|result) ;; *) exit 2;; esac
tmp=$(mktemp -d /tmp/ventoy-diagnostic.XXXXXX) || exit 1
trap 'rm -rf -- "$tmp"' EXIT HUP INT TERM
{
    [ ! -f "$src/support-metadata.sh" ] || sh "$src/support-metadata.sh"
    printf 'Ventoy release: '; cat "$src/support-version" 2>/dev/null
    printf 'Kind: %s\nReason: %s\n' "$kind" "${2:-Installer failure detected; see log excerpts}"
    date -u; uname -r
    for mac in /sys/class/net/*/address; do printf 'MAC: '; cat "$mac"; done
    if [ "$kind" = result ]; then
        cat /run/ventoy-install-report.txt 2>/dev/null
    else
        for log in /tmp/ventoy-center-pre.log /tmp/ventoy-center-guard.log /var/log/ventoy-center-guard.log /tmp/ventoy-center-menu.log /var/log/ventoy-center-menu.log /tmp/ventoy-center-setup-preload.log /tmp/storage.log /tmp/anaconda.log /tmp/program.log /var/log/installer/curtin-install.log /var/log/installer/subiquity-server-debug.log /var/log/syslog /var/log/ventoy-manual-init.log; do
            [ -f "$log" ] || continue
            printf '\n--- %s (last 60 lines) ---\n' "$log"
            tail -n 60 "$log"
        done
        printf '\n--- block devices ---\n'
        lsblk -o NAME,SIZE,TYPE,FSTYPE,MOUNTPOINT 2>/dev/null || true
        cat /proc/mdstat 2>/dev/null || true
    fi
} > "$tmp/raw"
# Defense in depth: drop command/config lines with secrets and encoded blobs.
# The receiving server redacts again, including its known task credentials.
sed -E '/[Aa]uthorization|[Bb]earer |[Pp]assword|[Pp]asswd|[Tt]oken|[Ss]ecret|--key|ssh-(rsa|ed25519)|BEGIN .*PRIVATE KEY|\$[1256y]\$/c\[credential-bearing line redacted]' "$tmp/raw" |
    sed -E '/[A-Za-z0-9+\/=]{160}/c\[encoded content redacted]' | head -c 60000 > "$tmp/report"
if [ "$kind" != result ]; then
    cp "$tmp/report" /tmp/ventoy-install-diagnostic.log
    printf '\nVentoy diagnostic saved: /tmp/ventoy-install-diagnostic.log\n'
    printf 'Use Ctrl+Alt+F2 for a shell. No automatic erase/retry will be performed.\n'
    if [ "$kind" = failure ]; then
        {
            printf '\nINSTALLER FAILURE - original log excerpts\n'
            head -n 5 "$tmp/report"
            grep -iE -B 1 -A 2 'error|failed|timeout|existing.*(lvm|raid)|stopped|exception' "$tmp/report" | head -n 30
            printf '\nFull log: /tmp/ventoy-install-diagnostic.log (Ctrl+Alt+F2)\n'
        } > "$tmp/summary"
        cat "$tmp/summary" >&2
        [ ! -c /dev/tty9 ] || cat "$tmp/summary" > /dev/tty9
    fi
fi
[ -s "$src/support-url" ] || exit 0
url=$(cat "$src/support-url")
case "$url" in https://*/diagnostics/v1/report) ;; *) exit 0;; esac
if command -v curl >/dev/null 2>&1; then
    curl --config "$src/support-curl.conf" --cacert "$src/support-ca.crt" --proto '=https' \
        --silent --show-error --fail --connect-timeout 5 --max-time 15 \
        -H 'Content-Type: text/plain' -H "X-Ventoy-Report-Kind: $kind" --data-binary "@$tmp/report" "$url" > "$tmp/receipt" 2>/dev/null
elif command -v wget >/dev/null 2>&1; then
    WGETRC="$src/support-wget.conf" wget --ca-certificate="$src/support-ca.crt" --tries=1 --timeout=15 \
        --header='Content-Type: text/plain' --header="X-Ventoy-Report-Kind: $kind" --post-file="$tmp/report" -O "$tmp/receipt" "$url" 2>/dev/null
else exit 0
fi
rc=$?
if [ "$rc" = 0 ]; then printf 'Diagnostic report saved in Ventoy Center.\n'; fi
exit "$rc"
