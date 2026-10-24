#!/usr/bin/env bash
# safe-server-init.sh v3.0.0 — audited conservative hostname / DHCPv4 setup
# Ventoy adaptation: Debian 10 ifupdown is also supported; other safeguards unchanged.
# Target: EL 7-10, Ubuntu 20-26, Debian 10-13. Bash >= 4.2.
# No package installation, network-manager migration, global apply/restart,
# deletion of existing profiles, or conversion of existing static addresses.
# NetworkManager: new autoconnect profiles MAY activate immediately.
# Other backends: persistent configuration only; explicitly apply at console.
# Existing configurations, including inactive ones, are protected.
# Sourceable: set-hostname NAME; set-network --dry-run.
# Exit: 0 success/no change, 1 failure, 2 argument error, 3 incomplete/safety skip.

_si_log()  { printf '[%s] %s\n' "$(date '+%F %T')" "$*"; }
_si_warn() { printf '[警告] %s\n' "$*" >&2; }
_si_err()  { printf '[错误] %s\n' "$*" >&2; }
_si_has()  { command -v "$1" >/dev/null 2>&1; }
_si_path() { printf '%s\n' "$1"; } # Internal indirection for isolated tests, not a CLI option.
_si_active() { systemctl is-active --quiet "$1" 2>/dev/null; }
_si_file() { [[ -e "$1" || -L "$1" ]]; }
_si_nic_valid() { [[ "$1" =~ ^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,14}$ ]]; }

_si_init() {
    export LC_ALL=C SYSTEMD_COLORS=0 SYSTEMD_PAGER=cat
    umask 077
    ((BASH_VERSINFO[0] > 4 || (BASH_VERSINFO[0] == 4 && BASH_VERSINFO[1] >= 2))) || {
        _si_err '需要 Bash >= 4.2。'; return 1;
    }
    ((EUID == 0)) || { _si_err '请以 root 或 sudo bash 运行。'; return 1; }
    local ID='' VERSION_ID='' PRETTY_NAME='' major osfile
    osfile="$(_si_path /etc/os-release)"
    [[ -r "$osfile" ]] || { _si_err '缺少可读的 /etc/os-release。'; return 1; }
    # /etc/os-release is a root-managed OS file, not a user configuration.
    # shellcheck disable=SC1090
    . "$osfile"
    major="${VERSION_ID%%.*}"
    [[ "$major" =~ ^[0-9]+$ ]] || { _si_err '不能确定发行版版本。'; return 1; }
    case "$ID" in
        ubuntu) ((major >= 20 && major <= 26)) || return 1; SI_FAMILY=ubuntu ;;
        debian) ((major >= 10 && major <= 13)) || return 1; SI_FAMILY=debian ;;
        rhel|centos|rocky|almalinux|ol|scientific|anolis)
            ((major >= 7 && major <= 10)) || return 1; SI_FAMILY=el ;;
        *) _si_err "未适配的发行版：$ID $VERSION_ID。"; return 1 ;;
    esac
    SI_VERSION="$VERSION_ID"
    SI_OS="${PRETTY_NAME:-$ID $VERSION_ID}"
    local cmd
    for cmd in systemctl flock awk sed grep mktemp readlink stat cp chmod chown mv ln cat date tail head; do
        _si_has "$cmd" || { _si_err "缺少命令：$cmd；未安装或切换任何软件。"; return 1; }
    done
}

_si_lock() {
    local file dir
    file="$(_si_path /run/lock/safe-server-init.lock)"; dir="${file%/*}"
    mkdir -p -- "$dir" || return 1
    if [[ -L "$file" ]] || { [[ -e "$file" ]] && [[ ! -f "$file" ]]; }; then
        _si_err "不安全的锁文件：$file"; return 1
    fi
    # Each public function runs in a subshell; descriptor 9 closes on return.
    exec 9>>"$file" || return 1
    flock -n 9 || { _si_err '另一个初始化进程正在运行。'; return 1; }
}

_si_backup() {
    local file root
    root="$(_si_path /var/backups/safe-server-init)"
    mkdir -p -- "$root" || return 1
    chmod 700 "$root" || return 1
    SI_BACKUP="$(mktemp -d "$root/$(date '+%Y%m%d-%H%M%S').XXXXXX")" || return 1
    for file in "$@"; do
        if _si_file "$file"; then
            cp -pL -- "$file" "$SI_BACKUP/${file##*/}" || return 1
        fi
    done
    _si_log "变更前备份：$SI_BACKUP"
}

_si_hostname_valid() {
    local name="$1" label
    [[ -n "$name" && ${#name} -le 64 && "$name" != .* && "$name" != *. && "$name" != *..* ]] || return 1
    [[ "$name" != localhost && "$name" != localhost.localdomain ]] || return 1
    [[ ! "$name" =~ ^[0-9.]+$ ]] || return 1
    local -a labels=()
    IFS=. read -r -a labels <<< "$name"
    for label in "${labels[@]}"; do
        [[ ${#label} -le 63 && "$label" =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?$ ]] || return 1
    done
}

# Edit only old hostname aliases on loopback lines. Preserve localhost and
# unrelated mappings. Add a 127.0.1.1 mapping only if the new name is absent.
_si_hosts_data() {
    local file="$1" old="$2" new="$3"
    awk -v old="$old" -v new="$new" '
        BEGIN { split(old, o, "."); split(new, n, "."); oldshort=o[1]; newshort=n[1] }
        /^[[:space:]]*#/ || NF==0 { print; next }
        {
            original=$0; comment=""; pos=index($0,"#")
            if (pos) { comment=substr($0,pos); $0=substr($0,1,pos-1) }
            changed=0
            if ($1 ~ /^127\./ || $1=="::1") {
                for (i=2;i<=NF;i++) {
                    if (old!="" && old!="localhost" && old!="localhost.localdomain" && $i==old) { $i=new; changed=1 }
                    else if (oldshort!="" && oldshort!="localhost" && $i==oldshort) { $i=newshort; changed=1 }
                }
            }
            for (i=2;i<=NF;i++) if ($i==new) found=1
            if (changed) print $0 (comment=="" ? "" : " " comment); else print original
        }
        END { if (!found) { printf "127.0.1.1\t%s",new; if(newshort!=new) printf " %s",newshort; printf "\n" } }
    ' "$file"
}

_si_hostname() {
    local new='' arg dry=0 hosts=0 old runtime hfile hostsfile data='' tmp=''
    while (($#)); do
        arg="$1"; shift
        case "$arg" in
            --dry-run) dry=1 ;;
            --update-hosts) hosts=1 ;;
            -*) _si_err "未知 hostname 参数：$arg"; return 2 ;;
            *) [[ -z "$new" ]] || { _si_err '只能指定一个主机名。'; return 2; }; new="$arg" ;;
        esac
    done
    _si_init || return 1
    _si_has hostnamectl || { _si_err '缺少 hostnamectl。'; return 1; }
    if [[ -z "$new" ]]; then
        [[ -t 0 ]] || { _si_err '非交互运行必须指定主机名。'; return 2; }
        read -r -p '请输入主机名: ' new || return 1
    fi
    new="${new,,}"
    _si_hostname_valid "$new" || { _si_err '主机名必须为合法 ASCII DNS 名称，总长 <=64、每段 <=63；不接受 IP 地址或 localhost。'; return 2; }
    ((dry)) || _si_lock || return 1
    old="$(hostnamectl --static 2>/dev/null)" || { _si_err '无法读取静态主机名；请在已启动的 systemd 系统执行。'; return 1; }
    runtime="$(cat "$(_si_path /proc/sys/kernel/hostname)")" || return 1
    hfile="$(_si_path /etc/hostname)"; hostsfile="$(_si_path /etc/hosts)"
    if ((hosts)); then
        [[ -f "$hostsfile" && -r "$hostsfile" && ! -L "$hostsfile" ]] || {
            _si_err '/etc/hosts 缺失、不可读或是符号链接；拒绝自动编辑。'; return 1;
        }
        data="$(_si_hosts_data "$hostsfile" "$old" "$new")" || return 1
    fi
    if ((dry)); then
        _si_log "[预览] hostnamectl set-hostname $new；当前静态=$old，运行中=$runtime"
        if ((hosts)); then printf '[预览] /etc/hosts 内容（仅在 --update-hosts 下修改）：\n%s\n' "$data"; fi
        return 0
    fi
    if [[ "$old" != "$new" || "$runtime" != "$new" ]] || ((hosts)); then
        _si_backup "$hfile" "$hostsfile" || { _si_err '备份失败，未修改主机名。'; return 1; }
    fi
    if [[ "$old" != "$new" || "$runtime" != "$new" ]]; then
        hostnamectl set-hostname "$new" || { _si_err 'hostnamectl 失败；请检查当前主机名及备份，未改写 hosts。'; return 1; }
    fi
    if [[ "$(hostnamectl --static 2>/dev/null)" != "$new" || "$(cat "$(_si_path /proc/sys/kernel/hostname)")" != "$new" ]]; then
        _si_err '主机名回读验证失败；可能部分生效，请检查 hostnamectl 与备份。'; return 1
    fi
    if ((hosts)); then
        tmp="$(mktemp "${hostsfile%/*}/.safe-hosts.XXXXXX")" || return 1
        if ! printf '%s\n' "$data" > "$tmp" || ! chmod --reference="$hostsfile" "$tmp" || ! chown --reference="$hostsfile" "$tmp"; then
            rm -f -- "$tmp"; _si_err 'hosts 临时文件准备失败；主机名已修改，hosts 未修改。'; return 1
        fi
        # Preserve an SELinux context where present. Failure is not hidden.
        if [[ -e "$(_si_path /sys/fs/selinux/enforce)" ]] || { _si_has selinuxenabled && selinuxenabled; }; then
            if ! chcon --reference="$hostsfile" "$tmp"; then rm -f -- "$tmp"; return 1; fi
        fi
        if ! mv -f -- "$tmp" "$hostsfile"; then
            rm -f -- "$tmp"; _si_err '替换 hosts 失败（可能是 bind mount）；主机名已改，原 hosts 未改。'; return 1
        fi
    else
        _si_warn '未编辑 /etc/hosts；需要同步本机名称解析时，加 --update-hosts。'
    fi
    if [[ -d "$(_si_path /etc/cloud)" ]]; then
        _si_warn '检测到 cloud-init 配置目录；其主机名/hosts 管理可能在以后启动时覆盖设置，本脚本不改 cloud-init。'
    fi
    _si_log "主机名已验证：$new（当前及持久化）；已打开终端的提示符可能仍显示旧名称。"
}

# Interpreter is only used for static configuration parsing, not for shell exec.
# Python 2.7 is retained for stock EL7; Python 3 is preferred on newer systems.
_si_python_select() {
    local exe
    SI_PYTHON=''
    for exe in python3 /usr/libexec/platform-python python2 python; do
        if _si_has "$exe" && "$exe" -c 'import sys; sys.exit(0 if sys.version_info >= (2,7) else 1)' >/dev/null 2>&1; then
            SI_PYTHON="$exe"; return 0
        fi
    done
    _si_err '需要 Python >=2.7 的标准库进行离线配置检查（优先 Python 3）；Netplan 还需要 PyYAML。'
    return 1
}

# Probe status: 0=claimed / true, 1=unclaimed / false, 2=uncertain/error.
# All directories and staged files are checked, not just active connections.
_si_probe() {
    "$SI_PYTHON" - "$(_si_path /)" "$@" <<'PY'
from __future__ import print_function
import os, sys, glob, fnmatch, re, shlex
ROOT = sys.argv[1].rstrip('/')
MODE = sys.argv[2]
ARGS = sys.argv[3:]

def path(p): return ROOT + p

def read(p):
    with open(p, 'r') as f: return f.read()

def files(dirs, pattern):
    result = []
    for d in dirs:
        dd = path(d)
        if os.path.isdir(dd) and not os.access(dd, os.R_OK | os.X_OK):
            raise ValueError('unreadable directory: ' + dd)
        result.extend(sorted(glob.glob(dd + '/' + pattern)))
    return result

def words(s): return shlex.split(s, comments=False)

def path_match(value, pattern):
    # Shell globs do not let * cross a path separator (fnmatch alone does).
    v = os.path.normpath(value).split(os.sep)
    p = os.path.normpath(pattern).split(os.sep)
    return len(v)==len(p) and all(fnmatch.fnmatchcase(x,y) for x,y in zip(v,p))

def match(value, pats):
    neg = [p[1:] for p in pats if p.startswith('!')]
    pos = [p for p in pats if not p.startswith('!')]
    if any(fnmatch.fnmatchcase(value, p) for p in neg): return False
    return not pos or any(fnmatch.fnmatchcase(value, p) for p in pos)

def ini(p):
    out, section = {}, ''
    text = read(p)
    if '\\\n' in text: raise ValueError('continued INI line requires manual review: ' + p)
    for raw in text.splitlines():
        s = raw.strip()
        if not s or s.startswith(('#', ';')): continue
        if s.startswith('[') and s.endswith(']'):
            section = s[1:-1]; out.setdefault(section, {}); continue
        if '=' not in s: raise ValueError('invalid INI line: ' + p)
        k, v = [x.strip() for x in s.split('=', 1)]
        out.setdefault(section, {}).setdefault(k, []).append(v)
    return out

def last(table, key): return (table.get(key) or [''])[-1]

def netplan_docs():
    import yaml
    class StrictLoader(yaml.SafeLoader): pass
    def mapping(loader, node, deep=False):
        result = {}
        for kn, vn in node.value:
            key = loader.construct_object(kn, deep=deep)
            if key in result: raise ValueError('duplicate YAML key: ' + str(key))
            result[key] = loader.construct_object(vn, deep=deep)
        return result
    StrictLoader.add_constructor(yaml.resolver.BaseResolver.DEFAULT_MAPPING_TAG, mapping)
    docs = []
    # Superseded files are ALSO scanned for claims: conservative false positives
    # are safer than accidentally activating a shadowed configuration later.
    for p in files(['/lib/netplan', '/usr/lib/netplan', '/etc/netplan', '/run/netplan'], '*.yaml'):
        obj = yaml.load(read(p), Loader=StrictLoader)
        if obj is None: continue
        if not isinstance(obj, dict) or not isinstance(obj.get('network'), dict):
            raise ValueError('invalid netplan root: ' + p)
        docs.append((p, obj['network']))
    return docs

def netplan_claim(nic, mac, driver):
    uncertain = False
    known_sections = ('ethernets','wifis','bonds','bridges','vlans','tunnels','modems',
                      'vrfs','dummy-devices','virtual-ethernets')
    for p, net in netplan_docs():
        for section, table in net.items():
            if section in ('version','renderer'): continue
            if section not in known_sections:
                if table: uncertain = True
                continue
            if table is None: continue
            if not isinstance(table, dict): raise ValueError('invalid netplan section: ' + p)
            for key, cfg in table.items():
                if not isinstance(cfg, dict): raise ValueError('invalid netplan interface: ' + p)
                if str(key) == nic or cfg.get('set-name') == nic or cfg.get('link') == nic: return 0
                refs = cfg.get('interfaces', []) or []
                if not isinstance(refs, list): raise ValueError('invalid interface references: ' + p)
                if nic in refs: return 0
                if 'match' not in cfg: continue
                m = cfg['match']
                if not isinstance(m, dict): raise ValueError('invalid netplan match: ' + p)
                possible, unknown = True, False
                for key, vals in m.items():
                    if key not in ('name','macaddress','driver'):
                        unknown = True; continue
                    val = {'name':nic,'macaddress':mac.lower(),'driver':driver}[key]
                    if not val: unknown = True; continue
                    vals = vals if isinstance(vals, list) else [vals]
                    vals = [str(x).lower() if key=='macaddress' else str(x) for x in vals]
                    if not any(fnmatch.fnmatchcase(val, x) for x in vals): possible = False
                if possible:
                    if unknown: uncertain = True
                    else: return 0
    return 2 if uncertain else 1

def netplan_renderer():
    # Evaluate global renderer in effective basename/lexical order; interface
    # claims still conservatively examine all source files separately.
    effective = {}
    for p, net in netplan_docs(): effective[os.path.basename(p)] = net
    renderer = ''
    for name in sorted(effective):
        if 'renderer' in effective[name]: renderer = effective[name]['renderer']
    if renderer not in ('','networkd','NetworkManager'): raise ValueError('unknown renderer')
    print(renderer)
    return 0

def nm_disk(nic, mac):
    for p in files(['/etc/NetworkManager/system-connections', '/run/NetworkManager/system-connections',
                    '/usr/lib/NetworkManager/system-connections'], '*'):
        if not os.path.isfile(p): continue
        b = os.path.basename(p)
        if b.startswith('.') or b.endswith(('~','.bak','.swp','.rpmnew','.rpmorig','.rpmsave')): continue
        c = ini(p); con=c.get('connection',{})
        typ=last(con,'type'); name=last(con,'interface-name')
        if name == nic: return 0
        if typ in ('ethernet','802-3-ethernet'):
            eth=c.get('ethernet',c.get('802-3-ethernet',{})); hw=last(eth,'mac-address')
            if hw.lower()==mac.lower(): return 0
            if not name and not hw: return 0
        elif not typ: return 2
    return 1

def ifcfg(nic, mac):
    for p in files(['/etc/sysconfig/network-scripts'], 'ifcfg-*'):
        b=os.path.basename(p)
        if b.endswith(('~','.bak','.rpmsave','.rpmnew','.orig')): continue
        if b=='ifcfg-'+nic or b.startswith('ifcfg-'+nic+':'): return 0
        vals={}
        for line in read(p).splitlines():
            line=line.strip()
            if not line or line.startswith('#'): continue
            m=re.match(r'^(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$',line)
            if not m: raise ValueError('nonliteral ifcfg requires review: '+p)
            value=m.group(2)
            if '$' in value or '`' in value: raise ValueError('dynamic ifcfg requires review: '+p)
            value=shlex.split(value,comments=True)
            if len(value)>1: raise ValueError('nonliteral ifcfg value: '+p)
            vals[m.group(1)]=value[0] if value else ''
        if vals.get('DEVICE')==nic or vals.get('NAME')==nic: return 0
        if any(vals.get(k,'').lower()==mac.lower() for k in ('HWADDR','MACADDR')): return 0
        if vals.get('TYPE','').lower() in ('ethernet','') and not vals.get('DEVICE') and not vals.get('HWADDR'):
            return 2
    return 1

def networkd(nic, mac, driver):
    permanent=''
    # Eligibility rejects reassigned addresses. Require positive sysfs evidence
    # here too, since the probe can be called independently of eligibility.
    assign=path('/sys/class/net/'+nic+'/addr_assign_type')
    address=path('/sys/class/net/'+nic+'/address')
    if os.path.isfile(assign) and os.path.isfile(address):
        if read(assign).strip()=='0' and read(address).strip().lower()==mac.lower():
            permanent=mac.lower()
    dirs=['/usr/lib/systemd/network','/usr/local/lib/systemd/network','/lib/systemd/network','/run/systemd/network','/etc/systemd/network']
    if any(files(dirs,pat) for pat in ('*.network.d/*.conf','network.d/*.conf')):
        return 2  # Drop-ins may affect even a new file that does not yet exist.
    effective={}
    for p in files(dirs,'*.network'): effective[os.path.basename(p)]=p
    uncertain=False
    for name in sorted(effective):
        p=effective[name]
        if not read(p).strip(): continue  # includes /dev/null masks
        c=ini(p); m=c.get('Match',{})
        # Matching drop-ins can alter Name/MAC/Driver, so do not guess their effects.
        if any(files(dirs,pat) for pat in ('*.network.d/*.conf','network.d/*.conf')):
            uncertain=True; continue
        possible, unknown=True,False
        for key, entries in m.items():
            pats=[]
            for entry in entries:
                if not entry: pats=[]
                else: pats.extend(words(entry))
            if not pats: continue
            if key not in ('Name','MACAddress','PermanentMACAddress','Driver','Type'):
                unknown=True; continue
            val={'Name':nic,'MACAddress':mac.lower(),'PermanentMACAddress':permanent,'Driver':driver,'Type':'ether'}[key]
            if not val: unknown=True; continue
            if key in ('MACAddress','PermanentMACAddress'): pats=[x.lower() for x in pats]
            if not match(val,pats): possible=False
        if possible:
            if unknown: uncertain=True
            else: return 0
    return 2 if uncertain else 1

def interfaces(nic, target=None):
    state=path('/run/network/ifstate')
    if target is None and os.path.isfile(state):
        if any(line.split('=',1)[0].strip()==nic for line in read(state).splitlines()): return 0
    start=path('/etc/network/interfaces')
    if not os.path.exists(start): return 1
    visited=set(); claimed=[False]; included=[False]; uncertain=[False]
    refkeys=('iface','auto','allow-auto','allow-hotplug','bridge_ports','bridge-ports',
             'bond-slaves','bond-primary','slaves','ports','link','vlan-raw-device')
    def visit(p, depth=0):
        p=os.path.normpath(p)
        if depth>32: raise ValueError('interfaces source recursion too deep')
        if p in visited: return
        visited.add(p)
        text=read(p).replace('\\\n',' ')
        for raw in text.splitlines():
            s=raw.strip()
            if not s or s.startswith('#'): continue
            w=words(s)
            if not w: continue
            if w[0] in ('source','source-directory'):
                if len(w)<2: raise ValueError('empty source directive')
                for pattern in w[1:]:
                    if any(x in pattern for x in ('$','`','~')): raise ValueError('dynamic source path')
                    if pattern.startswith('/'):
                        pattern=path(pattern)
                    else: pattern=os.path.join(os.path.dirname(p),pattern)
                    pattern=os.path.normpath(pattern)
                    if w[0]=='source':
                        if target and path_match(target,pattern): included[0]=True
                        paths=sorted(glob.glob(pattern))
                    else:
                        paths=[]
                        for d in sorted(glob.glob(pattern)):
                            if not os.path.isdir(d): continue
                            if target and os.path.dirname(target)==d and re.match(r'^[a-zA-Z0-9_-]+$',os.path.basename(target)):
                                included[0]=True
                            paths.extend(os.path.join(d,x) for x in sorted(os.listdir(d)) if re.match(r'^[a-zA-Z0-9_-]+$',x))
                        # A not-yet-created standard interfaces.d directory is OK.
                        if target and path_match(os.path.dirname(target),pattern) and re.match(r'^[a-zA-Z0-9_-]+$',os.path.basename(target)):
                            included[0]=True
                    for f in paths:
                        if os.path.isfile(f): visit(f,depth+1)
                        elif os.path.islink(f): raise ValueError('broken source link: '+f)
                continue
            if w[0] in ('mapping','rename'):
                uncertain[0]=True
            if w[0] in refkeys or w[0].startswith('allow-'):
                for x in w[1:]:
                    if x==nic or x.startswith(nic+':'): claimed[0]=True
                    if '/' in x or '=' in x or x in ('regex','all'):
                        uncertain[0]=True
                    elif fnmatch.fnmatchcase(nic,x): claimed[0]=True
    visit(start)
    if target is not None: return 0 if included[0] and not uncertain[0] else 2
    if claimed[0]: return 0
    return 2 if uncertain[0] else 1

try:
    if MODE=='netplan': rc=netplan_claim(*ARGS)
    elif MODE=='renderer': rc=netplan_renderer()
    elif MODE=='nm-disk': rc=nm_disk(*ARGS)
    elif MODE=='ifcfg': rc=ifcfg(*ARGS)
    elif MODE=='networkd': rc=networkd(*ARGS)
    elif MODE=='ifupdown': rc=interfaces(ARGS[0])
    elif MODE=='ifup-included': rc=interfaces(ARGS[0],path(ARGS[1]))
    else: raise ValueError('unknown probe')
    sys.exit(rc)
except Exception as e:
    print('[config-check] '+str(e),file=sys.stderr)
    sys.exit(2)
PY
}

_si_netplan_present() {
    local d f
    for d in /lib/netplan /usr/lib/netplan /etc/netplan /run/netplan; do
        for f in "$(_si_path "$d")"/*.yaml; do
            _si_file "$f" && return 0
        done
    done
    return 1
}

_si_backend() {
    local nm=0 nd=0 legacy=0 ifup=0 count configured
    SI_BACKEND=''; SI_RENDERER=''
    _si_active NetworkManager && nm=1
    _si_active systemd-networkd && nd=1
    _si_active network && legacy=1
    _si_active networking && ifup=1
    if _si_netplan_present; then
        _si_has netplan || { _si_err '有 Netplan YAML 但缺少 netplan，拒绝绕过配置源。'; return 1; }
        ((legacy==0 && ifup==0 && nm+nd==1)) || {
            _si_err 'Netplan 的活跃管理后端不唯一，或与 legacy/ifupdown 混用；拒绝猜测。'; return 1;
        }
        configured="$(_si_probe renderer)" || { _si_err '无法解析 Netplan renderer（需要 PyYAML）。'; return 1; }
        if ((nm)); then SI_RENDERER=NetworkManager; else SI_RENDERER=networkd; fi
        [[ -z "$configured" || "$configured" == "$SI_RENDERER" ]] || {
            _si_err 'Netplan 全局 renderer 与当前活跃后端不同，拒绝写入。'; return 1;
        }
        SI_BACKEND=netplan
    else
        # EL7/8 may keep network.service active alongside NM; Debian may keep
        # networking.service for loopback while NM manages other interfaces.
        # Per-device NM state and all disk checks are still mandatory below.
        if ((nm && !nd && (legacy || ifup))); then
            _si_warn 'NM 与传统服务并存：仅处理 NM 已识别且未配置的设备；保留其他接口。'
            legacy=0; ifup=0
        fi
        count=$((nm+nd+legacy+ifup))
        ((count==1)) || { _si_err '网络管理服务未运行或同时有多个后端；不会启动、切换或重启服务。'; return 1; }
        if ((nm)); then
            _si_has nmcli || return 1; SI_BACKEND=nm
        elif ((nd)); then
            _si_has networkctl || return 1
            local version
            version="$(systemctl --version | awk 'NR==1 {print $2}')"
            [[ "$version" =~ ^[0-9]+$ ]] && ((version>=245)) || {
                _si_err '直接 networkd 模式需要 systemd >=245；EL7/8 请使用现有 NM 或 legacy 后端。'; return 1;
            }
            SI_BACKEND=networkd
        elif ((ifup)); then
            _si_has ifquery && _si_has ifup || return 1
            SI_BACKEND=ifupdown
        elif ((legacy)) && [[ "$SI_FAMILY" == el ]] && (( ${SI_VERSION%%.*} <= 8 )); then
            _si_has ifup || return 1; SI_BACKEND=el-ifcfg
        else
            _si_err '当前网络管理后端不在支持范围。'; return 1
        fi
    fi
    _si_log "系统：$SI_OS；网络后端：$SI_BACKEND ${SI_RENDERER:+($SI_RENDERER)}"
}

_si_nm_value() {
    local prop="$1" out
    shift
    out="$(nmcli -t -e no -f "$prop" "$@" 2>/dev/null)" || return 1
    [[ "$out" == "$prop:"* ]] || return 1
    out="${out#*:}"; [[ "$out" != '--' ]] || out=''
    printf '%s\n' "$out"
}

_si_nm_claimed() {
    local nic="$1" mac="$2" list uuid typ bound bmac
    _si_active NetworkManager || return 1
    _si_has nmcli || return 2
    list="$(nmcli -t -e no -f UUID,TYPE connection show 2>/dev/null)" || return 2
    while IFS=: read -r uuid typ; do
        [[ -n "$uuid" ]] || continue
        bound="$(_si_nm_value connection.interface-name connection show uuid "$uuid")" || return 2
        [[ "$bound" != "$nic" ]] || return 0
        case "$typ" in ethernet|802-3-ethernet) ;; *) continue ;; esac
        bmac="$(_si_nm_value 802-3-ethernet.mac-address connection show uuid "$uuid")" || return 2
        [[ "${bmac,,}" != "${mac,,}" ]] || return 0
        [[ -n "$bound" || -n "$bmac" ]] || return 0
    done <<< "$list"
    return 1
}

_si_networkd_live() {
    local nic="$1" result file
    _si_active systemd-networkd || return 1
    _si_has networkctl || return 2
    result="$(networkctl --no-pager status "$nic" 2>/dev/null)" || return 2
    file="$(printf '%s\n' "$result" | sed -n 's/^.*Network File:[[:space:]]*//p' | head -n 1)"
    case "$file" in
        n/a|N/A|none|None|'-') return 1 ;;
        '') return 2 ;;
        *) return 0 ;;
    esac
}

_si_claimed() {
    local nic="$1" mac="$2" driver="$3" check rc
    SI_REASON=''
    for check in netplan nm-live nm-disk networkd-live networkd ifupdown ifcfg; do
        rc=0
        case "$check" in
            netplan) if _si_netplan_present; then _si_probe netplan "$nic" "$mac" "$driver" || rc=$?; else rc=1; fi ;;
            nm-live) _si_nm_claimed "$nic" "$mac" || rc=$? ;;
            nm-disk|ifcfg) _si_probe "$check" "$nic" "$mac" || rc=$? ;;
            networkd-live) _si_networkd_live "$nic" || rc=$? ;;
            networkd) _si_probe networkd "$nic" "$mac" "$driver" || rc=$? ;;
            ifupdown) _si_probe ifupdown "$nic" || rc=$? ;;
        esac
        if ((rc==0)); then SI_REASON="已有配置：$check（含未激活配置）"; return 0; fi
        if ((rc!=1)); then SI_REASON="不能安全解析/确定配置：$check"; return 2; fi
    done
    return 1
}

_si_ssh_devices() {
    SI_SSH_DEVICES=''
    [[ -n "${SSH_CONNECTION:-}" ]] || return 0
    local client server client_port server_port route family
    read -r client client_port server server_port <<< "$SSH_CONNECTION"
    [[ -n "$client" && -n "$server" ]] || { _si_err 'SSH_CONNECTION 格式不完整。'; return 1; }
    if [[ "$client" == *:* ]]; then family=-6; else family=-4; fi
    # Use the session source address to respect source-based policy routing.
    route="$(ip "$family" route get "$client" from "$server" 2>/dev/null)" || {
        _si_err '无法确定 SSH 回程路由；为避免误改，停止网络初始化。'; return 1;
    }
    SI_SSH_DEVICES="$(printf '%s\n' "$route" | awk '{for(i=1;i<NF;i++) if($i=="dev") print $(i+1)}')"
    [[ -n "$SI_SSH_DEVICES" ]] || { _si_err '无法确定 SSH 出口网卡。'; return 1; }
}

_si_eligible() {
    local nic="$1" item devpath addr state dtype claimed=0
    SI_REASON=''; SI_MAC=''; SI_DRIVER=''
    item="$(_si_path /sys/class/net)/$nic"
    _si_nic_valid "$nic" || { SI_REASON='特殊/非法接口名'; return 1; }
    [[ -e "$item" ]] || { SI_REASON='网卡不存在'; return 2; }
    [[ -r "$item/type" && "$(cat "$item/type")" == 1 && -e "$item/device" ]] || { SI_REASON='非硬件以太网接口'; return 1; }
    [[ ! -e "$item/master" && ! -e "$item/wireless" && ! -e "$item/phy80211" ]] || { SI_REASON='无线网卡或 bond/bridge/VRF 从接口'; return 1; }
    case "$nic" in lo|wl*|ww*|usb*|enp0s20f0u6u3c2) SI_REASON='保留/USB/无线接口'; return 1 ;; esac
    devpath="$(readlink -f "$item/device")" || { SI_REASON='设备路径不可读'; return 2; }
    [[ "$devpath" != *'/usb'* ]] || { SI_REASON='USB 网卡'; return 1; }
    # Skip interfaces already referenced by upper devices (including VLANs).
    local upper
    for upper in "$item"/upper_*; do
        if _si_file "$upper"; then SI_REASON='已被上层虚拟接口引用'; return 1; fi
    done
    if printf '%s\n' "$SI_SSH_DEVICES" | grep -Fxq -- "$nic"; then SI_REASON='承载当前 SSH 会话'; return 1; fi
    if [[ -r "$item/addr_assign_type" ]] && [[ "$(cat "$item/addr_assign_type")" != 0 ]]; then
        SI_REASON='当前 MAC 并非永久硬件地址（可能被改写）；不能安全绑定'; return 1
    fi
    SI_MAC="$(cat "$item/address")" || { SI_REASON='MAC 不可读'; return 2; }
    [[ "$SI_MAC" =~ ^([[:xdigit:]]{2}:){5}[[:xdigit:]]{2}$ ]] || { SI_REASON='MAC 不合法'; return 2; }
    [[ "$SI_MAC" != '00:00:00:00:00:00' ]] || { SI_REASON='MAC 全零'; return 2; }
    if [[ -L "$item/device/driver" ]]; then SI_DRIVER="$(basename "$(readlink -f "$item/device/driver")")"; fi
    ip link show dev "$nic" >/dev/null 2>&1 || { SI_REASON='读取接口失败'; return 2; }
    addr="$(ip -o addr show dev "$nic" scope global 2>/dev/null)" || { SI_REASON='读取 IP 失败'; return 2; }
    [[ -z "$addr" ]] || { SI_REASON='已有 IPv4/IPv6 地址'; return 1; }
    if [[ "$SI_BACKEND" == nm || ( "$SI_BACKEND" == netplan && "$SI_RENDERER" == NetworkManager ) ]]; then
        dtype="$(_si_nm_value GENERAL.TYPE device show "$nic")" || { SI_REASON='NM 类型读取失败'; return 2; }
        [[ "$dtype" == ethernet || "$dtype" == 802-3-ethernet ]] || { SI_REASON='非 NM 以太网设备'; return 1; }
        state="$(_si_nm_value GENERAL.STATE device show "$nic")" || { SI_REASON='NM 状态读取失败'; return 2; }
        case "${state%% *}" in 20|30) ;; *) SI_REASON="保留 NM 现有状态：$state"; return 1 ;; esac
    fi
    _si_claimed "$nic" "$SI_MAC" "$SI_DRIVER" || claimed=$?
    case "$claimed" in 0) return 1 ;; 1) return 0 ;; *) return 2 ;; esac
}

_si_has_existing_network() {
    local r4 r6 addresses
    r4="$(ip -4 route show table all 2>/dev/null)" || return 2
    if ! r6="$(ip -6 route show table all 2>/dev/null)"; then
        [[ ! -d "$(_si_path /proc/sys/net/ipv6)" ]] || return 2
        r6='' # IPv6 can be disabled at boot or absent from the kernel.
    fi
    addresses="$(ip -o addr show scope global 2>/dev/null)" || return 2
    # Any configured address is treated as an existing network, even without a
    # main-table default route. This protects policy-routed and IPv6-only hosts.
    if [[ -n "$addresses" ]] || printf '%s\n%s\n' "$r4" "$r6" | grep -Eq '(^|[[:space:]])default([[:space:]]|$)'; then return 0; fi
    return 1
}

_si_data() {
    local nic="$1" secondary="$2" mac="$3"
    case "$SI_BACKEND" in
        netplan)
            printf '# safe-server-init v3; DHCPv4 only; existing files are untouched.\nnetwork:\n  version: 2\n  ethernets:\n    "%s":\n      renderer: %s\n      dhcp4: true\n      dhcp6: false\n      accept-ra: false\n      optional: true\n' "$nic" "$SI_RENDERER"
            if ((secondary)); then
                printf '      dhcp4-overrides:\n        use-routes: false\n        use-dns: false\n        route-metric: 20000\n'
            fi
            if [[ "$SI_RENDERER" == NetworkManager ]]; then
                # use-dns:false alone is networkd-only in Netplan. Require NM
                # passthrough (Netplan >=0.102); native validation fails closed
                # on older versions rather than silently permitting DNS takeover.
                printf '      networkmanager:\n        passthrough:\n          ipv6.method: "link-local"\n'
                if ((secondary)); then
                    printf '          ipv4.ignore-auto-dns: "true"\n          ipv4.ignore-auto-routes: "true"\n          ipv4.never-default: "true"\n'
                fi
            fi ;;
        networkd)
            printf '# safe-server-init v3\n[Match]\nName=%s\nMACAddress=%s\n\n[Link]\nRequiredForOnline=no\n\n[Network]\nDHCP=ipv4\nIPv6AcceptRA=no\nLinkLocalAddressing=ipv6\n\n[DHCPv4]\n' "$nic" "$mac"
            if ((secondary)); then printf 'UseRoutes=no\nUseDNS=no\nUseNTP=no\nUseHostname=no\nUseDomains=no\nRouteMetric=20000\n'; fi ;;
        ifupdown)
            printf '# safe-server-init v3; allow-hotplug is DEVICE hotplug, not cable insertion.\nallow-hotplug %s\niface %s inet dhcp\n' "$nic" "$nic"
            if ((secondary)); then printf '    metric 20000\n'; fi ;;
        el-ifcfg)
            printf '# safe-server-init v3\nTYPE=Ethernet\nDEVICE=%s\nNAME=%s\nHWADDR=%s\nONBOOT=yes\nBOOTPROTO=dhcp\nNM_CONTROLLED=no\nIPV6INIT=no\n' "$nic" "$nic" "$mac"
            if ((secondary)); then printf 'DEFROUTE=no\nPEERDNS=no\nMETRIC=20000\n'; fi ;;
        *) return 1 ;;
    esac
}

_si_target() {
    local nic="$1"
    case "$SI_BACKEND" in
        netplan) printf '/etc/netplan/90-safe-dhcp-%s.yaml\n' "$nic" ;;
        networkd) printf '/etc/systemd/network/90-safe-dhcp-%s.network\n' "$nic" ;;
        ifupdown) printf '/etc/network/interfaces.d/safe-dhcp-%s\n' "${nic//./_}" ;;
        el-ifcfg) printf '/etc/sysconfig/network-scripts/ifcfg-%s\n' "$nic" ;;
        *) return 1 ;;
    esac
}

_si_target_conflict() {
    local target="$1" dir file base="${1##*/}"
    local -a dirs=()
    case "$SI_BACKEND" in
        netplan) dirs=(/lib/netplan /usr/lib/netplan /etc/netplan /run/netplan) ;;
        networkd) dirs=(/usr/lib/systemd/network /usr/local/lib/systemd/network /lib/systemd/network /run/systemd/network /etc/systemd/network) ;;
        *) if _si_file "$(_si_path "$target")"; then return 0; else return 1; fi ;;
    esac
    for dir in "${dirs[@]}"; do
        file="$(_si_path "$dir")/$base"
        if _si_file "$file"; then
            _si_err "同名文件已存在于后端搜索目录：$file"
            return 0
        fi
    done
    return 1
}

_si_write_new() {
    local target="$1" data="$2" perms="${3:-600}" dir tmp
    if _si_file "$target"; then _si_err "不覆盖已有文件：$target"; return 1; fi
    dir="${target%/*}"
    # networkd runs unprivileged: newly created configuration directories must
    # be traversable. Existing directory permissions are never widened.
    (umask 022; mkdir -p -- "$dir") || return 1
    tmp="$(mktemp "$dir/.safe-init.XXXXXX")" || return 1
    if ! printf '%s\n' "$data" > "$tmp" || ! chmod "$perms" "$tmp"; then rm -f -- "$tmp"; return 1; fi
    # Hard-link publish is atomic and fails if destination already exists.
    if ! ln -- "$tmp" "$target"; then rm -f -- "$tmp"; return 1; fi
    rm -f -- "$tmp"
    if _si_has restorecon; then
        restorecon "$target" >/dev/null 2>&1 || { _si_err "已写入但 SELinux 标签恢复失败：$target"; return 1; }
    fi
    _si_log "已创建：$target"
}

_si_netplan_validate() {
    local tmp src dir nic sec target i
    tmp="$(mktemp -d /tmp/safe-netplan.XXXXXXXX)" || return 1
    for dir in /lib/netplan /usr/lib/netplan /etc/netplan /run/netplan; do
        mkdir -p "$tmp$dir" || { rm -rf -- "$tmp"; return 1; }
        for src in "$(_si_path "$dir")"/*.yaml; do
            _si_file "$src" || continue
            if ! cp -pL -- "$src" "$tmp$dir/${src##*/}"; then rm -rf -- "$tmp"; return 1; fi
        done
    done
    for ((i=0;i<${#SI_CANDIDATES[@]};i++)); do
        nic="${SI_CANDIDATES[$i]}"; sec="${SI_SECONDARY[$i]}"
        target="$tmp$(_si_target "$nic")"
        if _si_file "$target" || ! _si_data "$nic" "$sec" "${SI_MACS[$i]}" > "$target" || ! chmod 600 "$target"; then
            rm -rf -- "$tmp"; return 1
        fi
    done
    # Native version validation: unsupported keys fail before any real writes.
    if ! netplan generate --root-dir "$tmp" > "$tmp/check.log" 2>&1; then
        _si_err 'Netplan 隔离生成验证失败；真实配置未修改：'
        tail -n 30 "$tmp/check.log" >&2
        rm -rf -- "$tmp"; return 1
    fi
    rm -rf -- "$tmp"
    _si_log 'Netplan 已通过本机版本的隔离生成验证（未执行 apply）。'
}

_si_nm_args() {
    local nic="$1" secondary="$2" mac="$3" uuid="$4"
    SI_NM_ARGS=(connection add type ethernet ifname "$nic" con-name "safe-dhcp-$nic"
        connection.uuid "$uuid" connection.autoconnect yes
        802-3-ethernet.mac-address "$mac" ipv4.method auto ipv4.may-fail no ipv6.method link-local)
    if ((secondary)); then
        SI_NM_ARGS+=(ipv4.never-default yes ipv4.ignore-auto-routes yes ipv4.ignore-auto-dns yes ipv4.route-metric 20000)
    fi
}

_si_network() {
    local dry=0 iface='' primary_req='' primary='' legacy_secondary=0 arg item nic rc target
    local existing=0 issues=0 count=0 failures=0 i secondary data names uuid addr perms
    local -a SI_CANDIDATES=() SI_MACS=() SI_SECONDARY=() SI_NM_ARGS=()
    while (($#)); do
        arg="$1"; shift
        case "$arg" in
            --dry-run) dry=1 ;;
            --allow-legacy-secondary) legacy_secondary=1 ;;
            --iface|--primary)
                (($#)) && _si_nic_valid "$1" || { _si_err "$arg 需要合法网卡名。"; return 2; }
                if [[ "$arg" == --iface ]]; then iface="$1"; else primary_req="$1"; fi
                shift ;;
            *) _si_err "未知 network 参数：$arg"; return 2 ;;
        esac
    done
    [[ -z "$iface" || -z "$primary_req" || "$iface" == "$primary_req" ]] || {
        _si_err '--iface 与 --primary 同时指定时必须是同一网卡。'; return 2;
    }
    _si_init || return 1
    _si_has ip || { _si_err '缺少 ip 命令。'; return 1; }
    _si_python_select || return 1
    ((dry)) || _si_lock || return 1
    _si_backend || return 1
    _si_ssh_devices || return 1
    if [[ -n "$iface" && ! -e "$(_si_path /sys/class/net)/$iface" ]]; then _si_err "网卡不存在：$iface"; return 2; fi
    for item in "$(_si_path /sys/class/net)"/*; do
        [[ -e "$item" ]] || continue
        nic="${item##*/}"; [[ "$nic" != lo ]] || continue
        [[ -z "$iface" || "$nic" == "$iface" ]] || continue
        rc=0; _si_eligible "$nic" || rc=$?
        if ((rc==0)); then
            SI_CANDIDATES+=("$nic"); SI_MACS+=("$SI_MAC")
        else
            _si_log "跳过 $nic：$SI_REASON"
            if ((rc==2)); then issues=$((issues+1)); fi
            if [[ -n "$iface" ]]; then _si_warn '指定接口未修改。'; return 3; fi
        fi
    done
    if [[ -n "$primary_req" ]]; then
        for nic in "${SI_CANDIDATES[@]}"; do [[ "$nic" != "$primary_req" ]] || primary="$nic"; done
        [[ -n "$primary" ]] || { _si_err "--primary $primary_req 不是未配置候选网卡；未写入。"; return 2; }
    fi
    if ((${#SI_CANDIDATES[@]}==0)); then
        _si_log '没有符合条件的未配置硬件网卡；没有更改网络。'
        ((issues==0)) && return 0
        return 3
    fi
    rc=0; _si_has_existing_network || rc=$?
    case "$rc" in 0) existing=1 ;; 1) existing=0 ;; *) _si_err '路由或地址读取失败；拒绝猜测。'; return 1 ;; esac
    if [[ -z "$primary" ]]; then
        for nic in "${SI_CANDIDATES[@]}"; do
            if [[ "$(cat "$(_si_path /sys/class/net)/$nic/carrier" 2>/dev/null)" == 1 ]]; then primary="$nic"; break; fi
        done
        [[ -n "$primary" ]] || primary="${SI_CANDIDATES[0]}"
    fi
    if ((existing)); then
        _si_log '已有 IP/默认路由：所有新增接口均按次要接口处理，不主动接管默认路由/DNS。'
        [[ -z "$primary_req" ]] || _si_warn '--primary 不覆盖已有网络；本次仍按次要接口处理。'
    else
        _si_log "尚无已配置 IP/默认路由：主 DHCP 网卡为 $primary。"
    fi
    # Complete all preflight checks BEFORE writing any backend configuration.
    for ((i=0;i<${#SI_CANDIDATES[@]};i++)); do
        nic="${SI_CANDIDATES[$i]}"; secondary=1
        if ((!existing)) && [[ "$nic" == "$primary" ]]; then secondary=0; fi
        SI_SECONDARY+=("$secondary")
        if [[ "$SI_BACKEND" == ifupdown || "$SI_BACKEND" == el-ifcfg ]] && ((secondary && !legacy_secondary)); then
            _si_err "$SI_BACKEND 无法跨 DHCP 客户端保证次要网卡的路由/DNS 隔离；本次未写入任何配置。"
            _si_warn '可用 --iface 仅选无现有网络时的主网卡；仅在接受上述风险时添加 --allow-legacy-secondary。'
            return 3
        fi
        if [[ "$SI_BACKEND" != nm ]]; then
            target="$(_si_target "$nic")"
            if _si_target_conflict "$target"; then _si_err "目标文件名冲突：$target"; return 1; fi
            if [[ "$SI_BACKEND" == ifupdown ]]; then
                _si_probe ifup-included "$nic" "$target" || { _si_err 'interfaces 没有明确包含目标文件，或有复杂映射；未写入。'; return 1; }
            fi
        fi
    done
    if [[ "$SI_BACKEND" == netplan ]]; then _si_netplan_validate || return 1; fi
    if [[ "$SI_BACKEND" == nm ]]; then
        names="$(nmcli -t -e no -f NAME connection show)" || return 1
        for nic in "${SI_CANDIDATES[@]}"; do
            if printf '%s\n' "$names" | grep -Fxq -- "safe-dhcp-$nic"; then _si_err "连接名称已存在：safe-dhcp-$nic"; return 1; fi
        done
    fi
    if ((legacy_secondary)) && [[ "$SI_BACKEND" == ifupdown || "$SI_BACKEND" == el-ifcfg ]]; then
        _si_warn '已显式允许传统后端的次要 DHCP：高 metric/DEFROUTE 不等于完整路由及 DNS 隔离。'
    fi
    for ((i=0;i<${#SI_CANDIDATES[@]};i++)); do
        nic="${SI_CANDIDATES[$i]}"; secondary="${SI_SECONDARY[$i]}"
        # Recheck runtime state immediately before each write (NM can autoconnect).
        if ((!dry)); then
            rc=0; _si_eligible "$nic" || rc=$?
            if ((rc!=0)) || [[ "$SI_MAC" != "${SI_MACS[$i]}" ]]; then
                _si_err "$nic 状态/配置在检查后发生变化，停止后续写入：$SI_REASON"
                failures=$((failures+1)); break
            fi
        fi
        if ((!dry && !secondary)); then
            rc=0; _si_has_existing_network || rc=$?
            if ((rc!=1)); then
                _si_err '主网卡写入前检测到已有网络或读取异常；停止，避免抢占新出现的路由。'
                failures=$((failures+1)); break
            fi
        fi
        if [[ "$SI_BACKEND" == nm ]]; then
            uuid="$(cat "$(_si_path /proc/sys/kernel/random/uuid)")" || return 1
            [[ "$uuid" =~ ^[0-9a-fA-F-]{36}$ ]] || return 1
            _si_nm_args "$nic" "$secondary" "${SI_MACS[$i]}" "$uuid"
            if ((dry)); then
                printf '[预览] nmcli '; printf '%q ' "${SI_NM_ARGS[@]}"; printf '\n'
            else
                if ! nmcli --wait 30 "${SI_NM_ARGS[@]}"; then
                    _si_err "创建失败：$nic；请检查 UUID=$uuid 是否留下部分配置。"
                    failures=$((failures+1)); break
                fi
                _si_log "已创建 DHCPv4 profile：safe-dhcp-$nic；UUID=$uuid（可能自动连接）"
            fi
        else
            target="$(_si_path "$(_si_target "$nic")")"
            data="$(_si_data "$nic" "$secondary" "${SI_MACS[$i]}")" || return 1
            perms=600
            [[ "$SI_BACKEND" != networkd ]] || perms=644
            if ((dry)); then printf '\n[预览] %s (0%s)：\n%s\n' "$target" "$perms" "$data"
            elif ! _si_write_new "$target" "$data" "$perms"; then failures=$((failures+1)); break
            fi
        fi
        count=$((count+1))
    done
    if ((dry)); then
        _si_log "预览结束：计划 $count 项；系统配置未改写。"
    else
        _si_log "已创建 $count 项；失败 $failures 项；检查不确定 $issues 项。未删除原配置、未全局应用或重启网络。"
        case "$SI_BACKEND" in
            nm)
                for nic in "${SI_CANDIDATES[@]}"; do
                    addr="$(ip -4 -o addr show dev "$nic" scope global 2>/dev/null)" || addr=''
                    if [[ -n "$addr" ]]; then _si_log "$nic 当前 IPv4：$addr"
                    else _si_warn "$nic 当前尚无 IPv4：配置保存不代表已获得 DHCP 租约。"; fi
                done ;;
            netplan) _si_warn '尚未应用。通过控制台/IPMI 或可靠回滚环境审查后执行 netplan try --timeout 120；它会应用整套 Netplan 配置。' ;;
            networkd) _si_warn '尚未重载。控制台审查后 networkctl reload，再对新增网卡 networkctl reconfigure 网卡名。reload 也可能应用其他待处理配置。' ;;
            ifupdown|el-ifcfg) _si_warn '尚未激活。控制台审查后对新增网卡执行 ifup 网卡名。allow-hotplug 不保证插网线时重新发起 DHCP。' ;;
        esac
        _si_warn '多网卡同网段的直连路由、ARP、系统自定义 hook/dispatcher 仍可能影响联网；本脚本不能保证绝对不断网。'
    fi
    ((failures==0)) || return 1
    ((issues==0)) || return 3
    return 0
}

# Subshells prevent changing a source caller's locale, umask, traps, or descriptors.
set-hostname() ( set +e; set +u; set +o pipefail; _si_hostname "$@"; )
set-network()  ( set +e; set +u; set +o pipefail; _si_network "$@"; )

_si_usage() {
    cat <<'HELP'
safe-server-init.sh v3.0.0
目标：EL7-EL10 / Ubuntu20-26 / Debian10-13，现有网络管理器，不切换服务。
用法：
  bash safe-server-init.sh hostname NAME [--update-hosts] [--dry-run]
  bash safe-server-init.sh network [--dry-run] [--iface NIC] [--primary NIC]
                                  [--allow-legacy-secondary]
  bash safe-server-init.sh status
  bash safe-server-init.sh --version

hostname：校验、备份、hostnamectl 设置并回读验证。--update-hosts 才同步 hosts。
network：只给无配置/无 IP 的硬件以太网卡新增 DHCPv4；不是静态 IP 转 DHCP 工具。
不改已有 DHCP/静态地址、VLAN/Bond/Bridge，不处理 USB/Wi-Fi/SSH 出口。
--primary：仅在没有已配置 IP 或默认路由时，选一个允许接受 DHCP 网关/DNS的接口。
--allow-legacy-secondary：允许 ifupdown/传统 ifcfg 配置次要 DHCP，不能保证路由/DNS隔离。
NM 新 profile 可能自动激活；Netplan/networkd/ifupdown/ifcfg 仅保存配置。
未知/复杂/混用配置拒绝猜测；dry-run 会创建并删除验证临时文件，不改系统配置。
Python >=2.7 标准库用于静态检查（优先 Python3）；Netplan 另需现有 PyYAML。
退出码：0 成功/无变更，1 运行错误，2 参数错误，3 安全跳过/未完整完成。
HELP
}

_si_main() {
    local action="${1:-help}"
    (($#==0)) || shift
    case "$action" in
        hostname) set-hostname "$@" ;;
        network) set-network "$@" ;;
        status)
            export LC_ALL=C
            hostnamectl || return 1
            ip -o addr show || return 1
            ip -4 route show table all || return 1
            ip -6 route show table all ;;
        -h|--help|help) _si_usage ;;
        --version) printf 'safe-server-init.sh 3.0.0\n' ;;
        *) _si_err "未知命令：$action"; _si_usage; return 2 ;;
    esac
}
if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then _si_main "$@"; fi
