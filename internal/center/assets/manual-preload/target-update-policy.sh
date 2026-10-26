#!/bin/sh
# Only the kernel/APT policy is applied here; never run the full setup.
set -eu
src=${0%/*}
target=${1:?Missing installed target}
[ "$target" != / ] && [ -f "$target/etc/os-release" ] || exit 1
target=$(readlink -f "$target")
[ "$target" != / ] || exit 1
ID=''
. "$target/etc/os-release"
case "$ID" in ubuntu|debian) ;; *) exit 0 ;; esac
for dir in usr usr/local usr/local/sbin var var/lib var/lib/ventoy-center; do
    [ ! -L "$target/$dir" ] || { echo 'Unsafe update-policy destination' >&2; exit 1; }
done
mkdir -p "$target/usr/local/sbin" "$target/var/lib/ventoy-center"
policy=$target/usr/local/sbin/ventoy-center-update-policy
marker=$target/var/lib/ventoy-center/manual-install-update-policy
[ ! -L "$policy" ] && [ ! -L "$marker" ] || exit 1
cp "$src/linux-update-policy.sh" "$policy"
chmod 0755 "$policy"
chroot "$target" /bin/sh /usr/local/sbin/ventoy-center-update-policy --apply --offline
printf '%s\n' 'APPLIED_BEFORE_FIRST_BOOT' > "$marker"
echo 'Ventoy: installed kernel locked and APT automatic updates disabled before first boot.'
