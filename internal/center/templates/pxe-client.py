#!/usr/bin/env python
# Python 2.7 (CentOS 7) and Python 3. No writes to target disks here.
from __future__ import print_function
import base64
import glob
import io
import json
import os
import re
import select
import shlex
import socket
import subprocess
import sys
import tempfile
try:
    from urllib.parse import urlsplit
except ImportError:
    from urlparse import urlsplit

SETTINGS = json.loads(base64.b64decode('{{.PXESettings}}').decode('utf-8'))
CONSOLE_VT = '9'
CONSOLE_PATH = '/dev/tty' + CONSOLE_VT

def reserve_console():
    # logind starts autovt@ when changing VTs; masking only getty@ does not
    # block that alias. VT6 is also reserved for an emergency login by default.
    # Keep the installer/login terminals available and reserve an unused VT.
    units = [kind + '@tty' + CONSOLE_VT + '.service' for kind in ('getty', 'autovt')]
    subprocess.check_call(['systemctl', 'mask', '--runtime'] + units)
    subprocess.check_call(['systemctl', 'stop'] + units)

def output(args):
    return subprocess.check_output(args).decode('utf-8', 'replace').strip()

def read(path):
    with io.open(path, encoding='utf-8') as f:
        return f.read().strip()

def disk_location(row):
    device = os.path.realpath('/sys/class/block/' + row['NAME'])
    transport = row['TRAN'].lower()
    if row['RM'] != '0' or transport == 'usb' or re.search(r'/usb[0-9]+/', device):
        return 'external'
    # Network and virtual block devices must not become implicit install targets.
    if (re.search(r'/session[0-9]+/', device) or '/virtual/' in device or
            transport in ('iscsi', 'fc', 'fcoe', 'tcp', 'rdma')):
        return 'unknown'
    if transport in ('ata', 'sata', 'sas', 'spi', 'nvme', 'virtio'):
        return 'internal'
    # Hardware RAID logical disks and virtio disks can have no TRAN value.
    if transport == '' and re.search(r'/pci[0-9a-f:]+/', device):
        return 'internal'
    return 'unknown'

def write(path, content):
    if isinstance(content, bytes):
        content = content.decode('utf-8')
    with io.open(path, 'w', encoding='utf-8', newline='\n') as f:
        f.write(content)
    os.chmod(path, 0o600)

def boot_mac():
    for arg in read('/proc/cmdline').split():
        if arg.startswith('BOOTIF=01-'):
            return arg[len('BOOTIF=01-'):].replace('-', ':').lower()
    # Some iVentoy firmware paths do not set BOOTIF. The live system's route
    # back to this controller identifies the working deployment interface.
    host = socket.gethostbyname(urlsplit(SETTINGS['url']).hostname)
    route = output(['ip', '-o', 'route', 'get', host]).split()
    nic = route[route.index('dev') + 1]
    if not re.match(r'^[A-Za-z0-9_.:-]+$', nic):
        raise RuntimeError('Cannot determine deployment interface')
    return read('/sys/class/net/' + nic + '/address').lower()

class Console(object):
    def __init__(self):
        import termios
        # select() observes the tty, not Python's read-ahead buffer. Keep input
        # unbuffered so pasted lines cannot disappear from the next prompt.
        self.reader = open(CONSOLE_PATH, 'rb', 0)
        self.writer = open(CONSOLE_PATH, 'w')
        attrs = termios.tcgetattr(self.reader.fileno())
        attrs[3] |= termios.ICANON | termios.ECHO | termios.ISIG
        termios.tcsetattr(self.reader.fileno(), termios.TCSANOW, attrs)
        termios.tcflush(self.reader.fileno(), termios.TCIFLUSH)
    def write(self, value):
        return self.writer.write(value)
    def flush(self):
        return self.writer.flush()
    def fileno(self):
        return self.reader.fileno()
    def readline(self, count):
        return self.reader.readline(count).decode('utf-8', 'replace')
    def close(self):
        self.reader.close()
        self.writer.close()

def inventory(mac):
    if not re.match(r'^[0-9a-f]{2}(:[0-9a-f]{2}){5}$', mac):
        raise RuntimeError('iVentoy did not supply a valid boot MAC')
    if mac not in [read(p) for p in glob.glob('/sys/class/net/*/address')]:
        raise RuntimeError('Boot MAC does not belong to this machine')
    subprocess.check_call(['udevadm', 'settle'])
    disks = []
    for line in output(['lsblk', '-bdnP', '-o', 'NAME,TYPE,SIZE,SERIAL,MODEL,TRAN,RO,RM']).splitlines():
        row = dict(part.split('=', 1) for part in shlex.split(line))
        if row['TYPE'] != 'disk' or int(row['SIZE']) <= 0:
            continue
        name = row['NAME']
        if not re.match(r'^[A-Za-z0-9_-]+$', name):
            raise RuntimeError('Unexpected disk name')
        # RHEL 9 installers expose compressed RAM swap as TYPE=disk. It is
        # installer memory, not a second installation disk. Keep other virtual
        # devices (especially network block devices) in the unknown inventory.
        if (re.match(r'^(?:zram|ram)[0-9]+$', name) and
                os.path.realpath('/sys/class/block/' + name) ==
                '/sys/devices/virtual/block/' + name):
            continue
        serial = row['SERIAL'].strip()
        fallback = '/sys/block/' + name + '/serial'
        if not serial and os.path.isfile(fallback):
            serial = read(fallback)
        blocked = ''
        location = disk_location(row)
        if location != 'internal':
            blocked = 'External disk or unverified internal connection'
        elif row['RO'] != '0':
            blocked = 'Read-only disk'
        elif not re.match(r'^[A-Za-z0-9_.:/-]{1,120}$', serial):
            blocked = 'Missing or unsupported serial'
        elif output(['lsblk', '-nr', '-o', 'MOUNTPOINT', '/dev/' + name]).strip():
            blocked = 'Mounted disk or active swap'
        elif re.search(r'LVM2_member|linux_raid_member', output(['lsblk', '-nr', '-o', 'FSTYPE', '/dev/' + name])):
            blocked = 'Existing LVM/RAID: prepare storage separately'
        disks.append(dict(name=name,serial=serial,model=row['MODEL'],size=int(row['SIZE']),blocked=blocked,location=location))
    for disk in disks:
        if sum(d['serial'] == disk['serial'] for d in disks) != 1:
            disk['blocked'] = 'Duplicate disk serial'
    return dict(version=2,mac=mac,firmware='uefi' if os.path.isdir('/sys/firmware/efi') else 'bios',disks=disks)

def single_internal_disk(inv):
    disks = [d for d in inv['disks'] if d.get('location') != 'external']
    if len(disks) > 1:
        raise RuntimeError('Multiple internal or unknown disks. Assign the target disk in the management page before retrying.')
    if len(disks) != 1 or disks[0].get('location') != 'internal':
        raise RuntimeError('No verified internal disk available for automatic installation')
    disk = disks[0]
    if disk['blocked']:
        raise RuntimeError(disk['blocked'])
    if (not re.match(r'^[A-Za-z0-9_.:/-]{1,120}$', disk['serial']) or
            sum(d['serial'] == disk['serial'] for d in inv['disks']) != 1):
        raise RuntimeError('Target disk serial must be present and unique')
    return disk['serial']

def automatic_disk(inv, policy):
    if policy == 'console':
        return ''
    if policy == 'single-or-console':
        if len([d for d in inv['disks'] if d.get('location') != 'external']) > 1:
            return ''
        if any(d.get('location') == 'internal' and
               d['blocked'] == 'Existing LVM/RAID: prepare storage separately' for d in inv['disks']):
            return ''
    elif policy != 'single-internal':
        raise RuntimeError('Unknown PXE disk policy')
    return single_internal_disk(inv)

def api(work, endpoint, body):
    if SETTINGS['installer'] in ('ubiquity', 'autoinstall'):
        # Both Desktop installers can omit curl from the live system. Ubuntu
        # already provides Python 3; keep discovery independent of extra packages.
        import ssl
        import urllib.request
        import urllib.error
        request = urllib.request.Request(SETTINGS['url'] + '/' + endpoint,
            data=json.dumps(body).encode('utf-8'), headers={
                'Authorization': 'Bearer ' + SETTINGS['token'], 'Content-Type': 'application/json'})
        try:
            with urllib.request.urlopen(request, context=ssl.create_default_context(cafile=work + '/ca.crt' if SETTINGS['ca'] else None), timeout=45) as response:
                return json.load(response)
        except urllib.error.HTTPError as error:
            try:
                result = json.load(error)
            except ValueError:
                result = {}
            raise RuntimeError(result.get('error', 'Server rejected request: ' + str(error.code)))
    write(work + '/request.json', json.dumps(body))
    # Keep bearer credentials out of the process command line and console logs.
    write(work + '/curl.conf', 'header = "Authorization: Bearer ' + SETTINGS['token'] + '"\n')
    trust = ['--cacert', work + '/ca.crt'] if SETTINGS['ca'] else []
    status = output(['curl', '--config', work + '/curl.conf'] + trust + [
                     '--silent', '--show-error', '--connect-timeout', '10', '--max-time', '45',
                     '--proto', '=https', '--output', work + '/response.json', '--write-out', '%{http_code}',
                     '-H', 'Content-Type: application/json', '--data-binary', '@' + work + '/request.json',
                     SETTINGS['url'] + '/' + endpoint])
    with io.open(work + '/response.json', encoding='utf-8') as f:
        result = json.load(f)
    if status != '200':
        raise RuntimeError(result.get('error', 'Server rejected request: ' + status))
    return result

def confirm_erase(console, message):
    print(message, file=console)
    for attempt in range(1, 4):
        print('Type yes to erase and install, or no / q to cancel. Attempt %d/3:' % attempt, file=console)
        console.flush()
        value = prompt(console).lower()
        if value in ('no', 'q'):
            raise RuntimeError('Installation canceled at the console')
        if value == 'yes':
            return
        print('Please type yes, no or q. Empty Enter does not confirm. Nothing has been erased.', file=console)
    raise RuntimeError('Erase confirmation did not match after 3 attempts; stopped')

def choose_filesystem(console, default='ext4'):
    if default not in ('ext4', 'xfs'):
        raise RuntimeError('Invalid default filesystem')
    print('Filesystem for /, /tmp, /var and /home:', file=console)
    print('  1. ext4\n  2. XFS\n/boot stays ext4; EFI stays FAT32.', file=console)
    for attempt in range(1, 4):
        print('Select 1/ext4 or 2/xfs; Enter keeps %s; q cancels. Attempt %d/3:' % (default, attempt), file=console)
        console.flush()
        value = prompt(console).lower()
        if value == 'q':
            raise RuntimeError('Installation canceled at the console')
        if value == '':
            return default
        if value in ('1', 'ext4'):
            return 'ext4'
        if value in ('2', 'xfs'):
            return 'xfs'
        print('Invalid filesystem. No disk has been erased.', file=console)
    raise RuntimeError('No valid filesystem selected after 3 attempts')

def choose_raid(inv, console):
    allowed = ('', 'Existing LVM/RAID: prepare storage separately')
    for attempt in range(1, 4):
        print('RAID1 + LVM: enter TWO disk numbers separated by a space (example: 1 2), or q:', file=console)
        console.flush()
        value = prompt(console)
        if value.lower() == 'q':
            raise RuntimeError('Installation canceled at the console')
        numbers = value.split()
        if (len(numbers) != 2 or not all(n.isdigit() and 1 <= int(n) <= len(inv['disks']) for n in numbers)
                or int(numbers[0]) == int(numbers[1])):
            print('Choose two different disk numbers. Nothing has been erased.', file=console)
            continue
        members = [inv['disks'][int(n)-1] for n in numbers]
        if any(d.get('location') != 'internal' or d['blocked'] not in allowed or d['size'] < 120000000000
               or not re.match(r'^[A-Za-z0-9_.:/-]{1,120}$', d['serial'])
               or sum(x['serial'] == d['serial'] for x in inv['disks']) != 1 for d in members):
            print('A selected member is unavailable, mounted, ambiguous, or smaller than 120 GB.', file=console)
            continue
        serials = [d['serial'] for d in members]
        for d in members:
            print('Selected RAID member: /dev/%s  %.1f GiB  SERIAL=%s' % (d['name'], d['size']/float(1024**3), d['serial']), file=console)
        confirm_erase(console, 'ALL DATA ON BOTH SELECTED DISKS WILL BE ERASED.\nRebuild: RAID1 mirror + LVM. Usable capacity is approximately the smaller disk.\nStorage spanning any unselected disk will stop this installation.')
        return serials[0], 'ERASE RAID1 ' + ' '.join(serials), dict(mode='raid1-lvm', disk_serials=serials)
    raise RuntimeError('No valid RAID member pair selected after 3 attempts')

def choose(inv, info, console):
    print('\nVentoy Center - PXE automatic installation', file=console)
    print('System: ' + SETTINGS['iso'], file=console)
    print('Boot MAC: ' + inv['mac'], file=console)
    print('Kernel will be locked; automatic package updates will be disabled.\n', file=console)
    for index, disk in enumerate(inv['disks'], 1):
        status = disk['blocked']
        if info.get('disk_policy') == 'single-or-console' and status == 'Existing LVM/RAID: prepare storage separately':
            status = 'Existing LVM/RAID: select to rebuild after membership checks'
        print('%d. /dev/%s  %.1f GiB  %s  SERIAL=%s  %s' % (
            index,disk['name'],disk['size']/float(1024**3),disk['model'],disk['serial'],status),file=console)
    disk = None
    raid = SETTINGS['installer'] in ('autoinstall', 'kickstart', 'preseed') and info.get('disk_policy') == 'single-or-console'
    for attempt in range(1, 4):
        print('\nSelect a disk number for single-disk installation%s, or q to stop. Attempt %d/3:' %
              (', r for TWO-DISK RAID1 + LVM' if raid else '', attempt), file=console)
        console.flush()
        value = prompt(console)
        if value.lower() == 'q':
            raise RuntimeError('Installation canceled at the console')
        if value.lower() == 'r' and raid:
            return choose_raid(inv, console)
        if not value.isdigit() or not 1 <= int(value) <= len(inv['disks']):
            print('Invalid disk number. Nothing has been erased.', file=console)
            continue
        candidate = inv['disks'][int(value)-1]
        rebuild = (info.get('disk_policy') == 'single-or-console' and
                   candidate['blocked'] == 'Existing LVM/RAID: prepare storage separately')
        if (candidate['blocked'] and not rebuild) or candidate.get('location') != 'internal':
            print('This disk cannot be selected: ' + (candidate['blocked'] or 'Unverified internal connection'), file=console)
            continue
        disk = candidate
        break
    if disk is None:
        raise RuntimeError('No usable disk selected after 3 attempts')
    print('\nSelected: /dev/%s  %.1f GiB  %s  SERIAL=%s' % (
        disk['name'], disk['size']/float(1024**3), disk['model'], disk['serial']), file=console)
    print('ALL DATA ON THIS DISK WILL BE ERASED. Other disks will not be selected for installation.', file=console)
    if rebuild:
        print('Old LVM/RAID on this disk will be removed. Membership will be checked before writing.\nIf it also uses another disk, installation stops and lists that disk; select all members for a rebuild.', file=console)
    for attempt in range(1, 4):
        print('Type yes to erase and install, or no / q to cancel. Attempt %d/3:' % attempt, file=console)
        console.flush()
        confirmation = prompt(console)
        if confirmation.lower() in ('no', 'q'):
            raise RuntimeError('Installation canceled at the console')
        if confirmation.lower() == 'yes':
            # The server still binds confirmation to the exact inventoried disk.
            if rebuild:
                return disk['serial'], 'ERASE REBUILD ' + disk['serial'], dict(mode='single-rebuild', disk_serials=[disk['serial']])
            return disk['serial'], 'ERASE ' + disk['serial'], None
        print('Please type yes, no or q. Empty Enter does not confirm. Nothing has been erased.', file=console)
    raise RuntimeError('Erase confirmation did not match after 3 attempts; stopped')

def prompt(console):
    if not select.select([console], [], [], 600)[0]:
        raise RuntimeError('Console confirmation timed out')
    value = console.readline(256)
    if not value or len(value) >= 255:
        raise RuntimeError('Invalid console input')
    return value.strip()

def selected_config(installer, source):
    if installer != 'autoinstall':
        return source
    import yaml
    document = yaml.safe_load(source)
    # The API returns cloud-init user-data. Subiquity 23.08 (22.04.3)
    # re-reads /autoinstall.yaml as a bare mapping after early-commands;
    # leaving the cloud-init wrapper silently selects installer defaults,
    # including an unbound disk. Unwrap BEFORE the guard rewrites and claims it.
    if not isinstance(document, dict) or set(document) != {'autoinstall'}:
        raise RuntimeError('Expected cloud-config autoinstall response')
    config = document['autoinstall']
    if not isinstance(config, dict) or config.get('version') != 1:
        raise RuntimeError('Unsupported autoinstall response')
    identity = config.get('identity')
    userdata = config.get('user-data')
    users = userdata.get('users') if isinstance(userdata, dict) else None
    has_identity = (isinstance(identity, dict) and
                    all(identity.get(k) for k in ('hostname', 'username', 'password')))
    has_admin = (isinstance(users, list) and len(users) == 1 and
                 isinstance(users[0], dict) and users[0].get('name') == 'admin' and
                 users[0].get('passwd') and users[0].get('lock_passwd') is False and
                 'sudo' in users[0].get('groups', []) and userdata.get('hostname'))
    if not isinstance(config.get('storage'), dict) or not (has_identity or has_admin):
        raise RuntimeError('Incomplete autoinstall response; refusing installer defaults')
    return yaml.safe_dump(config, default_flow_style=False)

def main():
    os.umask(0o077)
    work = tempfile.mkdtemp(prefix='ventoy-pxe-',dir='/tmp')
    write(work + '/ca.crt',SETTINGS['ca'])
    mac = boot_mac()
    inv = inventory(mac)
    info = api(work,'discover',inv)
    policy = SETTINGS['disk_policy']
    if info.get('disk_policy') != policy:
        raise RuntimeError('PXE disk policy changed. Refresh the menu and boot again.')
    serial = info['preset_disk']
    confirmation = ''
    storage_plan = None
    console = None
    if policy not in ('single-internal', 'single-or-console', 'console'):
        raise RuntimeError('Unknown PXE disk policy')
    if not serial:
        serial = automatic_disk(inv, policy)
        if info.get('automatic_disk', '') != serial:
            raise RuntimeError('Controller and client selected different disks; stopped')
    if info['preset_disk']:
        # A ready management task has already authorized its filesystem. Never
        # silently change its layout through a shared menu entry.
        filesystem = info.get('preset_filesystem') or 'ext4'
        if filesystem not in ('ext4', 'xfs'):
            raise RuntimeError('Invalid authorized filesystem')
        print('Ventoy Center: authorized task filesystem = ' + filesystem)
    else:
        reserve_console()
        console = Console()
        subprocess.check_call(['chvt', CONSOLE_VT])
        print('\nVentoy Center - ' + SETTINGS['iso'], file=console)
        filesystem = choose_filesystem(console, info.get('filesystem', 'ext4'))
        print('Selected filesystem: ' + filesystem, file=console)
        console.flush()
    if serial and info.get('automatic_disk'):
        print('Ventoy Center: installing %s on the only internal disk, SERIAL=%s' % (SETTINGS['iso'], serial))
    elif not serial and policy in ('console', 'single-or-console'):
        # Keep installer UI and keyboard input separate. No dependency on the
        # caller having a controlling terminal (Subiquity runs as a service).
        serial, confirmation, storage_plan = choose(inv,info,console)
    if inventory(mac) != inv:
        raise RuntimeError('Hardware or mount state changed before installation')
    body = dict(ticket=info['ticket'], disk_serial=serial, confirmation=confirmation, filesystem=filesystem)
    if storage_plan:
        body['storage_plan'] = storage_plan
    result = api(work, 'install', body)
    installer = SETTINGS['installer']
    if result['installer'] != installer:
        raise RuntimeError('Installer type mismatch')
    if installer == 'autoinstall':
        failure = result['install_failure']
        compile(failure, 'installation-failure-handler', 'exec')
        # The original entry's error controller is already loaded. Save its
        # job-specific callback before running any selected-config guard.
        write('/run/ventoy-center-install-failed.py', failure)
    destination = '/autoinstall.yaml' if installer == 'autoinstall' else '/tmp/ventoy-center-selected.ks'
    if installer == 'ubiquity':
        destination = '/tmp/ventoy-center-selected.cfg'
    write(destination,selected_config(installer,result['config']))
    write(work + '/preinstall.sh',result['preinstall'])
    if inventory(mac) != inv:
        raise RuntimeError('Hardware or mount state changed while obtaining installation config')
    # Guard rechecks OS, MAC, serial, storage and firmware, then atomically claims
    # the job. The real installer cannot run before this returns successfully.
    subprocess.check_call(['/bin/bash',work + '/preinstall.sh'])
    if installer == 'ubiquity':
        subprocess.check_call(['debconf-set-selections', destination])
    if console:
        print('Checks passed. Automatic installation is starting.',file=console);console.flush()
        console.close()
        subprocess.call(['chvt','1'])
    # Do not leave a replayable per-machine response in the shared entry.
    for name in ('response.json', 'curl.conf'):
        if os.path.exists(work + '/' + name):
            os.unlink(work + '/' + name)

if __name__ == '__main__':
    try:
        main()
    except Exception as exc:
        message = 'Ventoy Center stopped: ' + str(exc)
        print(message,file=sys.stderr)
        try:
            with open(CONSOLE_PATH,'w') as tty:
                print('\n' + message + '\nInstallation stopped. No new installation will start. Correct the reported condition, then boot PXE again.',file=tty)
            subprocess.call(['chvt', CONSOLE_VT])
        except Exception:
            pass
        sys.exit(1)
