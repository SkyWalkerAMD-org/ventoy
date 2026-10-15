# Rocky Linux 8.10 实机验收

2026-10-14，`rocky-8` 在 iVentoy 1.0.44 / Standard / ProxyNet 下完成 UEFI 物理机自动安装。2026-10-15 使用 alpha.7 再次实装，启动阶段的网络等待修复回归通过；不要将此记录扩展为全部 Rocky 8 镜像、Legacy BIOS 或多盘存储已通过。

## 镜像和环境

- 用户现有 `Rocky-8.10-x86_64-dvd.iso`：14,213,840,896 字节。
- SHA-256：`642ada8a49dbeca8cca6543b31196019ee3d649a0163b5db0e646c7409364eeb`。
- 内部 `.treeinfo` 确认为 Rocky 8.10 / x86_64；嵌入式介质校验通过。该历史镜像不同于目前下载站的重新构建镜像，不将当前发布的校验值当作它的验证依据。
- Xeon w7-2495X、64 GiB 内存、Intel X710 网卡、UEFI；安装到按序列号指定的一块 SATA SSD，另一块 SSD 保留。
- 控制器 `0.3.0-alpha.4`；Kickstart 与 `rocky-iventoy.tar.gz` 同时绑定到专属任务镜像。

## 验收结果

| 检查 | 结果 |
| --- | --- |
| PXE、镜像加载、Kickstart 执行 | 通过 |
| 自动分区和安装，无安装器人工应答 | 通过；启动网络等待见下文 |
| 从目标盘 UEFI 启动 | 通过，Rocky 8.10 / `4.18.0-553.el8_10.x86_64` |
| 普通管理员控制台密码登录、SSH 公钥、sudo | 通过 |
| DHCP、默认路由、sshd | 正常 |
| SELinux / root 账户 / 失败服务 | Enforcing / 锁定 / 0 个 |
| 首次启动完成回报 | `installed`，回调服务成功，临时脚本已移除 |
| 完成任务的旧授权重放 | HTTP 401 |
| 保留盘的分区和签名信息 | 与安装前一致，未挂载 |

目标盘最终为 600 MiB EFI、1 GiB `/boot`、其余空间为 LVM；其中根文件系统 70 GiB、swap 31.3 GiB、`/home` 344.3 GiB。保留盘上的旧 RAID 成员仍存在，系统枚举为 inactive 的单成员 MD 设备。当前模板仍要求对目标盘已有的 LVM / RAID 先进行人工准备，本轮在获授权后离线处理目标盘。

## 启动等待问题

本次 initramfs 耗时约 10 分 14 秒。原始日志显示 NetworkManager 反复等待名为 `eth1` 的旧网络配置；iVentoy 提前加载驱动后，网卡随后被 udev 改名，导致等待超时。安装器最终继续安装成功。

`0.3.0-alpha.5` 将 Rocky 8 的 hostname 与 DHCP 网络拆为两条 Kickstart 指令，并使用 `--bindto=mac`。使用该 ISO 原始 initrd 的 `/usr/sbin/parse-kickstart`，在无外网、无真实磁盘的隔离环境中，以虚拟 `eth1` sysfs 输入复现结果：

| 配置 | dracut 参数 | `net.ifaces` 等待标记 | ifcfg |
| --- | --- | --- | --- |
| 修复前 | `ip=eth1:dhcp:`、`bootdev=eth1` | 存在 | 绑定 `DEVICE=eth1` |
| 修复后 | 无新增网络启动参数 | 不存在 | 按 MAC 绑定，DHCP / ONBOOT 保留 |

解析器验证和 Go / 模板语法测试已通过；2026-10-15 的完整 PXE 实装进一步确认修复生效。

2026-10-15 回归：原始 Rocky 8.10 DVD 再次通过介质校验。目标盘为用户手动安装 Ubuntu 留下的单盘 LVM，另一块盘为 CentOS 的独立 LVM；已在 live 环境核对序列号、挂载和卷组成员后，仅离线准备获授权的目标盘。

新任务 `bd70a888e8e9fb40b17a8618` 于 `07:06:38Z` 领取安装授权，`07:10:08Z` 首次启动回报 `installed`。从选择安装入口起无需安装器人工应答，安装后直接进入 Rocky 登录界面。安装日志记录 kernel 6.328 秒、initrd 12.733 秒、userspace 15.381 秒；固件和菜单等待时间受人工操作影响，不计入网络等待修复的对比。

控制台密码、SSH 公钥、sudo、DHCP、sshd、SELinux Enforcing、root 锁定、无失败服务和回调脚本清理均再次核对通过。旧任务授权重放返回 HTTP 401；保留盘的分区表及磁盘 / 三个分区签名与本轮基线一致。原始证据保存于现场私有 QA 目录，不提交任务凭据和完整安装日志。
