# CentOS Linux 7.9 安装

使用 `centos-7.9` profile 和 x86_64 DVD / Everything 2009 镜像。当前优先完成该版本，其余发行版按 [18 个目标版本清单](linux-targets.md) 逐项推进。

## 镜像

本轮验证使用官方 DVD 2009，文件内容与 [CentOS Vault SHA-256 清单](https://vault.centos.org/7.9.2009/isos/x86_64/sha256sum.txt) 一致：

```text
官方文件名：CentOS-7-x86_64-DVD-2009.iso
现场文件名：CentOS-7.9-x86_64-DVD-2009.iso
大小：4712300544 bytes
SHA-256：e33d7b1ea7a9e2f38c8f693215dd85254c3a4fe446f93f563279715b68d07987
```

两种文件名都能建议 `centos-7.9`；文件名仅帮助选择，不能代替校验和及镜像内容验证。Everything 模板可选择，但本轮没有对 Everything ISO 做完整安装验证。Minimal、NetInstall 和 Live 不适用于当前完整 DVD 模板。

安装从 DVD 读取软件包，不依赖已停用的在线镜像列表。安装后的额外软件与更新需要单独准备可用的软件源，本模板不会暗中切换到第三方仓库。

## 7.9 专用兼容处理

- DVD 内的最小环境标识是 `minimal`，配置使用 `@^minimal` 和 `@core`；Rocky 保留其 `minimal-environment`。
- CentOS 7 安装环境没有 `base64`。安装前脚本直接写入经过 shell 转义的 PEM CA，仍然验证控制器 HTTPS 证书。
- 旧版 `lsblk` 对 VirtIO 磁盘可能不显示序列号。为空时读取该整盘的 `/sys/block/磁盘名/serial`，检查全部整盘并要求唯一匹配；不会退回“第一块盘”。
- `operator` 是系统内置账户，不能当作普通管理员创建。默认改为 `deployadmin`；首次启动核对普通用户 UID 与可登录 shell 后才回传完成状态。

## iVentoy 操作

把已核验 ISO 放入 iVentoy 的 `iso/CentOS/`，在原生界面刷新镜像。普通网启验证从客户端 Boot 菜单选择实际接线网口的 **UEFI PXE IPv4**，进入 `CentOS` 目录，选择 ISO，再选择 `Install CentOS 7`。

仅验证网启时停在欢迎 / 语言页，不开始安装。若安装器报告现有磁盘 UUID 重复，应退出并单独核实存储；不要把这类存储错误当作 PXE 传输失败，也不要为通过启动检查而清盘。

正式自动安装需要独立登记 MAC、目标整盘序列号及启动模式，核对并授权对应任务，再按 [iVentoy 文件准备流程](iventoy-files.md) 生成和绑定专属镜像 / Kickstart。不要将某台机器的任务脚本设为所有镜像的公共默认配置。

## 验证边界

2026-10-13，`0.3.0-alpha.2` 使用上述 DVD 和管理端实际生成的 Kickstart，在 KVM / QEMU 10.1、OVMF UEFI、2 vCPU、3 GiB RAM、24 GiB 新建虚拟磁盘上通过以下验证：

| 项目 | 结果 |
| --- | --- |
| 安装入口 | 从 DVD 提取的内核 / initrd 启动安装器，自动读取生成的 Kickstart |
| 完整安装 | 无人工应答，安装 306 个软件包、分区、引导程序及首次启动服务 |
| 磁盘启动 | 移除 ISO、直接内核及 initrd 参数后，仅从安装好的 qcow2 启动 |
| 系统版本 | CentOS Linux 7.9.2009，内核 `3.10.0-1160.el7.x86_64` |
| 管理账户 | `deployadmin`，UID 1000，属于 wheel；SSH 公钥登录与 sudo 密码验证通过 |
| 网络与安全设置 | VirtIO 网卡 DHCP，sshd active，SELinux Enforcing，root 密码锁定 |
| 首次启动回报 | `ready → installing → installed`；启动服务 Result=success，临时凭据脚本已移除 |
| 凭据重放 | 已完成任务再次领取安装，HTTP 401 拒绝 |

单元测试、安装配置解析、虚拟机安装和真实硬件 PXE 是不同层次。虚拟机验证使用独立控制器数据、新建 qcow2、用户态 NAT；不挂载宿主机物理盘，也不修改 fyhocsys / sckocstp。

CentOS 7.9 真实硬件完整安装仍需针对实际网卡、磁盘和固件完成验收；UEFI 下的结果也不代表 Legacy BIOS 或 Secure Boot 已通过。
