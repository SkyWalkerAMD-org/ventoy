Ventoy Linux setup — preloaded, not applied

To install/update the center's matching software without resetting system
configuration, run: sudo bash /root/ventoy-linux-setup.run
See /root/README-ventoy-software.txt for package selection and checks.

The installer copied this package into the installed system. No setup policy
has been applied by the preload hook. After booting the installed system, run:

  sudo bash /root/ventoy-linux-setup.run --apply --hostname server01

Replace server01 with your desired hostname, or omit --hostname to keep the
installation hostname. Default accounts after --apply: admin / ventoy and
root / ventoy. Use --keep-passwords to preserve existing account passwords.

The package configures and checks SSH, hostname, DHCP on unconfigured wired
interfaces, kernel locking, disabled automatic updates, CLI and E610 drivers.
The script prints VERIFIED or PENDING, and does not automatically reboot.

Before --apply, login still uses the accounts/passwords chosen during manual
installation. Preloading does not enable SSH or change those passwords.

Verify package integrity:
  cd /root && sha256sum -c ventoy-linux-setup.run.sha256

Subsequent check after --apply:
  sudo bash /opt/ventoy-manual/current/install.sh --check
