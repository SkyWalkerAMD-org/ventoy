#!/bin/sh
# SPDX-License-Identifier: GPL-2.0-only
# mokutil 0.6 and newer releases invert --test-key's 0/1 exit meanings.
# Only an explicit, exact result is accepted; crashes and blocked keys fail.
e610_mok_status() {
    e610_mok_rc=0
    e610_mok_output=$(LC_ALL=C mokutil --test-key "$1" 2>&1) || e610_mok_rc=$?
    printf '%s\n' "$e610_mok_output" >&2
    case "$e610_mok_rc" in 0|1) ;; *) return 3 ;; esac
    case "$e610_mok_output" in
        "$1 is already enrolled") return 0 ;;
        "$1 is not enrolled") return 1 ;;
        "$1 is already in the enrollment request") return 2 ;;
        *) return 3 ;;
    esac
}
