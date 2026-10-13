#!/bin/sh
set -eu
[ "$(id -u)" -eq 0 ] || { echo 'Run as root.' >&2; exit 1; }
[ "$#" -eq 1 ] || { echo 'Usage: install-linux.sh /path/to/ventoy-center' >&2; exit 2; }
source_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
binary=$1
[ -f "$binary" ] && [ -x "$binary" ] || { echo 'Provide the executable Linux binary.' >&2; exit 1; }
"$binary" version
command -v systemctl >/dev/null
getent group ventoy-center >/dev/null || groupadd --system ventoy-center
id ventoy-center >/dev/null 2>&1 || useradd --system --gid ventoy-center --home-dir /var/lib/ventoy-center --shell /usr/sbin/nologin ventoy-center
install -m 0755 "$binary" /usr/local/bin/ventoy-center
runtime=/var/lib/ventoy-center
if [ ! -e "$runtime/config.json" ]; then
    /usr/local/bin/ventoy-center init --dir "$runtime"
fi
chown ventoy-center:ventoy-center "$runtime" "$runtime/config.json" "$runtime/admin.token" "$runtime/data" "$runtime/iso" "$runtime/packages"
chmod 0700 "$runtime" "$runtime/data" "$runtime/iso" "$runtime/packages"
chmod 0600 "$runtime/config.json" "$runtime/admin.token"
install -m 0644 "$source_dir/ventoy-center.service" /etc/systemd/system/ventoy-center.service
systemctl daemon-reload
echo 'Installed. Configure /var/lib/ventoy-center/config.json and TLS first.'
echo 'Then run: systemctl enable --now ventoy-center'
echo 'Admin token: /var/lib/ventoy-center/admin.token (private; do not publish).'
