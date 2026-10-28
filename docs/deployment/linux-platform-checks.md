# Linux 手动与自动安装的平台检查

## 2026-10-28 检查范围

现场 52 个不重复 Linux ISO 继续共用手动、自动两种脚本分支。已在独立目录重新生成全部入口，核对每个文件的摘要，以及注入包中同一份 setup 和 Ubuntu 退出介质钩子；没有生成第二套 ISO，也没有更换现场 ISO 内容。完整模板、账户、RAID、容量、文件系统、共享菜单、手动预置、软件分发、更新策略及退出介质回归通过。

| 验证层次 | 结果与边界 |
| --- | --- |
| 52 个 Linux 共享入口 | 生成、文件摘要、预置 setup 一致性均通过；不是 52 次新物理安装 |
| CPU 共用逻辑 | 未发现把系统安装限定为 Intel 的判断，也没有按 CPU 厂商盲目关闭 IOMMU、ACPI 或其他功能 |
| E610 检测 | PCI 厂商 `0x8086` 用于识别 Intel 网卡，不要求主机 CPU 为 Intel |
| CentOS 7.9 自动安装 | 独立 UEFI 虚拟机完成安装、硬盘启动、账户/策略检查及 RPM 安装；FYHOC 正常重启后自动解锁 |
| Ubuntu Desktop 26.04 自动安装 | 独立 UEFI 虚拟机完成安装、两次硬盘启动、分区/账户/策略检查及 DEB 安装；FYHOC 正常重启后自动解锁 |
| AMD CPU 补充测试 | CentOS 7.9 测试磁盘的独立覆盖层在 QEMU TCG `EPYC-Turin-v1` 下到达登录页，实际登录读取 `AuthenticAMD`、DHCP 地址和运行服务；模拟器报告部分 CPU 特性不支持，不代表 Zen 5 全功能或 AMD 主板实机验证 |

现场现有镜像/驱动与历史验收范围另见 [Linux 目标](linux-targets.md) 和 [当前进度](../development/progress.md)。本轮没有物理 E610 网卡，也没有增加真实 RAID 拔盘故障测试。

## 版本与硬件兼容的边界

`amd64` / `x86_64` 并不是只支持某一 CPU 厂商。新发行版也可能有最低指令集要求，例如 [RHEL 10 官方架构说明](https://docs.redhat.com/en/documentation/red_hat_enterprise_linux/10/html/considerations_in_adopting_rhel_10/architectures) 将 AMD/Intel 64 的最低级别列为 x86-64-v3。[Ubuntu 架构说明](https://ubuntu.com/project/docs/how-ubuntu-is-made/concepts/supported-architectures/) 另外区分 `amd64` 与 `amd64v3`，不能把一项发行版要求泛化到所有镜像。

[AMD 9995WX 产品页](https://www.amd.com/en/products/processors/workstations/ryzen-threadripper/9000-wx-series/amd-ryzen-threadripper-pro-9995wx.html) 列出 RHEL/Ubuntu x86-64 支持，不能据此宣布 CentOS 7.9 已通过该平台认证。CPU 指令集检查也不能替代网卡、存储控制器、固件和原生安装日志的检查。

## `Pane is dead` 的现场线索

10980XE（带外 `192.168.0.249`）截图显示 CentOS 7 在 `%pre` 脚本失败后，错误处理又因缺少 `updates_disk_hook` 退出。用户确认 9995WX 曾出现同样的可见错误；两者的原始 `%pre` 错误仍需日志才能区分。`Pane is dead` 只是 Anaconda 进程退出后的终端状态，不是 CPU 不兼容的诊断。

用户已更正截图归属：2495X（带外 `.235`）可以进入 CentOS 7.9 文件系统选择页，不能把 `.249` 的报错记为 `.235` 的升级回归。9995WX（带外 `.54`）已进入系统，按用户要求不连接或操作该机。

排查应先保留 `/tmp/ventoy-center-pre.log`、`/tmp/ks-script-*.log`、Anaconda 日志及当前启动参数，核对选择的手动/自动分支，再检查 TLS、网络、磁盘保护或脚本依赖。不能为消除这一提示而跳过选盘保护或强制继续写盘。

私有证据目录：`/home/ventoy-center/e610-pxe-media-20261023/ubuntu26-reboot42-20261027/`。其中 `all52-entry-audit.json` 为去凭据的入口核验，`full-matrix.log` 为完整回归；实际安装证据在 `new-systems-qa/`，AMD 补充证据在 `amd-tcg-c7/console-acceptance.txt`。
