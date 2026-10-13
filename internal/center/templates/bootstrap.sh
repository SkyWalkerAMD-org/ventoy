#!/bin/bash
set -euo pipefail
umask 077
work=/var/lib/ventoy-center-bootstrap
install -d -m 0700 "$work"
printf '%s' {{sh (b64 .CA)}} | base64 -d > "$work/ca.crt"
request() {
    {{if .CA}}curl --cacert "$work/ca.crt"{{else}}curl{{end}} --fail --silent --show-error --connect-timeout 10 --max-time 900 \
        -H {{sh (printf "Authorization: Bearer %s" .Token)}} "$@"
}
event() { request -H 'Content-Type: application/json' --data "{\"state\":\"$1\"}" {{sh (printf "%s/event" .Base)}}; }
trap 'event failed >/dev/null 2>&1 || true' ERR
[ "$(hostname | tr '[:lower:]' '[:upper:]')" = {{sh .Machine.Name}} ]
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
