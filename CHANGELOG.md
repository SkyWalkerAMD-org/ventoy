# 开发记录

## 0.3.0-alpha.30 — 2026-10-25

- 将新版 setup 与软件分发客户端加入 Linux 普通手动安装和 PXE 自动安装的预置包；安装后直接运行 `/root/ventoy-linux-setup.run`。
- 自动安装完成前显式复制并校验预置文件，注入链保留启动修复与 E610 驱动钩子。缺少完整离线包时拒绝生成不完整入口。
- 仅刷新带完整校验记录且尚未应用的旧托管 setup，保留旧文件备份；安装器不执行 setup 或安装中心软件。

## 0.3.0-alpha.29 — 2026-10-25

- 新增中心软件目录、只读分发接口和“软件分发”页面；节点运行新版 `ventoy-linux-setup.run`，按 DEB/RPM、架构、EL 版本自动选择默认软件。
- 支持包内版本比较、同一软件择新、已安装版本跳过、不降级、未变化软件免重复传输。中心替换软件包无需重新生成 setup。
- 缺依赖时由本机已配软件源解析；检查 SHA-256、包元数据、安装结果及现有内核/更新策略。只执行软件安装事务，不进行全系统升级。
- 提供按发行版配置专属组以及明确的 RPM 跨 EL 版本兼容规则。`--apply` 完整配置包含默认软件，`--offline` 不下载；旧预置 setup 需替换为新版一次。

## 0.3.0-alpha.7 — 2026-10-14

- Ubuntu 自动安装显式使用官方 HTTPS 主仓库和安全仓库，关闭 GeoIP 自动替换镜像站，修复 22.04.3 实机因镜像站 HTTP 403 无法安装 EFI 引导包的问题。
- Ubuntu Server 22.04.3 已完成 Intel X710 / UEFI 实装、硬盘启动、控制台 / SSH / sudo 登录、网络与首次启动回报；无失败服务，保留盘分区表及签名一致，旧授权返回 HTTP 401。
- 此流程仍需一次安装 `yes` 确认；测试机由现场调整为硬盘优先启动，尚未实现完整无人值守。实际系统标识为 22.04.3，在线更新后内核为 `5.15.0-198-generic`。

## 0.3.0-alpha.6 — 2026-10-14

- 修正 Ubuntu 自动安装的磁盘匹配：`lsblk SERIAL` 与 Subiquity 使用的 udev `ID_SERIAL` 不同，旧配置可能没有匹配磁盘并报 `NoneType ... id`。
- 在 MAC、唯一短序列号、设备类型、存储和固件检查通过后，将确认的磁盘路径原子写入 `/autoinstall.yaml`；初始路径不可安装，写入失败时不领取安装授权。
- 增加 Ubuntu 绑定及失败顺序回归测试；在 22.04.3 原始 Subiquity 5004 上复现旧匹配失败并验证新路径准确命中目标盘。后续完整安装结果见 alpha.7。

## 0.3.0-alpha.5 — 2026-10-14（待实机复测）

- 将 CentOS 7 已验证的分离 hostname / MAC 绑定网络配置扩展到 Rocky 8，避免 initramfs 等待 udev 改名前的 `ethN`。
- Rocky 8.10 原始安装器解析器的隔离复现通过：旧配置生成 `ip=eth1:dhcp:` / `bootdev=eth1` 和网络等待标记；新配置不生成这些条件，保留 MAC、DHCP 和自动连接。此修复尚未完成物理机回归。

## 0.3.0-alpha.4 — 2026-10-14

- Rocky 8 任务增加 `rocky-iventoy.tar.gz` 启动注入包，补齐 iVentoy 1.0.44 的 Kickstart 入口，并纳入准备、校验和清理清单。
- Rocky 8.10 DVD 已完成 Intel X710 / UEFI 实机自动安装、硬盘启动、控制台 / SSH / sudo 登录和首次启动回报；保留盘分区信息一致，完成任务重放返回 HTTP 401。
- 本次安装存在约 10 分钟 initramfs 网络等待，日志定位到已改名的 `eth1`；功能验收通过，启动等待问题待 alpha.5 实机复测。

## 0.3.0-alpha.3 — 2026-10-14

- CentOS 7.9 任务额外生成受清单管理的 iVentoy 注入包，通过 dracut 的 `cmdline.d` 传入 Kickstart 路径；管理页面显示两份文件的绑定路径。
- 修正 iVentoy 提前加载网卡驱动时的 CentOS 7 网络配置：避免把 udev 改名前的 `ethN` 写成启动等待目标，安装器网络按 MAC 绑定。
- 已用官方 DVD 的原始 `parse-kickstart` 在隔离环境复现旧配置的 `eth1` 等待条件，并验证新配置不产生该条件、保留 MAC / DHCP / 自动连接设置。
- CentOS 7.9 官方 DVD 已通过 Intel X710 / UEFI / iVentoy 1.0.44 的物理机自动安装、目标盘启动、控制台 / SSH / sudo 登录及首次启动回报；另一块盘原有分区保留，已完成任务的授权重放返回 HTTP 401。
- 本轮先在已授权测试机上离线解除旧 RAID 并仅准备目标盘；模板仍拦截现有 RAID / LVM，不把单盘实装结果扩展为多盘存储或其他启动模式已验收。

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
