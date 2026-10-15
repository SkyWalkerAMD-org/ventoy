#!/bin/sh
set -eu
newroot=${1:?Missing live root}
[ -f "$newroot/etc/os-release" ] || exit 1
. "$newroot/etc/os-release"
[ "$ID" = ubuntu ] && [ "$VERSION_ID" = 22.04 ] || exit 1
printf '%s\n' '{{.UserDataSHA}}  /ventoy/autoinstall' | sha256sum -c - >/dev/null || exit 1
grep -qx {{sh .Machine.MAC}} /sys/class/net/*/address || exit 1
[ -f "$newroot/etc/systemd/system/snap.subiquity.subiquity-server.service" ] || exit 1
mkdir -p "$newroot/usr/local/sbin" "$newroot/etc/systemd/system/snap.subiquity.subiquity-server.service.d"
cp /ventoy-center/ubuntu-subiquity.sh "$newroot/usr/local/sbin/ventoy-center-subiquity"
chmod 0755 "$newroot/usr/local/sbin/ventoy-center-subiquity"
cat > "$newroot/etc/systemd/system/snap.subiquity.subiquity-server.service.d/ventoy-center.conf" <<'VCUNIT'
[Service]
ExecStart=
ExecStart=/usr/local/sbin/ventoy-center-subiquity
VCUNIT
echo 'Ventoy Center: task-specific Subiquity autoinstall enabled' > "$newroot/var/log/ventoy-center-autoinstall.log"
