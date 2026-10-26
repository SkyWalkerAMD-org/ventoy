#!/bin/sh
# Only installer runtime files are prepared here. No target OS policy is run.
set -eu
umask 077
root=${1:?Missing installer root}
# Older Anaconda initramfs lacks dirname; use only shell builtins here.
src=${0%/*}
[ "$src" != "$0" ] || src=.
src=$(CDPATH= cd -- "$src" && pwd)
[ "$root" != / ] && [ -f "$root/etc/os-release" ] || exit 1
mkdir -p "$root/usr/local/lib/ventoy-preload"
dest=$root/usr/local/lib/ventoy-preload
for name in ventoy-linux-setup.run setup.sha256 copy-to-target.sh README.txt software-README.txt subiquity-launch.sh subiquity-legacy.py subiquity-desktop.sh finish-subiquity ubuntu-reboot.sh linux-update-policy.sh target-update-policy.sh ubuntu-kernel.py manual-ubuntu-policy.py; do
    cp -p "$src/$name" "$dest/$name"
done
if [ -d "$root/usr/share/anaconda/post-scripts" ]; then
    cp -p "$src/finish-anaconda.ks" "$root/usr/share/anaconda/post-scripts/99-ventoy-preload.ks"
elif [ -x "$root/usr/bin/ubiquity" ]; then
    mkdir -p "$root/usr/lib/ubiquity/target-config"
    cp -p "$src/finish-ubiquity" "$root/usr/lib/ubiquity/target-config/99ventoy-preload"
else
    # Subiquity's built-in completion hook runs for interactive installations.
    mkdir -p "$root/etc/subiquity/postinst.d"
    cp -p "$src/finish-subiquity" "$root/etc/subiquity/postinst.d/99-ventoy-preload"
    app=''
    for snap in "$root"/var/lib/snapd/snaps/ubuntu-desktop-bootstrap_*.snap; do
        [ ! -f "$snap" ] || app=ubuntu-desktop-bootstrap
    done
    if [ -z "$app" ] && grep -Eq '^VERSION_ID="?20\.04"?$' "$root/etc/os-release"; then
        # The 20.04.3 installer predates Subiquity's postinst.d API.
        app=subiquity
    fi
    if [ -n "$app" ]; then
        unit=$root/etc/systemd/system/snap.$app.subiquity-server.service.d
        mkdir -p "$unit"
        printf '[Service]\nExecStart=\nExecStart=/bin/sh /usr/local/lib/ventoy-preload/subiquity-launch.sh %s\n' "$app" > "$unit/90-ventoy-preload.conf"
    fi
fi
# A selected automatic script has its own policy and answers. Do not shadow it.
if [ ! -f /ventoy/autoinstall ] && [ ! -f "$root/ventoy-center-selected.cfg" ] && [ ! -x "$root/usr/bin/ubiquity" ] && [ ! -d "$root/usr/share/anaconda/post-scripts" ] && [ "${app:-}" != ubuntu-desktop-bootstrap ]; then
    unit=$root/etc/systemd/system/snap.subiquity.subiquity-server.service.d
    mkdir -p "$unit"
    printf '[Service]\nExecStartPre=/usr/bin/python3 /usr/local/lib/ventoy-preload/manual-ubuntu-policy.py\n' > "$unit/80-ventoy-manual-update-policy.conf"
fi
echo 'Ventoy manual setup completion hook prepared; installer remains interactive.'
