#!/usr/bin/env python
# -*- coding: utf-8 -*-
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
import sys
import tempfile
import time
try:
    from urllib.parse import urlsplit
    import http.client as httplib
except ImportError:
    from urlparse import urlsplit
    import httplib

PROGRAMS = ('sckocstp', 'fyhocsys')
STRING = (str, type(u''))
TIMEOUT = 150
REJOIN_WAIT = 330
RETRY_INTERVAL = 15
CONFIG_ROOT = '/etc'
KEY_ROOT = '/run'
MANUAL_STATE = '/var/lib/ventoy-center/manual-init'
DEFAULT_HOSTNAMES = ('localhost', 'localhost.localdomain', 'ubuntu', 'ubuntu-server',
                     'ubuntu-desktop', 'debian', 'centos', 'rocky', 'rockylinux',
                     'almalinux', 'rhel', 'linux', 'anolis', 'ol', 'scientific')


class BindingRejected(RuntimeError):
    """Only a pinned center's HTTP 401 proves that a saved token is obsolete."""


class JoinCooldown(RuntimeError):
    """The official same-machine rebind is waiting for the old heartbeat."""


def failure_hint(data):
    # Do not forward server/CLI text: it may contain private credentials.
    if u'5 \u5206\u949f\u6ca1\u6709\u8054\u7cfb' in data or u'\u8fd8\u8fde\u7740\u4e2d\u5fc3' in data:
        raise JoinCooldown('The previous node connection was seen recently')
    if u'DMI UUID \u4e0d\u540c' in data:
        return 'this hostname belongs to different hardware; choose a unique hostname or review the original card in the node center'
    if u'\u4e2d\u5fc3\u6ca1\u6709\u8bb0\u4e0b' in data or u'\u8bfb\u4e0d\u5230 DMI UUID' in data:
        return 'the center cannot prove this is the original hardware; use its Rebind command once, then rerun setup'
    if u'\u5bc6\u94a5\u5df2\u4f5c\u5e9f' in data or u'\u5bc6\u94a5\u5df2\u8fc7\u671f' in data or 'join key is not valid' in data:
        return 'the central enrollment key is invalid, expired or revoked; update the private node-join configuration on the Ventoy center'
    if u'\u53f0\u5df2\u7ecf\u7528\u5b8c' in data:
        return 'the central enrollment key has reached its new-node limit'
    if 'no usable DMI serial' in data or u'\u8bfb\u4e0d\u5230\u8fd9\u53f0\u673a\u5668\u7684\u5e8f\u5217\u53f7' in data:
        return 'no usable DMI serial; join with an explicit --serial asset number, then rerun setup'
    if 'existing node configuration retained' in data or 'hostname/serial differs' in data:
        return 'existing binding or hostname differs; use the registered hostname or review the binding in the node center'
    if 'older than 0.8.6' in data or 'flag provided but not defined' in data:
        return 'upgrade both the node and center to 0.8.6 or newer'
    if 'certificate' in data.lower():
        return 'verify the configured center address and certificate fingerprint'
    return 'check the center, enrollment key and installed node version; existing configuration was retained'


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
                raise RuntimeError(description + ' failed; ' + failure_hint(data))
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


def connection(node, path=None):
    path = path or connection_path(node)
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
                not re.match(r'^[0-9a-f]{64}\Z', config.get('token', '')) or
                not re.match(r'^[A-Za-z0-9_.-]{1,96}\Z', config.get('serial', '')) or
                config['ca_pem'].count('-----BEGIN CERTIFICATE-----') != 1 or
                hashlib.sha256(certificate).hexdigest() != node['pin']):
            raise ValueError()
    except (ValueError, TypeError, KeyError, AttributeError):
        raise RuntimeError(node['program'] + ': saved connection does not match this host and pinned center')
    return config


def heartbeat(node, config):
    """Verify authentication without polling, claiming or starting any work.

    Like the official join protocol, trust only the exact leaf fingerprint.
    Connect and check it BEFORE sending credentials; do not follow redirects.
    """
    parsed = urlsplit(node['url'])
    version = command([trusted_binary(node['program']), 'version'], node['program'] + ': version check')
    match = re.match(r'^' + node['program'] + r' (\d+\.\d+\.\d+)\Z', version)
    if not match:
        raise RuntimeError(node['program'] + ': cannot verify the installed node version')
    context = ssl.SSLContext(ssl.PROTOCOL_TLSv1_2)
    context.verify_mode = ssl.CERT_NONE
    client = httplib.HTTPSConnection(parsed.hostname, parsed.port or 443,
                                    timeout=15, context=context)
    try:
        client.connect()
        if hashlib.sha256(client.sock.getpeercert(binary_form=True)).hexdigest() != node['pin']:
            raise RuntimeError('certificate fingerprint mismatch')
        prefix = 'X-Fyhocsys-' if node['program'] == 'fyhocsys' else 'X-Sckocstp-'
        headers = {'Authorization': 'Bearer ' + config['token'], prefix + 'Node': config['node'],
                   prefix + 'Request': '1', prefix + 'Version': match.group(1)}
        # Do not impersonate a node version or overwrite its hardware identity.
        client.request('GET', '/node/v1/heartbeat', headers=headers)
        response = client.getresponse()
        if response.status == 401:
            raise BindingRejected(node['program'] + ': the center rejected the saved node credential')
        data = response.read(4097)
        if response.status != 200 or len(data) > 4096:
            raise RuntimeError('unexpected center response')
        reply = json.loads(data.decode('utf-8'))
        if not isinstance(reply, dict) or not isinstance(reply.get('time'), STRING) or not reply['time']:
            raise RuntimeError('invalid heartbeat response')
    except BindingRejected:
        raise
    except Exception:
        raise RuntimeError(node['program'] + ': cannot verify the pinned center online; check network, clock and certificate; saved connection retained')
    finally:
        client.close()


def enroll(node, binary, hostname, previous):
    path = connection_path(node)
    parent = os.path.dirname(path)
    if os.path.realpath(parent) != parent:
        raise RuntimeError(node['program'] + ': unsafe node configuration directory')
    if not os.path.isdir(parent):
        os.makedirs(parent, 0o700)
    info = os.stat(parent)
    if info.st_uid != 0 or info.st_mode & 0o077:
        raise RuntimeError(node['program'] + ': node configuration directory must be private and root-owned')
    # Keep the candidate on the same filesystem for an atomic commit. The old
    # file remains intact while the official command validates DMI + cooldown.
    folder = tempfile.mkdtemp(prefix='.ventoy-join-', dir=parent)
    keys = None
    try:
        keys = tempfile.mkdtemp(prefix='ventoy-node-join-', dir=KEY_ROOT)
        keyfile = os.path.join(keys, 'join.key')
        fd = os.open(keyfile, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, 'wb') as handle:
            handle.write(node['key'].encode('ascii') + b'\n')
        candidate = os.path.join(folder, 'connection.json')
        args = [binary, 'join', '--url', node['url'], '--pin', node['pin'],
                '--key-file', keyfile, '--config', candidate, '--start=false']
        deadline = time.time() + REJOIN_WAIT
        while True:
            if ready_hostname() != hostname:
                raise RuntimeError('Hostname changed during node enrollment; no local binding was replaced')
            try:
                command(args, node['program'] + ': node enrollment')
                break
            except JoinCooldown:
                if time.time() >= deadline:
                    raise RuntimeError(node['program'] + ': the old node is still contacting the center; stop that old instance, then rerun setup; no binding was replaced')
                print('NODE_JOIN_WAIT: %s; old connection seen recently; retrying in %s seconds (at most %s seconds remaining).' %
                      (node['program'], RETRY_INTERVAL, max(0, int(deadline - time.time()))))
                sys.stdout.flush()
                time.sleep(min(RETRY_INTERVAL, max(0, deadline - time.time())))
        connection(node, candidate)
        if previous is not None:
            # Only an explicit HTTP 401 permits replacing a saved binding.
            # A live, accepted service never passes through this branch.
            if connection(node) != previous:
                raise RuntimeError(node['program'] + ': connection changed during enrollment; current file retained')
            fd, backup = tempfile.mkstemp(prefix='connection.before-setup-', suffix='.json', dir=parent)
            with os.fdopen(fd, 'wb') as handle, open(path, 'rb') as old:
                handle.write(old.read()); handle.flush(); os.fsync(handle.fileno())
            print('NODE_JOIN_BACKUP: ' + backup)
            command(['systemctl', 'stop', node['program'] + '-node.service'], node['program'] + ': stop obsolete connection')
        elif os.path.lexists(path):
            raise RuntimeError(node['program'] + ': connection appeared during enrollment; existing file retained')
        os.rename(candidate, path)
        print('NODE_JOIN_REBOUND: ' + node['program'] if previous is not None else 'NODE_JOIN_ENROLLED: ' + node['program'])
    finally:
        if keys is not None:
            shutil.rmtree(keys)
        shutil.rmtree(folder)


def check(nodes):
    if nodes:
        ready_hostname()
    for node in nodes:
        validate_node(node, secret=False)
        program = node['program']
        config = connection(node)
        command([trusted_binary(program), 'check'], program + ': node configuration/worker check')
        service = program + '-node.service'
        for action, expected in (('is-enabled', 'enabled'), ('is-active', 'active')):
            if command(['systemctl', action, service], program + ': service check') != expected:
                raise RuntimeError(program + ': node service is not enabled and active')
        heartbeat(node, config)
        print('NODE_JOIN_VERIFIED: %s; center accepted the credential; local binding, certificate, worker and boot service checked.' % program)


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
        stale = False
        if os.path.lexists(connection_path(node)):
            config = connection(node)
            try:
                heartbeat(node, config)
            except BindingRejected:
                stale = True
        else:
            config = None
        if stale or config is None:
            enroll(node, binaries[program], hostname, config)
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
