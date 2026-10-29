#!/usr/bin/env python
"""Enroll installed 0.8.6+ nodes using the projects' official join commands.

Python 2.7 / 3.x. Keys exist only in a private temporary file during enrollment;
argv, environment, logs and software reports never contain their values.
"""
from __future__ import print_function
import hashlib
import json
import os
import re
import resource
import shutil
import signal
import socket
import ssl
import stat
import subprocess
import tempfile
import time
try:
    from urllib.parse import urlsplit
except ImportError:
    from urlparse import urlsplit

PROGRAMS = ('sckocstp', 'fyhocsys')
STRING = (str, type(u''))
TIMEOUT = 150
CONFIG_ROOT = '/etc'
KEY_ROOT = '/run'
MANUAL_STATE = '/var/lib/ventoy-center/manual-init'
DEFAULT_HOSTNAMES = ('localhost', 'localhost.localdomain', 'ubuntu', 'ubuntu-server',
                     'ubuntu-desktop', 'debian', 'centos', 'rocky', 'rockylinux',
                     'almalinux', 'rhel', 'linux', 'anolis', 'ol', 'scientific')


def validate_node(node, secret=True):
    if not isinstance(node, dict) or node.get('program') not in PROGRAMS:
        raise RuntimeError('Invalid node join configuration')
    fields = ('url', 'pin', 'key') if secret else ('url', 'pin')
    if any(not isinstance(node.get(k), STRING) for k in fields):
        raise RuntimeError('Invalid node join configuration')
    url = node['url']
    parsed = urlsplit(url)
    if (len(url) > 2048 or parsed.scheme != 'https' or not parsed.hostname or
            parsed.username is not None or parsed.password is not None or
            parsed.path or '?' in url or '#' in url or re.search(r'\s', url) or
            not re.match(r'^[0-9a-f]{64}\Z', node['pin'])):
        raise RuntimeError('Invalid node join address or certificate fingerprint')
    if secret and not re.match(r'^[A-HJ-NP-Z2-9]{32}\Z', node['key']):
        raise RuntimeError('Invalid node join key')


def fetch(client):
    resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
    response = client.get('node-join', {})
    try:
        data = response.read(16385)
    finally:
        response.close()
    try:
        if len(data) > 16384:
            raise ValueError()
        config = json.loads(data.decode('utf-8'))
        if config.get('enabled') is False:
            return []
        nodes = config.get('nodes')
        if (config.get('enabled') is not True or config.get('schema') != 1 or
                not isinstance(nodes, list) or not 1 <= len(nodes) <= 2):
            raise ValueError()
        for node in nodes:
            validate_node(node)
        if len(set(n['program'] for n in nodes)) != len(nodes):
            raise ValueError()
        return nodes
    except (ValueError, TypeError, AttributeError, RuntimeError):
        raise RuntimeError('Invalid private node join response')


def trusted_binary(program):
    if program not in PROGRAMS:
        raise RuntimeError('Unsupported node program')
    for directory in ('/usr/bin', '/usr/local/bin', '/usr/sbin', '/usr/local/sbin'):
        name = os.path.join(directory, program + '-node')
        if not os.path.exists(name):
            continue
        real = os.path.realpath(name)
        info = os.stat(real)
        if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o022 or not os.access(real, os.X_OK):
            raise RuntimeError(program + ': node executable is not root-owned and protected')
        directory = os.path.dirname(real)
        while directory != '/':
            info = os.stat(directory)
            if info.st_uid != 0 or info.st_mode & 0o022:
                raise RuntimeError(program + ': node executable directory is not protected')
            directory = os.path.dirname(directory)
        return real
    raise RuntimeError(program + ': install the matching node package, version 0.8.6 or newer')


def command(args, description):
    # The subprocess may print its input or a server response on failure. Keep
    # all output private, including on timeouts, and only expose fixed hints.
    with tempfile.TemporaryFile() as output, open(os.devnull, 'rb') as null:
        process = subprocess.Popen(args, stdin=null, stdout=output, stderr=output,
                                   preexec_fn=os.setsid,
                                   env={'PATH': '/usr/sbin:/usr/bin:/sbin:/bin',
                                        'LC_ALL': 'C', 'SYSTEMD_PAGER': 'cat'})
        try:
            deadline = time.time() + TIMEOUT
            while process.poll() is None and time.time() < deadline:
                time.sleep(0.05)
            if process.poll() is None:
                raise RuntimeError(description + ' timed out; retry setup when the center is reachable')
            output.seek(0)
            data = output.read(65536).decode('utf-8', 'replace')
            if process.returncode:
                hint = 'check the center, certificate fingerprint, join key and installed node version'
                if 'no usable DMI serial' in data:
                    hint = 'no usable DMI serial; join this node with an explicit --serial asset number, then retry setup'
                elif 'existing node configuration retained' in data or 'hostname/serial differs' in data:
                    hint = 'existing binding or hostname differs; retain it and resolve the binding in the node center'
                elif 'older than 0.8.6' in data or 'flag provided but not defined' in data:
                    hint = 'upgrade both the node and center to 0.8.6 or newer'
                elif 'certificate' in data.lower():
                    hint = 'verify the configured center address and certificate fingerprint'
                raise RuntimeError(description + ' failed; ' + hint)
            return data.strip()
        finally:
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait()


def connection_path(node):
    return os.path.join(CONFIG_ROOT, node['program'], 'node', 'connection.json')


def ready_hostname():
    if os.path.lexists(os.path.join(MANUAL_STATE, 'hostname-pending')):
        raise RuntimeError('Node enrollment waits for the requested hostname; complete full setup first')
    hostname = socket.gethostname()
    with open(os.path.join(CONFIG_ROOT, 'hostname')) as handle:
        persistent = handle.read(256).strip()
    if not re.match(r'^[A-Za-z0-9][A-Za-z0-9.-]{0,95}\Z', hostname) or hostname != persistent:
        raise RuntimeError('Node enrollment requires a valid, matching runtime and persistent hostname; run full setup --hostname NAME first')
    return hostname


def connection(node):
    path = connection_path(node)
    if os.path.realpath(path) != path:
        raise RuntimeError(node['program'] + ': unsafe node connection file')
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, 'rb') as handle:
        info = os.fstat(handle.fileno())
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or
                stat.S_IMODE(info.st_mode) != 0o600 or info.st_nlink != 1):
            raise RuntimeError(node['program'] + ': node connection must be a root-owned 0600 regular file')
        raw = handle.read(65537)
    try:
        if len(raw) > 65536:
            raise ValueError()
        config = json.loads(raw.decode('utf-8'))
        certificate = ssl.PEM_cert_to_DER_cert(config['ca_pem'])
        if (config['url'] != node['url'] or config['node'] != socket.gethostname() or
                hashlib.sha256(certificate).hexdigest() != node['pin']):
            raise ValueError()
    except (ValueError, TypeError, KeyError, AttributeError):
        raise RuntimeError(node['program'] + ': saved connection does not match this host and pinned center')


def check(nodes):
    if nodes:
        ready_hostname()
    for node in nodes:
        validate_node(node, secret=False)
        program = node['program']
        connection(node)
        command([trusted_binary(program), 'check'], program + ': node configuration/worker check')
        service = program + '-node.service'
        for action, expected in (('is-enabled', 'enabled'), ('is-active', 'active')):
            if command(['systemctl', action, service], program + ': service check') != expected:
                raise RuntimeError(program + ': node service is not enabled and active')
        print('NODE_JOIN_VERIFIED: %s; local binding, certificate, worker and boot service checked.' % program)


def configure(nodes, installed_names):
    if not nodes:
        print('NODE_JOIN_SKIPPED: controller automatic node enrollment is not configured.')
        return []
    resource.setrlimit(resource.RLIMIT_CORE, (0, 0))
    hostname = ready_hostname()
    binaries = {}
    # Check every selected program before spending a key for any of them.
    for node in nodes:
        validate_node(node)
        program = node['program']
        if hostname.lower() in DEFAULT_HOSTNAMES and not os.path.lexists(connection_path(node)):
            raise RuntimeError(program + ': choose a unique hostname in full setup before first enrollment; a default OS hostname cannot be used')
        if program + '-node' not in installed_names:
            raise RuntimeError(program + ': publish and install its matching 0.8.6+ node package before enrollment')
        binary = trusted_binary(program)
        version = command([binary, 'version'], program + ': version check')
        match = re.match(r'^' + program + r' (\d+)\.(\d+)\.(\d+)\Z', version)
        if not match or tuple(int(v) for v in match.groups()) < (0, 8, 6):
            raise RuntimeError(program + ': automatic enrollment requires node version 0.8.6 or newer')
        binaries[program] = binary
    verified = []
    for node in nodes:
        program = node['program']
        if ready_hostname() != hostname:
            raise RuntimeError('Hostname changed during setup; retry enrollment after confirming the final hostname')
        folder = tempfile.mkdtemp(prefix='ventoy-node-join-', dir=KEY_ROOT)
        try:
            keyfile = os.path.join(folder, 'join.key')
            fd = os.open(keyfile, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
            with os.fdopen(fd, 'wb') as handle:
                handle.write(node['key'].encode('ascii') + b'\n')
            command([binaries[program], 'join', '--url', node['url'], '--pin', node['pin'],
                     '--key-file', keyfile], program + ': node enrollment')
        finally:
            shutil.rmtree(folder)
        # Upstream's repeated join is a no-op. Repair a previous failure after
        # saving the connection (e.g. service enable failed), without restarting
        # an already running node or changing its binding.
        service = program + '-node.service'
        command(['systemctl', 'enable', service], program + ': enable boot service')
        command(['systemctl', 'start', service], program + ': start node service')
        record = dict((k, node[k]) for k in ('program', 'url', 'pin'))
        check([record])
        verified.append(record)
    return verified
