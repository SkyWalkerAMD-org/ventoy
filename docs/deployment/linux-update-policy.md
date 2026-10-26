# Linux 内核与自动更新策略

从 `0.3.0-alpha.11` 新生成的 Linux 安装任务默认启用本策略。已准备的旧任务文件保持原内容；要采用新策略，请在客户端停止安装后取消旧任务并创建新任务。管理端升级不会替现有系统执行本脚本，也不会修改 fyhocsys / sckocstp。

## 自动安装

| 系统 | 初次内核来源 | 安装后的策略 |
| --- | --- | --- |
| CentOS 7.9 / 8.5 / Stream 9、10；Rocky 8、9、10 | 所选完整 DVD 的软件包 | 在 yum / dnf 主配置中合并 `kernel*` 排除规则，保留已有排除项；关闭并屏蔽 yum-cron、dnf-automatic 各变体及 dnf-makecache |
| Ubuntu Server 20.04 / 22.04 / 24.04 / 26.04 | 本次 live-server 实际启动的内核 | 安装前指定版本化内核包，按镜像已安装包版本生成 APT pin；安装后持久固定内核和相关元包，并屏蔽自动更新服务 |
| Debian 11 / 12 / 13 | 安装器使用的光盘或所配置镜像站 | 保持 `pkgsel/upgrade=none`，安装结束时固定实际安装的内核；netinst 不能据 ISO 文件名保证使用历史内核 |

Ubuntu 的 early-commands 在领取清盘授权前核对 live 系统内核元数据；如果无法确定对应软件包就停止，不自动换成最新内核。Subiquity 的 `updates` 只接受 `security` / `all`，因此使用 APT 的无人值守升级排除配置阻止其安装期间的自动升级，不生成无效的 `updates: none`。安装所需的软件包与依赖仍可从配置的镜像站取得，不能把此策略理解为完全离线安装。

Debian / Ubuntu 的持久设置包括：

- `APT::Periodic` 的索引刷新、预下载、自动清理、无人值守升级全部设为 0。
- 禁用并 mask `apt-daily`、`apt-daily-upgrade` 的 service / timer，以及 `unattended-upgrades.service`。
- 对已安装的内核、模块、头文件和常见内核元包执行 hold，同时写入精确版本 pin 和新 ABI 排除规则。只 hold 旧内核包不足以阻止安装新名称的内核包。

首次硬盘启动会再次检查策略；Ubuntu 还核对正在运行的内核与安装镜像启动内核一致，通过后才回报安装完成。所有系统的锁定清单保存在 `/var/lib/ventoy-center/kernel-policy/kernel-packages.tsv`，不使用安装器的 `uname` 代替目标系统的软件包清单。

## 普通 ISO 手动安装

alpha.34 修复普通入口在运行 setup 前缺少更新保护的问题。更新注入绑定后，Ubuntu Server 使用原生 Subiquity 交互配置：保留语言、键盘、网络、软件源、存储、账户、SSH 等手动页面，不提供磁盘或账户答案；默认跳过安装器自身更新，安装前固定实际启动的介质内核，并用 APT 排除配置阻止安装收尾的无人值守包升级。不能使用 `interactive-sections: ['*']`，因为它会重新启用交互式安装器更新检查。

Debian d-i 在基础系统和内核安装完成后、进入 pkgsel 前固定内核并设置 `pkgsel/upgrade=none`。Ubuntu/Debian 的完成钩子在首次启动前应用并检查持久 APT 策略。完整 setup 仍只预置、不自动执行，手动安装创建的账户、密码、网络和分区保持用户选择。Debian netinst 的首次内核仍来自软件源，不能据 ISO 名称保证历史版本。Ubuntu Desktop 当前仅在收尾应用持久策略；本次 Server 安装前保护不能扩展为 Desktop 全流程验收。

这些设置阻止自动升级和周期刷新，不禁止安装器为用户勾选的 SSH 等组件下载软件与依赖，也不禁止手动 `apt update`。更新索引、安装依赖和升级 Linux 内核是不同操作；应以包日志和内核基线确认，不能仅凭安装器显示“updating packages”判断。

alpha.23 可在“镜像管理”下载 [Linux 手动安装完整配置包](manual-linux.md)，一次应用 SSH、主机名、DHCP、驱动、内核与更新策略，并自动检查。所有支持的 Linux 家族均使用此入口。

若只需要内核锁定和禁用自动更新，点击“仅下载内核与更新策略脚本”。安装完毕、启动进入新系统后执行：

```bash
sudo sh linux-update-policy.sh --apply
sudo sh linux-update-policy.sh --check
```

独立下载的策略脚本默认只检查，必须带 `--apply` 才修改配置。它锁定执行时已安装的内核，不会把以前手动安装过程中已更新的内核降回 ISO 内核。未绑定 alpha.34 手动注入包的旧入口仍需在安装器中跳过在线更新；安装后才运行 setup 无法追回安装阶段的变化。

脚本不会删除软件源、内核或用户数据。手动执行 `apt update` 和安装非内核软件仍可用；以后更新内核或安装系统安全补丁需要管理员安排维护。普通 ISO 不会因为加入 iVentoy 目录就自动执行此脚本。

## 检查与维护

自动安装后的脚本路径为 `/usr/local/sbin/ventoy-center-update-policy`，可用 `sudo sh /usr/local/sbin/ventoy-center-update-policy --check` 检查。重复应用不会改变锁定基线；发现内核清单发生变化时会停止，要求先核对变更。

恢复更新前保留 `/var/lib/ventoy-center/kernel-policy`：

1. Debian / Ubuntu：根据 `kernel-packages.tsv` 和 `holds-before.txt`，仅解除本策略新增的 hold；移除 `/etc/apt/preferences.d/00-ventoy-center-kernel` 和 `/etc/apt/apt.conf.d/99-ventoy-center-no-auto-updates`。Ubuntu 自动安装还可能保留 Curtin 生成的 `90curtin` 配置与 `90curtin.pref`，需核对并移除其中对应的内核 pin / 升级排除项，保留其他配置。
2. CentOS / Rocky：从 yum / dnf 的 `[main]` 排除项中删除本策略加入的 `kernel*`，保留其他项。首次应用前的配置备份位于上述状态目录，以 `_etc_` 开头；不要直接用旧备份覆盖之后的其他维护改动。
3. 对照前述服务清单解除 mask，再按实际需要启用更新 timer / service。不要将所有列出的互斥 dnf-automatic 变体同时启用。

## 验证范围

2026-10-26，alpha.34 补充普通手动路径的安装阶段验证：Ubuntu Server 22.04.3 / 24.04.3 在线手动安装并正常重启后，分别保持 `5.15.0-78-generic` / `6.8.0-71-generic`；无人值守升级的 dpkg 日志为空。Debian DVD 12.15.0 验证原生安装钩子，硬盘启动为 `6.1.0-50-amd64`。三者均在完整 setup 尚未运行时通过持久策略检查。测试方式与发布状态见[当前进度](../development/progress.md#alpha34-普通手动安装的更新保护2026-10-26)。

2026-10-23，alpha.20 新增以下原始 ISO 的隔离 UEFI 自动安装与两次硬盘启动验收。每次均执行目标系统的策略检查，核对内核包 hold / pin、APT 周期选项为 0，以及五个 APT / unattended-upgrades 单元 masked / inactive；管理员 `admin` 登录和 sudo、后台完成回报同时通过。

| ISO 版本 | 两次启动确认的内核 |
| --- | --- |
| Ubuntu Server 22.04.3 | `5.15.0-78-generic` |
| Ubuntu Server 22.04.4 | `5.15.0-94-generic` |
| Ubuntu Server 22.04.5 | `5.15.0-119-generic` |
| Ubuntu Server 24.04.3 | `6.8.0-71-generic` |
| Ubuntu Server 24.04.4 | `6.8.0-100-generic` |
| Ubuntu Server 24.04.5 | `6.8.0-139-generic` |
| Debian DVD 12.8.0 | `6.1.0-27-amd64` |
| Debian DVD 12.15.0 | `6.1.0-50-amd64` |
| Debian DVD 13.0.0 | `6.12.38+deb13-amd64` |

这些结果来自新建 QEMU/KVM 测试盘，未重装 `.39` 或 `.235`。适用范围是上述 amd64 安装介质与生成的自动安装脚本，不扩展为所有硬件、Desktop、netinst 或其他 Debian / Ubuntu 版本均已实测。

alpha.20 修复安装器挂载标记的识别：Ubuntu 24.04 在目标根目录保留 `/cdrom` 时，根据目标文件系统判断；Anaconda 8 在目标 chroot 绑定 `/run/anaconda` 时，必须同时指定离线应用且确认当前根目录与安装器 PID 1 的根目录不同。直接在 live 安装环境应用策略仍被拒绝。启动后的检查也会核对更新单元是否仍在运行，而非仅查看持久 mask 文件。

本次对 Ubuntu 22.04.3 原始 ISO 的分层文件系统进行隔离测试：应用、再次应用、检查、持久服务屏蔽、原内核候选版本固定、新 ABI 安装拒绝和非内核包候选保留均通过。内核为 `5.15.0-78-generic`。生成模板另有 28 组解析检查和 Ubuntu 安装前内核绑定的异常输入回归。

Rocky 9.8 用户空间也在独立的临时根目录中验证了应用、检查、重复执行及服务屏蔽；真实 DNF 解析确认 `kernel*` 和两项原有排除规则同时保留，yum.conf 到 dnf.conf 的符号链接保持不变。未修改运行中的部署服务器更新配置。CI 增加 Ubuntu 22.04、Debian 12 和 Rocky 8 的隔离包管理回归，使用无文件的内核元数据测试包，不宣称容器中完成了系统安装。

此前四个版本的初始无人值守实装记录早于本策略。2026-10-16 已新增 CentOS 7.9 / E610 的 alpha.11 完整实装：安装后首次启动检查通过，内核保持 `3.10.0-1160.el7.x86_64`，实际 yum 读出的排除项包含 `kernel*`，yum-cron 及 dnf 自动更新单元被 mask，随后才回报 `installed`。详见 [CentOS 7.9 实机记录](centos7.md#intel-e610-实机适配2026-10-16)。

2026-10-19 新增 Ubuntu Server 22.04.3 的 alpha.13 通用 PXE 完整实装：安装配置固定 `linux-image-5.15.0-78-generic`，两次硬盘启动均保持 `5.15.0-78-generic`；已安装 image / modules 包为 `5.15.0-78.85`，均处于 hold 状态。APT 周期刷新、下载与无人值守升级选项为 `0`；`apt-daily`、`apt-daily-upgrade` 的 service / timer 及 `unattended-upgrades.service` 均为 masked / inactive，策略检查返回 0。详见 [Ubuntu 实机记录](ubuntu22.md#alpha13-通用-pxe-菜单验收2026-10-19)。其他版本仍需新的实机任务验收，不能把隔离测试等同于各版本新的完整装机验收。

同日该机器手动重装后，又在已安装系统中单独应用本脚本；应用及检查通过，正常重启后检查再次通过，实际 yum 排除项仍为 `kernel*`，内核保持 1160。本轮同时完成 E610 手动驱动恢复，但普通 ISO 本身仍不会自动执行这两个安装后步骤。

实现依据：[Subiquity 配置参考](https://canonical-subiquity.readthedocs-hosted.com/en/latest/reference/autoinstall-reference.html)、[Curtin APT 配置](https://curtin.readthedocs.io/en/latest/topics/apt_source.html)、[APT hold](https://manpages.ubuntu.com/manpages/jammy/man8/apt-mark.8.html)。
