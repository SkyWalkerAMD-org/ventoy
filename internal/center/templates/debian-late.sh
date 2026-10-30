#!/bin/sh
set -eu
umask 077
{{.FailureShell}}
mkdir -p /var/lib/ventoy-center
printf 'automatic\n' > /var/lib/ventoy-center/install-mode
trap 'vc_result=$?; [ "$vc_result" = 0 ] || vc_install_failed' EXIT
{{if .RAIDSetup}}printf '%s' {{sh (b64 .RAIDSetup)}} | base64 -d > /usr/local/sbin/ventoy-center-raid
chmod 0700 /usr/local/sbin/ventoy-center-raid
/bin/bash /usr/local/sbin/ventoy-center-raid --apply
{{end}}
# The reusable PXE preseed uses a temporary hostname until it binds this machine.
printf '%s\n' {{sh .Machine.Name}} > /etc/hostname
sed -i '/^127\.0\.1\.1[[:space:]]/d' /etc/hosts
printf '127.0.1.1\t%s\n' {{sh .Machine.Name}} >> /etc/hosts
{{if eq .Request.Username "admin"}}# user-setup cannot create its reserved name admin directly.
if getent passwd admin >/dev/null; then
    echo 'Target already has an admin account; refusing replacement' >&2
    exit 1
fi
[ "$(id -u vcinstall)" -ge 1000 ]
usermod --login admin --home /home/admin --move-home vcinstall
if getent group admin >/dev/null; then
    usermod --gid admin admin
    groupdel vcinstall
else
    groupmod --new-name admin vcinstall
fi
usermod --append --groups sudo admin
[ "$(id -u admin)" -ge 1000 ]
if getent passwd vcinstall >/dev/null; then exit 1; fi
{{end}}
{{if .SSHPolicy}}printf '%s' {{sh (b64 .SSHPolicy)}} | base64 -d > /usr/local/sbin/ventoy-center-root-ssh
chmod 0700 /usr/local/sbin/ventoy-center-root-ssh
/bin/sh /usr/local/sbin/ventoy-center-root-ssh --apply
{{end}}printf '%s' {{sh (b64 .ServerInit)}} | base64 -d > /usr/local/sbin/safe-server-init
chmod 0755 /usr/local/sbin/safe-server-init
printf '%s' {{sh (b64 .UpdatePolicy)}} | base64 -d > /usr/local/sbin/ventoy-center-update-policy
chmod 0700 /usr/local/sbin/ventoy-center-update-policy
/bin/sh /usr/local/sbin/ventoy-center-update-policy --apply --offline
systemctl set-default multi-user.target
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
