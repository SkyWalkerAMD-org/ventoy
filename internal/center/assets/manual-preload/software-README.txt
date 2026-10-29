Ventoy node software

Matching DEB/RPM files are retained with SHA-256 verification and mode 0600 in
/root/ventoy-tools, even if installation fails. Missing copies are downloaded
again without reinstalling packages already at the requested version or newer.
If automatic enrollment is configured, setup joins installed sckocstp/fyhocsys
0.8.6+ nodes using a private key file and checks their native boot services.
--check-software checks both retained packages and configured node bindings.

After booting the installed Linux system, run as root or with sudo:

  sudo /root/ventoy-linux-setup.run

With no arguments, setup applies and checks SSH, hostname, DHCP, kernel locking,
disabled automatic updates, CLI and E610 status, then installs matching software.
The preloaded setup selects matching RPM/DEB packages from the center's current
software directory. It installs/upgrades the newest compatible versions and
skips equal/newer installed versions. Missing dependencies may be obtained from
the system's configured repositories. Kernel locks and disabled automatic-update
settings remain in place. No scheduled application updates are enabled.

  sudo bash /root/ventoy-linux-setup.run --list-software
  sudo bash /root/ventoy-linux-setup.run --check-software

For software installation/upgrades only, use:
  sudo /root/ventoy-linux-setup.run --software
This explicit software-only command does not reset accounts, hostname or networking.
--apply remains a compatible alias for the default full setup.
The installer only preloads setup; it does not run it or install these packages.
Changing packages on the server does not require downloading setup again.
