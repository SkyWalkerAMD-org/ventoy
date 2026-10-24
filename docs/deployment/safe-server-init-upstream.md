# safe-server-init.sh v3.0.0 — 使用说明与审查记录

审查日期：2026-10-23

## 1. 这版脚本做什么

保留 `set-hostname()` 和 `set-network()` 两个函数，可以直接执行，也可以通过 `source` 导入。

**这是一份保守的初始化脚本，不是把所有现有网卡强制改为 DHCP 的脚本。**

- `hostname`：校验名称，调用 `hostnamectl` 设置，回读静态及运行中的名称。修改前备份相关文件；只有指定 `--update-hosts` 才同步本机 hosts 映射。
- `network`：为没有现有配置、没有已有全局地址的硬件以太网接口新增持久化 IPv4 DHCP 配置。既有静态 IP、DHCP、桥接、Bond、VLAN 等保留。
- 不安装软件、不换源、不切换网络管理器、不删除原连接，不执行全局 `netplan apply`、网络服务重启或 `ifdown`。
- 不承诺“绝对不断网”。新增 IP 的直连路由、同网段多网卡、ARP、自定义 dispatcher/hook、外部配置管理和并发管理员操作，都可能影响网络。

## 2. 目标范围与边界

| 目标系统 | 脚本处理的后端 |
|---|---|
| EL 7–8：RHEL / CentOS / Rocky / AlmaLinux / Oracle Linux 等已列入识别表的系统 | 活跃的 NetworkManager；或传统 network-scripts |
| EL 9–10 | 通常为 NetworkManager；不会尝试恢复已经不适合该版本的旧 network-scripts |
| Ubuntu 20.x–26.x | 已有 Netplan；或独立 NetworkManager / systemd-networkd |
| Debian 11–13 | ifupdown、NetworkManager、systemd-networkd；如已有 Netplan，则优先保留其配置源 |

上述是代码的适配目标与检查范围，不是逐发行版实机认证。

运行依赖：Bash ≥4.2、systemd、iproute2、flock、常规 GNU 系统工具，以及系统现有的网络管理工具。静态配置检查优先使用 Python 3，也保留 Python 2.7 的语法兼容写法供 EL7 使用；本次未实际运行 Python 2.7 解释器。Netplan 分支需要现有 PyYAML。

直接 systemd-networkd 分支要求 systemd ≥245。EL7/8 应使用其现有 NetworkManager 或传统后端。Ubuntu/Debian 的极简镜像若缺依赖，会明确退出，不会自动下载安装。

Netplan + NetworkManager 分支使用 `networkmanager.passthrough`，要求本机 Netplan 支持该功能（上游从 0.102 引入）。某些未更新的 Ubuntu 20.04 初始镜像不满足该条件：脚本会在隔离验证阶段拒绝写入，而不是忽略 DNS 保护。独立 NM 和 Netplan + networkd 不依赖此功能。

只识别“有硬件 device 信息的以太网接口”；常见 virtio 等虚拟机设备可能满足条件。veth、桥、VLAN 等软件接口、USB 网卡、无线接口、已挂到上层设备的接口默认跳过。MAC 被改写且不能安全绑定时也跳过。

复杂或未知的配置（例如 networkd drop-in、ifupdown 动态映射、动态 ifcfg、未知 Netplan 匹配规则）保守拒绝；不能把“跳过”理解成“已经改为 DHCP”。

## 3. 推荐使用流程

将脚本放到服务器目录，例如 `/root/safe-server-init.sh`。可以直接用 bash 执行，不必先 chmod。

### 设置主机名

先预览：

```bash
bash /root/safe-server-init.sh hostname 14900ks1 --update-hosts --dry-run
```

执行：

```bash
bash /root/safe-server-init.sh hostname 14900ks1 --update-hosts
```

不修改 hosts 时去掉 `--update-hosts`。脚本会将大写转为小写；主机名总长不超过 64，每段不超过 63，只接受合法 ASCII DNS 名称。不接受 IP 地址、空格、下划线、中文或保留的 localhost 名称。

`--update-hosts` 仅更新 loopback 行上的旧主机名/短名，保留 localhost 与其他别名，不重写远端主机映射。必要时添加 `127.0.1.1` 本机映射。原 hosts 是符号链接时拒绝自动改写；bind mount 也可能使替换失败，此时会明确报告主机名已经改变、hosts 没有改变，而不是报告整体成功。

备份位置会输出在终端：

```text
/var/backups/safe-server-init/日期-时间.随机后缀/
```

打开着的终端提示符可能继续显示旧名称，重新登录后查看。cloud-init、云平台和配置管理工具可能在后续启动时重写主机名/hosts；脚本只提醒，不擅自禁用这些工具。

### 新增 DHCP 配置

先预览：

```bash
bash /root/safe-server-init.sh network --dry-run
```

再执行：

```bash
bash /root/safe-server-init.sh network
```

只处理一张未配置网卡：

```bash
bash /root/safe-server-init.sh network --iface enp7s0 --dry-run
bash /root/safe-server-init.sh network --iface enp7s0
```

在没有已有 IP 或默认路由的空白环境中，指定主 DHCP 网卡：

```bash
bash /root/safe-server-init.sh network --primary enp6s0 --dry-run
bash /root/safe-server-init.sh network --primary enp6s0
```

`--primary` 不会覆盖已有网络。系统已经有 IPv4/IPv6 地址，或者任意路由表中有默认路由时，新网卡按次要网卡处理。这里不仅检查主路由表，还检查策略路由；SSH 回程路由检查使用客户端地址与当前会话的服务端源地址。

**既有静态 IP / DHCP 的接口，以及承载当前 SSH 会话的接口，仍然不会被强制改动，即使传入 `--iface`。**

## 4. “自动 DHCP”何时真正生效

| 后端 | `network` 执行后的状态 |
|---|---|
| NetworkManager | 创建 autoconnect DHCPv4 profile；新增网卡可能立即自动连接。没有地址时明确提示尚未取得 IPv4，不用固定 sleep 冒充成功。 |
| Netplan | 写入 YAML 前，用本机 `netplan generate --root-dir` 在隔离目录验证合并后的配置；真实系统只保存 YAML，不全局 apply。 |
| systemd-networkd | 保存 `.network` 文件，不 reload/reconfigure。文件采用 0644，新目录采用可遍历权限，使非特权 networkd 服务能读取。 |
| ifupdown | 保存 interfaces.d 配置，并确认包含关系；不主动 ifup。 |
| EL7/8 传统 network-scripts | 保存 ifcfg 文件，不主动 ifup。 |

这些配置是持久化文件，但重启后能否自动联网，还取决于后端服务是否开机启用、网线/交换机状态及是否存在 DHCP 服务器。脚本不替用户执行 `systemctl enable`。

对非 NM 后端，应在控制台/IPMI 或已有可靠恢复通道的环境下审查后应用：

```bash
# 仅适用于 Netplan；应用的是整套 Netplan 配置，不只是新增网卡。
netplan try --timeout 120

# 仅适用于直接 systemd-networkd；reload 也可能作用于其他待处理配置。
networkctl reload
networkctl reconfigure enp7s0

# 仅适用于 ifupdown / 传统 network-scripts，且 enp7s0 是本次新增配置的接口。
ifup enp7s0
```

上面三组不是让你全部执行，必须选择实际后端对应的一组。`netplan try` 的超时回滚不是任何情况下都可靠，不能代替带外恢复通道。

新增 NM / Netplan / networkd 配置以 IPv4 DHCP 为目标，不主动开启全局 IPv6 自动配置；它们保留链路本地 IPv6 或由配置限制 RA。传统 ifcfg 显式不初始化 IPv6。ifupdown 分支不更改全局或接口的内核 IPv6 策略，因此内核原有 RA/SLAAC 行为可能仍存在。所有已有网卡的 IPv6 配置不变。

## 5. 传统后端的多网卡限制

ifupdown 可以调用不同 DHCP 客户端，network-scripts 也可能受 dhclient hook 影响。仅提高 route metric 或设置 DEFROUTE，不足以跨环境保证“所有 DHCP 路由和 DNS 都不受影响”。

因此，**默认拒绝在传统后端上新增次要 DHCP 配置，而且在任何文件写入前退出**。这不是脚本报错漏配，而是安全边界。

只有明确接受这项风险、且有控制台恢复能力时，才使用：

```bash
bash /root/safe-server-init.sh network --allow-legacy-secondary --dry-run
bash /root/safe-server-init.sh network --allow-legacy-secondary
```

该参数仍不删除原配置、不主动执行 ifup，也不解除已有 IP/配置/SSH 保护。

ifupdown 的 `allow-hotplug` 指设备出现事件，不是“插入网线事件”。不能保证在运行脚本时没插网线，之后只插线就一定重新发起 DHCP；需要按客户端/后端情况检查或显式 ifup。

## 6. 验证结果与退出码

```bash
bash /root/safe-server-init.sh status
hostnamectl
ip -o addr show
ip -4 route show table all
ip -6 route show table all

# 只有 NM 后端才需要下面两项：
nmcli connection show
nmcli device status
```

已有网络正常且没有候选网卡时，脚本返回 0 并说明没有更改；这不表示已把所有网卡转成 DHCP。

| 退出码 | 含义 |
|---|---|
| 0 | 操作成功，或正常无变更 |
| 1 | 命令/解析/写入/验证失败；可能已经完成部分操作，按输出核查 |
| 2 | 参数、名称、系统范围等输入问题（部分系统范围检查返回 1） |
| 3 | 显式指定接口被保护、检查不确定或安全策略阻止完整执行 |

检查返回值应紧跟执行命令：

```bash
bash /root/safe-server-init.sh network --dry-run
rc=$?
printf 'exit=%s\n' "$rc"
```

## 7. 导入原来的初始化脚本

```bash
source /root/safe-server-init.sh

set-hostname 14900ks1 --update-hosts
set-network --dry-run
# 确认输出后，再执行：
set-network
```

仅 source 不执行初始化。两个公共函数在子 Shell 中运行，不改变调用者的 locale、umask、shell 选项、trap 或锁描述符。错误通过返回码传递，外层自动化应检查返回码。

## 8. 本次审查修正

1. 修复旧版 networkd 字符串拼接造成的 `DHCP=ipv4IPv6AcceptRA=no` 无效配置。
2. 主机名长度从错误的 253 改为 Linux 64 字符限制；增加运行中名称回读，修复静态名称相同但运行中名称不同的漏改。
3. 补充未激活/尚未加载的 NM、ifcfg、Netplan、networkd、ifupdown 配置检测；不执行待检查文件中的 shell 代码。
4. 保护不同搜索目录下的同名文件，防止新 `/etc` 文件覆盖 `/run` 或系统目录中的其他接口配置；复杂 drop-in 不猜测。
5. 修正 NetworkManager 只禁用默认路由却仍可能接受 DHCP 额外路由的问题；次要连接同时忽略自动路由和 DNS。
6. 修正 Netplan + NM 仅设置 `use-dns: false` 无法达到 DNS 隔离的问题，使用专用 passthrough，交给本机版本验证，不支持则停止。
7. 正确处理 ifupdown 的 source/source-directory、相对路径、递归包含和文件名规则；路径通配符不能跨目录层级误判。
8. 检查 IPv6 地址、所有路由表、带源地址的 SSH 回程；不因只有 IPv6 或策略路由而误认为机器未联网。
9. 增加进程锁、不覆盖/原子发布、写入前再次核对、明确退出码；不把配置保存等同 DHCP 成功。
10. 检查 networkd 服务读取权限：独立 networkd 文件 0644，新建目录可遍历；其他新增配置文件 0600。
11. EL7/8 常见的 NM 与 network.service 并存不再一概误判为必须切换；仍要求设备处于可处理 NM 状态并通过全部配置检查。
12. 增加主机名修改备份、可选 hosts 同步及相应失败状态报告。

## 9. 本次实际做过的验证

环境：Debian 13 容器，GNU Bash 5.2.37，Python 3.13.5。

- `bash -n`：通过。
- 133 项 pytest 检查：通过。覆盖名称边界、目标 OS 元数据、各后端解析、配置输出、dry-run、重复执行、SSH/IPv6/策略路由保护、冲突文件、错误退出和模拟完整流程。
- 在隔离临时目录内，以实际 `systemd-network` 非特权账户读取生成文件：通过。
- 所有 hostnamectl / systemctl / nmcli / ip / netplan 的完整流程测试使用 mock 命令；没有修改宿主机真实网络或主机名。

**没有完成的验证：** EL7–10、Ubuntu20–26、Debian11–13 逐版本真实系统启动与 DHCP 租约联调；真实旧版 Bash/Python2.7；真实 NM/Netplan 各版本解析；实际 SSH 断线/回滚故障注入；ShellCheck 静态分析。因此不能把上述 133 项测试当成每个发行版、每种网络拓扑都已实测通过。

脚本运行到真实 Netplan 机器时，会使用那台机器的原生生成器做隔离验证；这与本次测试环境中的 mock 验证是两回事。

完整包内附 `tests/test_safe_server_init.py`、测试结果和 XML 报告。重跑测试建议使用一次性 root 测试容器，安装 pytest/PyYAML 后，在完整包根目录执行：

```bash
SSI_SCRIPT="$PWD/safe-server-init.sh" python3 -m pytest -q tests/test_safe_server_init.py
```

## 10. 回退原则

网络分支不改已有配置，因此不会生成“整机网络自动还原”。需要撤销时，先核实终端输出中本次新增的文件或 NM UUID，只处理这些新增项，不要批量删除所有连接。删除已激活 NM profile 会断开对应接口，必须在可恢复的控制台环境操作。文件后端删除新文件也不会自动回退正在运行的配置。

主机名备份在终端给出的目录里；恢复时用旧名称调用 hostnamectl，并按需恢复备份 hosts。不要把整机配置自动替换为任意旧快照。

## 11. 核对的上游资料

以下是文档参考，不是脚本的联网依赖；脚本运行本身不下载任何内容。

```text
systemd hostnamectl / hostname:
https://www.freedesktop.org/software/systemd/man/252/hostnamectl.html
https://www.freedesktop.org/software/systemd/man/hostname.html

NetworkManager nmcli / settings:
https://networkmanager.dev/docs/api/latest/nmcli.html
https://networkmanager.dev/docs/api/latest/nm-settings-nmcli.html

Netplan YAML / generate:
https://netplan.readthedocs.io/en/latest/netplan-yaml/
https://netplan.readthedocs.io/en/stable-0.106/netplan-generate/

systemd network file matching and DHCP options:
https://www.freedesktop.org/software/systemd/man/latest/systemd.network.html

Debian ifupdown interfaces syntax / allow-hotplug:
https://manpages.debian.org/bullseye/ifupdown/interfaces.5.en.html
```
