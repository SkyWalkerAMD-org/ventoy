%post --nochroot --erroronfail --log=/tmp/ventoy-manual-preload.log
set -eu
if [ -f /mnt/sysroot/etc/os-release ]; then
    target=/mnt/sysroot
else
    target=/mnt/sysimage
fi
/bin/sh /usr/local/lib/ventoy-preload/copy-to-target.sh "$target"
%end
