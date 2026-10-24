#!/bin/sh
set -eu
case ${1:-} in
    subiquity)
        exec /usr/bin/snap run --shell subiquity.subiquity-server -c '
set -eu
for site in "$SNAP"/lib/python*/site-packages "$SNAP"/usr/lib/python3*/dist-packages; do
    [ -d "$site/subiquity" ] || continue
    export PYTHONPATH="${PYTHONPATH:+$PYTHONPATH:}$site"
    exec "$PYTHON" /usr/local/lib/ventoy-preload/subiquity-legacy.py
done
echo "Unsupported Subiquity Python layout; manual preload was not installed" >&2
exit 1
'
        ;;
    ubuntu-desktop-bootstrap)
        exec /usr/bin/snap run --shell ubuntu-desktop-bootstrap.subiquity-server -c 'exec /bin/sh /usr/local/lib/ventoy-preload/subiquity-desktop.sh'
        ;;
    *) echo 'Unknown installer service' >&2; exit 1 ;;
esac
