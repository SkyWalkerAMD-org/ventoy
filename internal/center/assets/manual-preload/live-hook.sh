#!/bin/sh
set -eu
# iVentoy runs this in initramfs. Debian d-i has its own finish-install hook.
# dracut uses the pre-pivot hook; casper needs a hook after its live root exists.
[ -f /scripts/casper ] || exit 0
[ -f /init ] || exit 1
grep -q '/ventoy-manual-preload/prepare-live.sh' /init && exit 0
[ "$(grep -c '^[[:space:]]*maybe_break init$' /init)" = 1 ] || exit 1
sed -i '/^[[:space:]]*maybe_break init$/i /ventoy/busybox/sh /ventoy-manual-preload/prepare-live.sh "$rootmnt"' /init
