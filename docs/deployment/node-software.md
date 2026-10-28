# 安装后从中心获取软件

从 alpha.29 开始，新版 `ventoy-linux-setup.run` 可以从中心服务器获取 RPM / DEB，在已安装并启动的 Linux 节点上安装或升级。不重新装系统，不改磁盘；软件模式不重设账户、主机名或网络。

## 服务器目录

本次部署使用用户指定的 `/home/ventoy-center/iventoy/tools/`。管理端 `config.json` 增加：

```json
{
  "software_dir": "/home/ventoy-center/iventoy/tools",
  "software_rpm_min_el": {"fyhocsys-node": 8}
}
```

通过 SFTP 上传 RPM / DEB 到此目录。保留标准文件名，如 `name_version_amd64.deb`、`name-version-release.el9.x86_64.rpm`。先上传成 `.part`，完成后原子改名为 `.deb` / `.rpm`；不要一边下载一边覆盖正在发布的文件。文件应为普通文件，服务账户有目录读取/遍历权限及文件读取权限。服务只读此目录。

打开管理端“软件分发”，查看包清单并下载新版 setup。替换、新增或移除包后无需重启中心、iVentoy，也无需重新下载 setup；节点下一次执行时读取最新清单。

tools 根目录中的压缩包、独立工具和其他普通文件原样保存到目标机的 `/root/ventoy-tools/`，只传输及校验 SHA-256，不解压、编译或执行。例如 `ipmitool-IPMITOOL_1_8_19.tar.gz`、`onload-9.2.1.70.tgz`、`exanic-software.zip`、`centos_opt_static` 和 `ubuntu-optimize.sh.x` 均保留原文件名。不同名称或版本的文件都会传输，用户按系统和硬件选择后手动使用；文件以 root 私有权限 `0600` 保存，执行独立工具前按需赋予执行权限。RPM / DEB 仍按系统匹配并自动安装，原样分发的文件不会被当成已安装软件。

普通文件分发需要 alpha.37 或更新的管理端和 setup。全局预置包更新后，新装节点直接运行预置 `.run` 即可；已有节点需更新一次 `.run`，后续 tools 内容变化自动读取最新清单。`copy_files` 为新增字段，旧客户端继续处理已有软件包和压缩包，不会因独立工具加入而报格式错误。文件名使用 ASCII 字母或数字开头，其余可包含字母、数字、点、下划线、加号、连字符和波浪号，总长不超过 248。仅发布根目录的普通文件；隐藏文件、符号链接、子目录、`.part/.partial/.tmp/.download/.crdownload/.swp/.bak` 和 `~` 临时后缀，以及 `ventoy-linux-setup.run` 本身及其附属文件不作为原样文件分发。此目录专用于向节点分发软件和工具。

未变化的压缩包校验后跳过下载；中心替换同名文件后，下一次运行原子替换节点上由 setup 管理的旧文件。节点文件被用户改动或同名文件不属于 setup 时保留并报错。中心删除文件不会删除节点已有副本。压缩包与软件包合计最多 256 个文件、8 GiB，单文件最多 4 GiB；目录中未完成的 `.part` 文件不发布。`--list-software` 列出两类文件但不传输，`--check-software` 同时回读软件版本与已分发压缩包摘要。

## 节点执行

alpha.31 的预置流程在普通手动安装和 PXE 自动安装结束时，将新版文件放在 `/root/ventoy-linux-setup.run`，无需另行下载。安装器只复制并校验文件，不执行软件安装。已安装的旧系统不会被远程改写，可从管理端下载新版后替换一次。

```bash
# 无参数：完整系统配置 + 匹配的软件安装 / 升级 + 自动检查
sudo /root/ventoy-linux-setup.run

# 仅安装软件；不重设账户、主机名、网络
sudo ./ventoy-linux-setup.run --software

# 查看当前候选包；不安装
sudo bash ventoy-linux-setup.run --list-software

# 检查上次成功软件组是否仍已安装到要求版本或更新版本
sudo bash ventoy-linux-setup.run --check-software

# 完整配置并指定主机名；--apply 保留兼容但无需填写
sudo ./ventoy-linux-setup.run --hostname server01
```

`--offline` 不连接软件库。从 alpha.32 起，附加网卡等待 DHCP、驱动等待重启等 `PENDING` 检查不会跳过用户请求的软件安装；已有网络可用时继续下载、安装并验证。软件失败明确报错并保留请求，用户再次运行或配置恢复服务可以重试；成功后清除请求，正常后续重启不再从软件库更新。新增软件、升级软件都由用户主动再次运行 setup 触发。

## FYHOC 每次开机自动解锁（alpha.42）

中心可在私有配置中设置 `fyhoc_unlock_password_file`，指向 tools 目录以外、仅服务账户可读的 `0600` 普通文件。文件只保存解锁口令；口令不写入公开源码、ISO、setup 包或软件清单。节点使用已有软件下载凭证，通过 HTTPS 的独立接口获取；因此持有该凭证的节点也有权获取解锁口令。更新中心文件后，下一次运行 setup 会验证并保存新口令。

匹配的软件包安装并校验成功后，setup 对已安装的 `fyhoc` 执行真实 `fyhoc unlk`，通过关闭回显的专用终端输入口令，不把口令放进命令参数或环境变量。节点把凭据保存为 root 所有的 `/etc/ventoy-center/fyhoc-unlock.key`（`0600`），启用 `ventoy-fyhoc-unlock.service` 并立即检查。每次开机重新认证，不依赖中心联网，不修改或伪造 FYHOC 的临时授权文件。软件升级后再执行 setup 也会重新验证。

`--check-software` 同时检查本次开机的解锁状态和开机服务；口令错误、认证超时、服务失败均返回错误，不报告解锁成功。服务日志仅记录成功/失败，不输出口令或认证终端内容。中心未配置该功能时明确显示 `FYHOC_UNLOCK_SKIPPED`。这只处理 FYHOC 解锁，不替代 fyhocsys-node 的入网注册。

CentOS 7.9 与 Ubuntu Desktop 26.04 隔离系统已分别用真实 `fyhoc 1.1.24-1` RPM / DEB 完成安装、立即解锁和正常重启后的自动解锁；内核分别保持 `3.10.0-1160.el7.x86_64`、`7.0.0-14-generic`。同次动态选择并安装 `fyhocsys-node 0.8.5`，9 个工具文件传输和摘要检查通过。完整范围和发布状态见[进度记录](../development/progress.md)。

## 自动选择与升级

- Ubuntu / Debian 选择本机架构的 DEB；EL 系列选择本机架构的 RPM。`all` / `noarch` 包也可使用。默认根据 RPM 的 EL 大版本匹配；例如 `.el9` 不发给 EL8。
- 名称、版本、Epoch、Release、架构以包内元数据为准。不同发行版之间的依赖兼容性由本机包管理器检查。
- 同一软件存在多个兼容版本时，使用原生 dpkg / RPM 比较选择最高版本。已安装相同或更新版本时跳过，不降级；新旧文件可暂时同时保留。同名、同版本、同架构但内容不同的重复包会停止并提示清理冲突。
- 清单和文件 SHA-256 未变化、已安装版本检查通过时，不重复下载或安装。请正确提高包版本号；原地重打同版本包不视作升级。
- 从服务器移除文件不会卸载节点已安装的软件，也不会设置后台定期软件更新。
- `software_rpm_min_el` 是带 `.elN` 标签 RPM 的向上兼容规则。原 `fyhocsys-node` EL8 构建包允许用于 EL8 及以上，不向 EL7 分发。用户于 2026-10-27 确认新版 `fyhocsys-node-0.8.4-1.x86_64.rpm` 已适配 EL7 及以上；这个通用包的文件名和包内版本均无 `.elN` 标签，现有分发与 setup 检查已允许 EL7 获取和安装，不受旧 EL8 标签规则限制。依赖仍由目标机包管理器检查；此声明不会扩大 `sckoct-fyh` 等其他包的适用范围。

需要专属软件组时，可使用 `系统/版本/架构/default/`：如 `ubuntu/22.04/amd64/default/`、`debian/12/amd64/default/`、`rocky/9/x86_64/default/`。专属组优先于根目录自动组。Ubuntu 精确匹配 `VERSION_ID`；EL / Debian 优先精确版本，其次大版本。不自动借用另一个 Ubuntu 版本的软件组。

## 依赖与结果

先下载并校验整组软件，读取包内信息，再做依赖和安装事务预检查。优先使用已安装的依赖和同时上传的依赖包；缺少依赖时从节点已配置的软件源获取。APT 仅在本次明确执行的安装需要时刷新索引；不会启用 APT 定时刷新或无人值守升级。普通依赖库可能随安装事务更新；不执行全系统 `upgrade` / `distro-sync`，不添加未知软件源或强制忽略依赖。

内核包不能通过软件分发安装；APT 事务中发现内核修改/软件删除会停止，RPM 依赖解析排除 `kernel*`。安装后再次对比内核包集合，并在已有策略时检查内核锁定和禁用自动更新。失败保留错误记录，不把部分完成标记为成功。包的维护脚本以 root 执行，应上传自己信任的软件；脚本失败后的部分改动不能保证自动回滚。

setup 会输出系统配置检查、APT 事务预演、实际安装及复查日志，因此同一清单可能出现两次。`apt update` 下载的是软件源索引；依赖事务中出现 `upgraded` 则确实表示对应依赖包升级，应按包名核对。登录页的 `New release ... available` 仅为发行版升级提示，不能当作已经升级的证据。以实际运行内核、已安装内核包集合和 APT 历史为准。

校验、下载、架构/EL 不匹配、依赖和安装错误返回非零。没有匹配软件时显示 `SKIPPED`。日志：`/var/log/ventoy-software.log`；最近事务：`/var/lib/ventoy-center/software/last-result.json`。完整配置流程中的日志也进入 `/var/log/ventoy-manual-init.log`。

RPM / DNF 即使报告 `%post` 等安装脚本失败，也可能以 0 退出且保留已安装包记录。setup 同时检查这类错误输出，并在 `rpm-scriptlet-failures.json` 保存受影响的包和版本；相同版本再次运行或执行软件检查仍报失败。发布并成功安装修正后的新版本可解除该版本对应的失败状态。无法确定所属包的脚本错误需要人工核查，不能仅凭 `rpm -q` 查到包就认定安装完整成功。

客户端内置中心 HTTPS 地址、公开 CA 和只读下载凭证，不含管理口令或安装任务凭证。管理口令轮换会使既有只读凭证失效，需要重新下载 setup。下载凭证不授予管理、上传、删除或远程执行权限。

依赖解析参考 [APT 官方手册](https://manpages.ubuntu.com/manpages/noble/man8/apt-get.8.html) 与 [DNF 官方命令参考](https://dnf.readthedocs.io/en/latest/command_ref.html)。

## 验证范围

2026-10-27，在 `.235` 普通 ISO 手动安装的 Rocky 10.0、双盘 RAID1 + LVM / XFS 上实际执行预置 setup，并指定 `--hostname s4-2495x1`。主机名持久配置与运行值一致，外部 root/user 密码 SSH、现有 DHCP、内核锁定和自动更新禁用检查通过；运行内核与全部 7 个内核包保持 `6.12.0-55.12.1.el10_0.x86_64`。`fyhoc -V` 返回 1.1.24，`fyhocsys-node --version` 返回 0.8.4，9 个压缩包及独立文件与中心摘要一致。安装依赖更新了 15 个非内核包（包括 OpenSSL、OpenSSH 等），未执行全系统或内核升级。

同次事务发现 `sckoct-fyh-1.1.8-1.el10` 的 `%post` 运行其自带 Python/Cython 模块时发生 `__Pyx_GetCurrentInterpreterId failed`，旧 setup 错误地报告软件组成功。修复后的检测已通过隔离 RPM/DNF 的失败脚本、重复运行、修正版升级测试，并在 `.235` 根据原日志恢复真实失败记录，重新运行明确返回失败；这不是该软件包本身已经修复。原生事务回归使用独立 Rocky 9 测试根；Rocky 10 实机验证使用本次实际错误日志及已安装包状态。该修复纳入 alpha.40，中心下载与全部 Linux 手动/PXE 预置包使用同一软件检查实现。

2026-10-25，用户在 `.235` 新装 Ubuntu 22.04.3 后无参数运行 setup。现场只读核对确认 `fyhoc 1.1.24-1`、`fyhocsys-node 0.8.3-1`、`sckoct-fyh 1:1.1.8-1` 均已安装；为补齐依赖，APT 新装 ipmitool/libatomic1 等包，并将 `gcc-12-base`、`libgcc-s1`、`libstdc++6` 从 `12.3.0-1ubuntu1~22.04` 升到 `12.3.0-1ubuntu1~22.04.3`。未执行全系统升级，运行内核及唯一已安装内核仍为 `5.15.0-78-generic`（包版本 `5.15.0-78.85`），hold 保留，五个 APT/无人值守更新单元全部 masked/inactive，APT 周期配置均为 0。软件请求完成；附加网卡 `enp188s0f1` 的 IPv4 DHCP 租约仍待获取，因此系统总状态是 `PENDING`，不等于软件安装失败或内核已更新。

alpha.32 在 Ubuntu 24.04.3 双盘 RAID1/LVM 实机上直接无参数运行 setup，安装并检查 `fyhoc 1.1.24-1`、`fyhocsys-node 0.8.2-1`、`sckoct-fyh 1:1.1.8-1`；`fyhoc -V` 与依赖 `ipmitool -V` 实际执行成功。`ipmitool-IPMITOOL_1_8_19.tar.gz` 原样传到 `/root/ventoy-tools/`，与中心摘要一致且未解压。重复运行不再下载软件包或压缩包；SSH、DHCP、内核锁定及禁用自动更新复查通过，内核保持 `6.8.0-71-generic`。所有 54 个普通 Linux 入口及 52 个 PXE 入口已更新预置包并核对摘要；此处不代表全部发行版都重新做过实机安装。

alpha.29 在 Ubuntu 24.04.3 隔离虚拟机中实际安装了 `fyhoc 1.1.24-1`、`fyhocsys-node 0.8.2-1`、`sckoct-fyh 1:1.1.8-1`，验证缺失依赖获取、第二次运行跳过、已安装版本回读及原有内核/更新策略。RPM 在隔离的 Rocky 9 根文件系统中使用真实 RPM / DNF 验证整组事务、仓库补齐依赖、原生版本排序、升级、不降级及失败时不进入安装事务。

这些结果验证分发机制；不代表所有软件在全部发行版或真实硬件上的运行功能均已验收。软件自身的接入配置和硬件功能仍按其要求执行。
