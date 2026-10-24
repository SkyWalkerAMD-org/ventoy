#!/bin/sh
# Run inside the newly installed target; never in the live installer's SSH server.
set -eu
umask 077
check() {
    # Socket-activated sshd may not have started yet on Ubuntu 24.04+.
    mkdir -p /run/sshd
    chmod 0755 /run/sshd
    /usr/sbin/sshd -t
    effective=$(/usr/sbin/sshd -T -C user=root,host=localhost,addr=127.0.0.1)
    printf '%s\n' "$effective" | grep -qx 'permitrootlogin yes'
    printf '%s\n' "$effective" | grep -qx 'passwordauthentication yes'
    secret=$(getent shadow root | cut -d: -f2)
    case "$secret" in ''|'!'*|'*'*) echo 'Root password is unavailable' >&2; return 1;; esac
}
case "${1:---check}" in
    --apply)
        # Prepend global settings: this also works on Debian 10, whose sshd
        # does not use the newer sshd_config.d Include convention.
        cfg=/etc/ssh/sshd_config
        test -f "$cfg" && test ! -L "$cfg"
        tmp=$(mktemp /etc/ssh/.ventoy-root-ssh.XXXXXX)
        trap 'rm -f -- "$tmp"' EXIT
        {
            printf '%s\n' '# BEGIN VENTOY ROOT SSH' 'PermitRootLogin yes' 'PasswordAuthentication yes' '# END VENTOY ROOT SSH'
            sed '/^# BEGIN VENTOY ROOT SSH$/,/^# END VENTOY ROOT SSH$/d' "$cfg"
        } > "$tmp"
        chmod --reference="$cfg" "$tmp"
        chown --reference="$cfg" "$tmp"
        mv -f -- "$tmp" "$cfg"
        trap - EXIT
        if command -v restorecon >/dev/null 2>&1; then restorecon "$cfg"; fi
        if [ -d /etc/cloud/cloud.cfg.d ]; then
            printf '%s\n' '#cloud-config' 'disable_root: false' 'ssh_pwauth: true' > /etc/cloud/cloud.cfg.d/99-ventoy-root-ssh.cfg
        fi
        mkdir -p /run/sshd
        chmod 0755 /run/sshd
        ssh-keygen -A
        check
{{if eq .Target.Installer "kickstart"}}        systemctl enable sshd.service
{{else}}        systemctl enable ssh.service
{{end}}
        ;;
    --check) check ;;
    *) exit 2 ;;
esac
