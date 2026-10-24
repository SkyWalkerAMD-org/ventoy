#!/bin/sh
set -eu
original=$SNAP/bin/subiquity-server
hooks=/run/ventoy-preload-postinst
needle='--postinst-hooks-dir=$SNAP/etc/subiquity/postinst.d'
[ -f "$original" ] && [ -d "$SNAP/etc/subiquity/postinst.d" ] || exit 1
[ "$(grep -Fc -- "$needle" "$original")" = 1 ] || {
    echo 'Unsupported desktop installer launcher; manual preload was not installed' >&2
    exit 1
}
mkdir -p "$hooks"
cp -a "$SNAP/etc/subiquity/postinst.d/." "$hooks/"
[ ! -e "$SNAP/etc/subiquity/postinst.d/99-ventoy-preload" ] || exit 1
cp /usr/local/lib/ventoy-preload/finish-subiquity "$hooks/99-ventoy-preload"
chmod 0755 "$hooks/99-ventoy-preload"
# Retain the original launcher environment, management script and all options.
# Only its native completion-hook directory is changed; original hooks remain.
# Keep $0 at the snap launcher path for its SCRIPT_DIR calculation.
exec /bin/bash -c "$(sed 's@--postinst-hooks-dir=$SNAP/etc/subiquity/postinst.d@--postinst-hooks-dir=/run/ventoy-preload-postinst@' "$original")" "$original"
