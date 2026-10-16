#!/bin/sh
set -eu
umask 077
printf '%s' {{sh (b64 .UpdatePolicy)}} | base64 -d > /usr/local/sbin/ventoy-center-update-policy
chmod 0700 /usr/local/sbin/ventoy-center-update-policy
/bin/sh /usr/local/sbin/ventoy-center-update-policy --apply --offline
printf '%s' {{sh (b64 .Bootstrap)}} | base64 -d > /usr/local/sbin/ventoy-center-bootstrap
chmod 0700 /usr/local/sbin/ventoy-center-bootstrap
cat > /etc/systemd/system/ventoy-center-bootstrap.service <<'VCUNIT'
[Unit]
Description=Ventoy Center installation handoff
Wants=network-online.target
After=network-online.target
ConditionPathExists=/usr/local/sbin/ventoy-center-bootstrap
[Service]
Type=oneshot
ExecStart=/usr/local/sbin/ventoy-center-bootstrap
TimeoutStartSec=30min
UMask=0077
[Install]
WantedBy=multi-user.target
VCUNIT
systemctl enable ventoy-center-bootstrap.service
{{if .Request.SSHKey}}install -d -m 0700 -o {{sh .Request.Username}} -g {{sh .Request.Username}} /home/{{.Request.Username}}/.ssh
printf '%s\n' {{sh .Request.SSHKey}} > /home/{{.Request.Username}}/.ssh/authorized_keys
chown {{.Request.Username}}:{{.Request.Username}} /home/{{.Request.Username}}/.ssh/authorized_keys
chmod 0600 /home/{{.Request.Username}}/.ssh/authorized_keys
{{end}}
