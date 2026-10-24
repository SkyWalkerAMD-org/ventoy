#!/bin/sh
set -eu
umask 077
. /etc/os-release
[ "$ID" = ubuntu ] && [ "$VERSION_ID" = {{sh .Target.Version}} ]
test -x /usr/bin/ubiquity
printf '%s\n' '{{.UserDataSHA512}}  /ventoy-center-selected.cfg' | sha512sum -c -
# This is a live-only service, activated by the exact selected automatic script.
systemctl mask --runtime --now display-manager.service
{{if .PXEMenu}}python3 -c {{sh .PXECommand}}
{{else}}cp /ventoy-center-selected.cfg /tmp/ventoy-center-selected.cfg
{{.Guard}}
debconf-set-selections /tmp/ventoy-center-selected.cfg
{{end}}
DEBIAN_FRONTEND=noninteractive ubiquity --automatic noninteractive
# Ubiquity does not propagate a failed success_command. Only our late hook can
# create this marker after installed-system policy and bootstrap are verified.
test -f /run/ventoy-center-ubiquity-complete
sync
systemctl reboot
