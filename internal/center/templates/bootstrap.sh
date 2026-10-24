#!/bin/bash
set -euo pipefail
umask 077
work=/var/lib/ventoy-center-bootstrap
install -d -m 0700 "$work"
printf '%s' {{sh (b64 .CA)}} | base64 -d > "$work/ca.crt"
request() {
    # network-online.target can precede DHCP on hotplug interfaces (Debian 13).
    # Retry only failures before a connection exists, never HTTP authorization
    # failures or ambiguous timeouts after a one-use request might be accepted.
    local attempt=0 status=0
    while :; do
        if {{if .CA}}curl --cacert "$work/ca.crt"{{else}}curl{{end}} --fail --silent --show-error --connect-timeout 10 --max-time 900 \
            -H {{sh (printf "Authorization: Bearer %s" .Token)}} "$@"; then
            return 0
        else
            status=$?
        fi
        case "$status" in 6|7) ;; *) return "$status";; esac
        attempt=$((attempt + 1))
        [ "$attempt" -lt 12 ] || return "$status"
        sleep 5
    done
}
event() { request -H 'Content-Type: application/json' --data "{\"state\":\"$1\"}" {{sh (printf "%s/event" .Base)}}; }
trap 'event failed >/dev/null 2>&1 || true' ERR
[ "$(hostname | tr '[:lower:]' '[:upper:]')" = {{sh .Machine.Name}} ]
{{.VersionCheck}}
{{if .RAIDSetup}}/bin/bash /usr/local/sbin/ventoy-center-raid --check
{{end}}
{{if .SSHPolicy}}# Check again after cloud-init; a locked account or overridden policy must
# not be reported as a successful root-login installation.
/bin/sh /usr/local/sbin/ventoy-center-root-ssh --check
{{end}}
# A completed task must have the update policy in effect on first boot.
/bin/sh /usr/local/sbin/ventoy-center-update-policy --check
{{if eq .OS "ubuntu"}}[ "$(uname -r)" = "$(cat /var/lib/ventoy-center/kernel-policy/expected-release)" ]
{{end}}
# A successful installer exit must also leave the requested human login account.
[ "$(id -u {{sh .Request.Username}})" -ge 1000 ]
case "$(getent passwd {{sh .Request.Username}} | cut -d: -f7)" in
    ''|*/nologin|*/false|*/true) echo 'Administrator login shell unavailable' >&2; false ;;
esac
{{if .Enroll}}event enrolling
request {{sh (printf "%s/package" .Base)}} -o "$work/node.{{if eq .OS "ubuntu"}}deb{{else}}rpm{{end}}"
printf '%s  %s\n' {{sh .PackageSHA256}} "$work/node.{{if eq .OS "ubuntu"}}deb{{else}}rpm{{end}}" | sha256sum -c -
request {{sh (printf "%s/connection" .Base)}} -o "$work/connection.json"
chmod 0600 "$work/connection.json"
{{if eq .OS "ubuntu"}}DEBIAN_FRONTEND=noninteractive apt-get install -y "$work/node.deb"
{{else}}dnf -y install "$work/node.rpm"
{{end}}fyhocsys-node enroll --file "$work/connection.json"
fyhocsys-node check
systemctl is-active --quiet fyhocsys-node.service
event enrolled
{{else}}event installed
{{end}}trap - ERR
systemctl disable ventoy-center-bootstrap.service
rm -f -- "$work/connection.json" "$work/node.rpm" "$work/node.deb" "$work/ca.crt"
rm -f -- /usr/local/sbin/ventoy-center-bootstrap
