"""Use the target installer's grammar in both single-disk and RAID checks."""

KICKSTART_VERSIONS = {
    **dict.fromkeys(("rhel-7", "centos-7.6", "centos-7.9"), "RHEL7"),
    **dict.fromkeys(("rhel-8", "almalinux-8", "centos-8.0", "centos-8.4",
                     "centos-8.5", "centos-stream-8", "rocky-8"), "RHEL8"),
    **dict.fromkeys(("rhel-9", "almalinux-9", "centos-stream-9", "rocky-9"), "RHEL9"),
    **dict.fromkeys(("centos-stream-10", "rocky-10"), "RHEL10"),
}
