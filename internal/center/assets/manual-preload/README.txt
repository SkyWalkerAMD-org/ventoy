Ventoy Linux setup — preloaded, not applied

Run the full setup and automatic checks: sudo /root/ventoy-linux-setup.run
To install/update only the center's matching software, add --software.
See /root/README-ventoy-software.txt for package selection and checks.

The installer copied this package into the installed system. No setup policy
has been applied by the preload hook. After booting the installed system, run:

  sudo /root/ventoy-linux-setup.run

Optionally set a hostname: sudo /root/ventoy-linux-setup.run --hostname server01

Replace server01 with your desired hostname, or omit --hostname to keep the
installation hostname. Default accounts after setup: admin / ventoy and
root / ventoy. Use --keep-passwords to preserve existing account passwords.

The package configures and checks SSH, hostname, DHCP on unconfigured wired
interfaces, kernel locking, disabled automatic updates, CLI and E610 drivers,
then installs and verifies the center's matching default software group.
The script prints VERIFIED or PENDING, and does not automatically reboot.

Before running setup, login still uses the accounts/passwords chosen during manual
installation. Preloading does not enable SSH or change those passwords.

Verify package integrity:
  cd /root && sha256sum -c ventoy-linux-setup.run.sha256

--apply remains a compatible alias for full setup. Subsequent check:
  sudo bash /opt/ventoy-manual/current/install.sh --check
