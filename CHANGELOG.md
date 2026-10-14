# 开发记录

## 0.3.0-alpha.3 — 2026-10-14

- CentOS 7.9 任务额外生成受清单管理的 iVentoy 注入包，通过 dracut 的 `cmdline.d` 传入 Kickstart 路径；管理页面显示两份文件的绑定路径。
- 修正 iVentoy 提前加载网卡驱动时的 CentOS 7 网络配置：避免把 udev 改名前的 `ethN` 写成启动等待目标，安装器网络按 MAC 绑定。
- 已用官方 DVD 的原始 `parse-kickstart` 在隔离环境复现旧配置的 `eth1` 等待条件，并验证新配置不产生该条件、保留 MAC / DHCP / 自动连接设置。
- 物理机已确认 Kickstart 被读取，完整安装、磁盘启动和登录仍在验收中；不将解析器通过视为实装通过。

## 0.3.0-alpha.2 — 2026-10-13

- 修复 CentOS 7.9 安装环境缺少 `base64`、旧版 `lsblk` 不能读取 VirtIO 磁盘序列号导致的安装前中止。
- 按 CentOS 7.9 DVD 的软件包清单使用 `minimal` 环境，避免安装器因错误的软件包组等待人工应答。
- 默认管理员改为 `deployadmin`，拒绝 Linux 内置 `operator` 账户；首次启动核对普通用户 UID 与登录 shell，再报告安装完成。
- 识别官方 `CentOS-7-x86_64-DVD-2009.iso` / Everything 文件名，拒绝不适用于当前模板的 Minimal、NetInstall 和 Live 镜像。
- 增加旧版 / 新版磁盘标识、重复序列号、错误版本、固件、USB、RAID 和授权拒绝的隔离回归测试。
- 记录用户当前的 18 个目标版本，Ubuntu 限 Server / live-server；CentOS 7.9 优先，其余逐 ISO 验收。
- CentOS 7.9 官方 DVD 已在独立 UEFI 虚拟机中通过无人值守安装、无 ISO 磁盘启动、SSH / sudo 登录、完成回报和凭据重放拒绝；真实硬件完整 PXE 自动安装仍待验收。

## 0.2.0-alpha.1 — 2026-10-13

- 调整为 Linux 直推装机优先；新任务仅安装系统，sckocstp / fyhocsys 接入后置，不改其代码。
- 增加系统 profile：CentOS 7.9、Rocky 8/9/10（含 10.2）、Ubuntu 20.04/22.04/24.04/26.04 LTS、Debian 11/12/13；Windows 保留次要支持。
- 新增 Debian preseed、安装器 HTTPS 授权与失败阻塞钩子；Linux 首次启动核对系统版本。
- 镜像目录树、文件名版本建议、MAC 搜索、启动任务表、系统版本页。
- 新增版本匹配、安装状态流转及 Debian 写盘前阻断测试；实机 PXE 验收仍待完成。

## 0.1.0-alpha.1 — 2026-10-13

- 建立独立 Go 管理端和中文部署工作台。
- 实现机器与安装任务、磁盘确认、单次授权、持久状态和受限安装回调。
- 提供 Rocky 9、Ubuntu Server 24.04 与 Windows 安装包生成。
- 提供 Linux fyhocsys 连接文件导入与节点包校验流程。
- 增加 Rocky 优先的 systemd 部署、Windows 安装脚本、跨平台构建和 Linux 启动矩阵。
- 当前交付为 alpha：iVentoy 规则人工配置，Windows fyhocsys 节点未实现，真实机器 PXE / 装机 / 中心心跳验收待完成。
