#!/bin/sh
# Runs while the installer runtime is mounted, before Anaconda is started.
[ -f /sysroot/etc/os-release ] || return 0
/bin/sh /ventoy-manual-preload/prepare-live.sh /sysroot
