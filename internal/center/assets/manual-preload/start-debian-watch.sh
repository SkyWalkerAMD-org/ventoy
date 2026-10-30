#!/bin/sh
# Executed by rootskel early-boot hooks; never sourced into the installer shell.
if [ -f /ventoy-manual-preload/support-watch.sh ]; then
    (
        while :; do
            sleep 30
            /bin/sh /ventoy-manual-preload/support-watch.sh || true
        done
    ) </dev/null >/dev/null 2>&1 &
fi
exit 0
