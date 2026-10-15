# iVentoy 1.0.45 现场升级记录

2026-10-15，Rocky Linux 9.8 装机服务器的 iVentoy 已从 1.0.44 免费版升级到 1.0.45 免费版。Ventoy Center 管理端仍为 `0.3.0-alpha.8`。

## 来源与迁移方式

使用 [iVentoy 官方 1.0.45 发布包](https://github.com/ventoy/PXE/releases/tag/v1.0.45)：`iventoy-1.0.45-linux-x86_64-free.tar.gz`，24,725,069 字节，SHA-256 为 `c7a94e1170d5b20eda7a16ac1308a00906b35d91f4488ffbe5ae6f9e40791f63`，与官方 GitHub 资产摘要一致。

按[官方升级说明](https://www.iventoy.com/en/doc_update.html)，先解压到独立目录，再迁移 `data/config.dat`、镜像和本站用户脚本，没有将新程序覆盖解压到旧程序目录。ISO 通过同一文件系统的硬链接迁移，不复制数百 GB 内容；旧版程序、配置和脚本独立保留。

由于管理端、systemd 和已有任务绑定使用绝对路径，切换时将新版本目录置于原运行路径 `/home/ventoy-center/iventoy-1.0.44`。目录名保留历史版本号，实际运行程序和后台版本均为 **1.0.45**。运行路径不是符号链接。

切换前确认没有待启动或安装中的管理端任务，测试机仍运行已验收的硬盘 Ubuntu，未持有网络镜像。短暂停止 iVentoy 与管理端后备份配置、切换目录并重新启动；管理端重启也刷新了 systemd 对 ISO 和脚本目录的挂载。没有重新安装客户端或修改 fyhocsys / sckocstp。

## 升级后检查

- iVentoy 页面及启动日志显示 `1.0.45 Linux x86_64 (Free Edition)`；运行进程对应已校验的新二进制。
- 59 个镜像入口与升级前一致，文件 inode、大小及权限在迁移时核对一致；40 个用户文件的 SHA-256 一致。
- 23 项已有页面参数一致，包含 ProxyNet、Standard Secure Boot、服务端口及空的全局默认镜像。
- Ubuntu 22.04.3 专属任务的 user-data、注入包路径、脚本编号 1 与 5 秒超时绑定保持有效。
- 1.0.45 新增参数使用其默认值，包括 TFTP 最大块 1468 字节和目录过滤。新版本自行保存配置，因此升级后的 `config.dat` 不要求与旧版逐字一致；原始旧版配置保留在回退目录。
- iVentoy 与管理端均正常运行且保持开机启动，管理端任务状态文件未变，HTTPS 管理接口可正常读取 iVentoy 文件状态。
- 管理页面仍为 `https://192.168.1.2:26080`，iVentoy 页面仍为 `http://192.168.1.2:26000`。

本次完成软件升级与服务、配置检查。CentOS 7.9、Rocky 8.10、Ubuntu Server 22.04.3 的完整实装证据来自 **iVentoy 1.0.44**；未在 1.0.45 下重新清盘实装。官方此次包含 Ubuntu Server cloud-init 修复，本站任务注入包继续保留，不能在未复测前直接移除。

## 回退资料

服务器受限目录：`/home/ventoy-center/upgrade-iventoy-20261015/`。

- `rollback-1.0.44/`：旧版完整运行目录，含原始配置、脚本及 ISO 硬链接。
- `controller-config.json.before`、`controller-state.before.json` 和 `iventoy.service.before`：切换前管理端配置、状态及服务定义。
- `iso-before.json`、`user-before.json`、`upgrade-result.json`：迁移清单、文件摘要与检查结果。

回退前先确认没有客户端正在使用网络镜像，停止 iVentoy 和管理端，将当前运行目录移到另一受限目录，再把 `rollback-1.0.44` 放回原运行路径，然后启动并检查两个服务。不得覆盖当前目录、复用已领取的安装授权或不加核对地回滚管理端任务状态。备份中的 ISO 硬链接共享内容，不属于独立的镜像数据备份。
