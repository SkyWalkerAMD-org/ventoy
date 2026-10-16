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

在管理端的“镜像管理”页面点击“下载手动安装后的配置脚本”。安装完毕、启动进入新系统后执行：

```bash
sudo sh linux-update-policy.sh --apply
sudo sh linux-update-policy.sh --check
```

脚本默认只检查，必须带 `--apply` 才修改配置。它锁定执行时已安装的内核，不会把手动安装过程中已更新的内核降回 ISO 内核。若需要保持 ISO 原内核，请在手动安装器中跳过在线更新；要由部署系统强制保证，应使用新生成的专属自动安装任务。

脚本不会删除软件源、内核或用户数据。手动执行 `apt update` 和安装非内核软件仍可用；以后更新内核或安装系统安全补丁需要管理员安排维护。普通 ISO 不会因为加入 iVentoy 目录就自动执行此脚本。

## 检查与维护

自动安装后的脚本路径为 `/usr/local/sbin/ventoy-center-update-policy`，可用 `sudo sh /usr/local/sbin/ventoy-center-update-policy --check` 检查。重复应用不会改变锁定基线；发现内核清单发生变化时会停止，要求先核对变更。

恢复更新前保留 `/var/lib/ventoy-center/kernel-policy`：

1. Debian / Ubuntu：根据 `kernel-packages.tsv` 和 `holds-before.txt`，仅解除本策略新增的 hold；移除 `/etc/apt/preferences.d/00-ventoy-center-kernel` 和 `/etc/apt/apt.conf.d/99-ventoy-center-no-auto-updates`。Ubuntu 自动安装还可能保留 Curtin 生成的 `90curtin` 配置与 `90curtin.pref`，需核对并移除其中对应的内核 pin / 升级排除项，保留其他配置。
2. CentOS / Rocky：从 yum / dnf 的 `[main]` 排除项中删除本策略加入的 `kernel*`，保留其他项。首次应用前的配置备份位于上述状态目录，以 `_etc_` 开头；不要直接用旧备份覆盖之后的其他维护改动。
3. 对照前述服务清单解除 mask，再按实际需要启用更新 timer / service。不要将所有列出的互斥 dnf-automatic 变体同时启用。

## 验证范围

本次对 Ubuntu 22.04.3 原始 ISO 的分层文件系统进行隔离测试：应用、再次应用、检查、持久服务屏蔽、原内核候选版本固定、新 ABI 安装拒绝和非内核包候选保留均通过。内核为 `5.15.0-78-generic`。生成模板另有 28 组解析检查和 Ubuntu 安装前内核绑定的异常输入回归。

Rocky 9.8 用户空间也在独立的临时根目录中验证了应用、检查、重复执行及服务屏蔽；真实 DNF 解析确认 `kernel*` 和两项原有排除规则同时保留，yum.conf 到 dnf.conf 的符号链接保持不变。未修改运行中的部署服务器更新配置。CI 增加 Ubuntu 22.04、Debian 12 和 Rocky 8 的隔离包管理回归，使用无文件的内核元数据测试包，不宣称容器中完成了系统安装。

此前四个版本的无人值守实装通过记录早于本策略。此功能需要随下一轮实机任务验证，不能把隔离测试等同于各版本新的完整装机验收。

实现依据：[Subiquity 配置参考](https://canonical-subiquity.readthedocs-hosted.com/en/latest/reference/autoinstall-reference.html)、[Curtin APT 配置](https://curtin.readthedocs.io/en/latest/topics/apt_source.html)、[APT hold](https://manpages.ubuntu.com/manpages/jammy/man8/apt-mark.8.html)。
