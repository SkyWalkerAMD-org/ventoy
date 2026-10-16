#!/bin/sh
# Synthetic package metadata for package-manager regression in disposable CI.
set -eu
[ -f /run/ventoy-policy-disposable-root ] || exit 1
testdir=$(mktemp -d /tmp/ventoy-policy-fixture.XXXXXX)
if command -v dpkg-deb >/dev/null 2>&1; then
    mkdir -p "$testdir/package/DEBIAN"
    cat > "$testdir/package/DEBIAN/control" <<'CONTROL'
Package: linux-image-5.15.0-1-generic
Version: 5.15.0-1.1
Architecture: amd64
Maintainer: Fixture <fixture@example.invalid>
Description: Empty kernel metadata fixture; not a bootable kernel
CONTROL
    dpkg-deb --build "$testdir/package" "$testdir/kernel.deb"
    dpkg -i "$testdir/kernel.deb"
else
    mkdir -p "$testdir"/BUILD "$testdir"/BUILDROOT "$testdir"/RPMS "$testdir"/SOURCES "$testdir"/SPECS "$testdir"/SRPMS
    cat > "$testdir/SPECS/kernel.spec" <<'SPEC'
Name: kernel
Version: 99.0
Release: 1
Summary: Empty kernel metadata fixture
License: MIT
BuildArch: noarch
%description
Disposable policy test only. Not a bootable kernel.
%files
SPEC
    rpmbuild -bb --define "_topdir $testdir" "$testdir/SPECS/kernel.spec"
    rpm -i "$testdir"/RPMS/noarch/kernel-*.rpm
fi
