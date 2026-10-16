#!/bin/sh
set -eu
# Preserve snap's own environment, mount namespace and Python interpreter.
# The 22.04.3 snap launcher discards arguments, so call its Python module
# through snap's shell entry point instead. No installed snap file is edited.
exec /usr/bin/snap run --shell subiquity.subiquity-server -c '
set -eu
for site in "$SNAP"/lib/python*/site-packages; do
    [ -d "$site/subiquity" ] || continue
    export PYTHONPATH="${PYTHONPATH:+$PYTHONPATH:}$site"
    exec "$PYTHON" -m subiquity.cmd.server --kernel-cmdline "$(cat /proc/cmdline) autoinstall"
done
echo "Ventoy Center: unsupported Subiquity snap layout" >&2
exit 1
'
