#!/bin/sh
set -eu
newroot=${1:?Missing live root}
[ -f "$newroot/etc/os-release" ] || exit 1
. "$newroot/etc/os-release"
[ "$ID" = ubuntu ] && [ "$VERSION_ID" = {{sh .Target.Version}} ] || exit 1
printf '%s\n' '{{.UserDataSHA512}}  /ventoy/autoinstall' | sha512sum -c - || exit 1
{{if not .PXEMenu}}grep -qx {{sh .Machine.MAC}} /sys/class/net/*/address || exit 1
{{end}}
{{if eq .Target.Installer "ubiquity"}}[ -x "$newroot/usr/bin/ubiquity" ] || exit 1
mkdir -p "$newroot/usr/local/sbin" "$newroot/etc/systemd/system/multi-user.target.wants"
cp /ventoy-center/ubuntu-ubiquity.sh "$newroot/usr/local/sbin/ventoy-center-ubiquity"
cp /ventoy/autoinstall "$newroot/ventoy-center-selected.cfg"
chmod 0600 "$newroot/ventoy-center-selected.cfg"
chmod 0700 "$newroot/usr/local/sbin/ventoy-center-ubiquity"
cat > "$newroot/etc/systemd/system/ventoy-center-ubiquity.service" <<'VCUNIT'
[Unit]
Description=Ventoy Center automatic Desktop installation
Wants=network-online.target
After=network-online.target NetworkManager-wait-online.service
ConditionPathExists=/ventoy-center-selected.cfg
[Service]
Type=oneshot
ExecStart=/usr/local/sbin/ventoy-center-ubiquity
TimeoutStartSec=infinity
StandardOutput=journal+console
StandardError=journal+console
[Install]
WantedBy=multi-user.target
VCUNIT
ln -s ../ventoy-center-ubiquity.service "$newroot/etc/systemd/system/multi-user.target.wants/ventoy-center-ubiquity.service"
{{else}}unit=snap.{{if .Target.Desktop}}ubuntu-desktop-bootstrap{{else}}subiquity{{end}}.subiquity-server.service
[ -f "$newroot/etc/systemd/system/$unit" ] || exit 1
mkdir -p "$newroot/usr/local/sbin" "$newroot/etc/systemd/system/$unit.d"
cp /ventoy-center/ubuntu-subiquity.sh "$newroot/usr/local/sbin/ventoy-center-subiquity"
chmod 0755 "$newroot/usr/local/sbin/ventoy-center-subiquity"
{{if .Target.Desktop}}cp /ventoy/autoinstall "$newroot/autoinstall.yaml"
chmod 0600 "$newroot/autoinstall.yaml"
{{end}}cat > "$newroot/etc/systemd/system/$unit.d/ventoy-center.conf" <<'VCUNIT'
[Service]
ExecStart=
ExecStart=/usr/local/sbin/ventoy-center-subiquity
VCUNIT
{{end}}
echo 'Ventoy Center: task-specific Subiquity autoinstall enabled' > "$newroot/var/log/ventoy-center-autoinstall.log"
