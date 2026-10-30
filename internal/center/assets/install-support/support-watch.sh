#!/bin/sh
# Installer-only observer. Never changes task state based on a log match.
set -u
src=$(CDPATH= cd -- "${0%/*}" && pwd)
exec 9>/run/ventoy-install-observer.lock
if command -v flock >/dev/null 2>&1; then flock -n 9 || exit 0; fi
for log in /tmp/anaconda.log /var/log/installer/subiquity-server-debug.log /var/log/installer/curtin-install.log /var/log/syslog; do
    [ -f "$log" ] || continue
    if tail -n 120 "$log" | grep -Eq 'Traceback \(most recent call last\)|curtin.*(failed|FAIL)|InstallationError|Installation failed|installation has failed|base-installer: error'; then
        sig=$(tail -n 120 "$log" | cksum)
        prev=/run/ventoy-install-observed
        [ "$sig" != "$(cat "$prev" 2>/dev/null || true)" ] || continue
        now=$(date +%s)
        last=$(cat /run/ventoy-install-observed-time 2>/dev/null || printf 0)
        [ "$((now-last))" -ge 120 ] || exit 0
        /bin/sh "$src/support-diagnostic.sh" observed "Installer log contains a failure; verify the original log" || true
        printf '%s\n' "$sig" > "$prev"
        printf '%s\n' "$now" > /run/ventoy-install-observed-time
        break
    fi
done
