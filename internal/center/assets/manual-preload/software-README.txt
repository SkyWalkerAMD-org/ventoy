Ventoy node software

After booting the installed Linux system, run as root or with sudo:

  sudo bash /root/ventoy-linux-setup.run

The preloaded setup selects matching RPM/DEB packages from the center's current
software directory. It installs/upgrades the newest compatible versions and
skips equal/newer installed versions. Missing dependencies may be obtained from
the system's configured repositories. Kernel locks and disabled automatic-update
settings remain in place. No scheduled application updates are enabled.

  sudo bash /root/ventoy-linux-setup.run --list-software
  sudo bash /root/ventoy-linux-setup.run --check-software

This software-only command does not reset accounts, hostname or networking.
For the complete manual-install system configuration, explicitly use --apply.
The installer only preloads setup; it does not run it or install these packages.
Changing packages on the server does not require downloading setup again.
