# Ubuntu Server 22.04.3 实机验收

2026-10-14，使用 `ubuntu-22.04` 模板、iVentoy 1.0.44 / Standard / ProxyNet 在 Intel X710 / UEFI 物理机上完成安装、硬盘启动、控制台 / SSH / sudo 登录及完成回报验收。启动安装时仍需一次 `yes` 确认；本记录不扩展到其他 Ubuntu 小版本、Desktop 或 Legacy BIOS。

## 镜像与目标

- `ubuntu-22.04.3-live-server-amd64.iso`，2,133,391,360 字节，内部日期 `20230810`。
- SHA-256：`a4acfda10b18da50e2ec50ccaf860d7f20b389df8765611142305c0e911d16fd`，与 [Ubuntu 官方历史镜像校验文件](https://old-releases.ubuntu.com/releases/22.04.3/SHA256SUMS)一致。
- Xeon w7-2495X、64 GiB 内存，目标为按唯一序列号登记的一块 447.1 GiB SATA SSD；另一块同型号 SSD 保留。
- 管理端 `0.3.0-alpha.7`，代码 `ea44e29`。每次失败后先保留日志、停止客户端、人工结单，再生成新的一次性任务，未重复使用已领取授权。

## 已定位并修复的问题

### 磁盘序列号匹配

此机器的 `lsblk SERIAL` 是磁盘短序列号，但 Subiquity 5004 的 `match.serial` 使用包含型号前缀的 udev `ID_SERIAL`。直接写入短序列号找不到磁盘，安装器随后报 `NoneType ... id`。

初始配置现在使用不存在的路径。安装前的 MAC、唯一短序列号、设备类型、存储和固件检查通过后，将确认的 `/dev/<disk>` 原子写入 `/autoinstall.yaml`，然后才领取单次授权。原始安装器与实际 udev 数据回归验证已通过，实机也已通过分区配置并写入目标盘。

此方式依据 [Subiquity 的 early-commands 与磁盘匹配规则](https://canonical-subiquity.readthedocs-hosted.com/en/latest/reference/autoinstall-reference.html)：安装器会在 early-commands 结束后重新读取配置。

### EFI 软件包下载

首次通过分区后，GeoIP 自动选择的镜像站返回 HTTP 403，`grub-efi-amd64`、`shim-signed` 等引导包无法下载，Curtin 以退出码 3 停止。客户端实测官方主仓库和安全仓库 HTTPS 均返回 200。

模板现在关闭 GeoIP 镜像替换，显式使用 `https://archive.ubuntu.com/ubuntu` 与 `https://security.ubuntu.com/ubuntu`；不关闭 TLS 或 APT 签名检查。修复后的实机已完成 EFI 包、系统与安全更新安装，并成功从硬盘启动。配置格式参照 [Curtin APT 配置](https://curtin.readthedocs.io/en/latest/topics/apt_source.html)。

## 实机结果

| 检查 | 结果 |
| --- | --- |
| 安装介质 / 系统标识 | 安装记录为 `Ubuntu-Server 22.04.3 ... (20230810)`；`/etc/os-release` 为 Ubuntu 22.04.3 LTS |
| 内核 | 安全更新后的 `5.15.0-198-generic`，不等于 ISO 最初的软件包版本 |
| 安装目标 | 已登记 SSD 的 `sda1` 为 EFI 分区，`sda2` 为 ext4 根分区；从 UEFI `ubuntu` / `SHIMX64.EFI` 启动 |
| 再次重启 | 调整为硬盘优先后，正常重启直接进入 Ubuntu；boot ID 已改变，根分区、内核、SSH 与 cloud-init 再次核对通过 |
| 登录 | `deployadmin` 控制台密码登录、SSH 公钥登录、sudo 执行验收脚本均通过；root 密码锁定，SSH 密码认证关闭 |
| 网络 | LAN1 / Intel X710 按目标 MAC 配置，DHCP 和到管理端的路由正常 |
| 系统状态 | 无失败 systemd 服务；cloud-init 为 done 且无错误；`dpkg --audit` 无输出 |
| 首次启动回报 | 任务 `91699face3f36fbbacf6de32` 于 `2026-10-14T09:02:10Z` 回报 `installed`；服务退出码 0，随后禁用并删除凭据脚本 |
| 重放保护 | 对已完成任务重放旧授权返回 HTTP 401 |
| 保留盘 | 分区表与安装前基线逐字一致；VFAT UUID、RAID 成员 UUID 与签名保持一致，未挂载为根目录 |

测试机原有 BootOrder 将 LAN1 排在硬盘之前，因此安装后第一次重启再次进入 iVentoy。验收时从 BIOS Boot Override 选择 `ubuntu`，确认系统正确后将其设为第一启动项，保留所有 PXE 入口。iVentoy 的“Exit and continue BIOS boot”在此固件组合下曾退到 iPXE 错误提示，未用它代替硬盘启动验收。启动顺序调整属于本次现场操作，尚未作为产品自动配置能力交付。

原始安装日志、验收 JSON、磁盘基线及旧授权拒绝结果保存在现场服务器的受限 QA 目录；不将口令、任务凭据或原始 user-data 提交到仓库。

## 验收边界

### alpha.8 无人值守候选改动（待实机验证）

`ubuntu-22.04` 的新任务会同时生成 `user-data` 和 `ubuntu-iventoy.tar.gz`，两者需绑定到同一专属任务镜像。兼容包使用 [iVentoy Live Injection 入口](https://www.iventoy.com/en/doc_live_injection.html)，在 live root 启动前核对该任务 user-data 的 SHA-256、目标 MAC 和 Ubuntu 22.04 系统标识，再为 live 环境中的 Subiquity 服务添加启动参数。原始 ISO 与 snap 文件保持原样，磁盘、固件和单次授权检查仍在 early-commands 中执行。

安装配置还设置 Curtin `grub.reorder_uefi: false`，避免把当前 PXE 入口重新移到首位；`remove_duplicate_entries: false` 保留现有 EFI 入口，参见 [Curtin GRUB 配置](https://curtin.readthedocs.io/en/latest/topics/config.html#grub)。旧任务的安装包不会自动改写；应创建新任务进行回归。

已通过模板解析和 Linux 下的脚本正反向检查；是否全程无需人工确认、装完是否直接从硬盘启动，仍需实机测试。暂不把这一兼容包用于 Ubuntu 20.04 / 24.04 / 26.04，也不把对 22.04 profile 的支持视为每个小版本已验收。

### alpha.7 实机记录的限制

- 当前 iVentoy 启动链未传入 `autoinstall` 内核参数，安装器要求一次 `yes` 确认。本次使用已授权的清盘范围确认；不能将结果标为全程无人值守。
- 当前 Ubuntu 模板需要能访问配置的软件仓库。此次系统仍标识为 22.04.3，但已安装在线安全更新；不属于完全离线或原始软件包版本固定的验收。
- 目标盘之前的 LVM 已在获授权后离线准备；模板仍拦截已有 LVM / RAID，不自动拆解跨盘存储。
- 保留盘验收核对分区表与文件系统 / RAID 签名，未进行整盘逐字节数据校验。
