# 普通 ISO 手动安装后的完整配置

alpha.30 将包含[安装后软件分发](node-software.md)的新 setup 统一预置到普通手动安装和 PXE 自动安装结果。无参数运行 `/root/ventoy-linux-setup.run` 从中心获取适合本机的软件，`--apply` 执行完整配置。后续中心替换 RPM / DEB 不需要更换 setup。

中心配置启用 `"linux_setup_preload": true` 后，生成的 Linux 专属任务与 PXE 菜单注入包包含当前 setup，安装收尾必须完成复制和 SHA-256 校验。普通手动入口绑定 `/api/linux-manual-setup?format=injection` 生成的当前注入包。升级中心后应重新生成并发布既有入口的注入包；已有准备文件不会在活动安装期间自动改写。若已校验的 E610 完整离线包缺失，生成失败，不会悄悄省略 setup。

此更新通过 iVentoy 注入覆盖介质中的旧托管预置，不重建 ISO，也不改变内置 E610 驱动。脱离该 iVentoy 直接使用旧 ISO 时，仍是原来内置的版本。此前已安装的节点不自动替换文件，可使用管理端下载入口更新一次。

从 alpha.24 起，普通 Linux ISO 可以绑定 **手动安装配置包预置注入文件**。安装器保留交互步骤，在安装收尾时将完整配置包复制到新系统的 `/root/ventoy-linux-setup.run`。进入硬盘系统后，由用户手动执行：

```bash
sudo bash /root/ventoy-linux-setup.run --apply --hostname server01
```

这套配置覆盖现有 Linux 家族：Ubuntu 20–26、Debian 10–13，以及 CentOS、Rocky、AlmaLinux、RHEL 7–10（x86_64）。不仅用于 Ubuntu / Debian。Windows 不在本轮 Linux 配置包范围内。

**预置不等于已配置。** 运行前仍使用手动安装时创建的账户和网络设置，预置过程不改密码、不启用 SSH、不应用 DHCP 或更新策略，也不替用户选盘。`/var/lib/ventoy-center/manual-setup-preloaded` 中的 `PRELOADED_ONLY` 只证明文件已放入，不能当成配置验收通过。执行 `--apply` 后再查看下文的 `VERIFIED` / `PENDING`。

alpha.24 的普通 ISO 预置依赖 iVentoy 的注入绑定。alpha.25 的 E610 适配 ISO 已把驱动、原生安装完成钩子及配置包写入镜像：普通手动入口与 PXE-Auto 复用同一成品，E610 驱动随安装自动写入目标系统，配置包仍由用户安装后手动执行。原生已支持 E610 的镜像继续保留原驱动和普通预置绑定。未经适配、也未绑定注入的原 ISO 不会携带这些内容；管理端保留下载入口供已有系统使用。

不传 `--hostname` 时保留安装时设置的合法名称。默认 localhost 会替换成机器标识生成的名称。默认账户为 `admin / ventoy`、`root / ventoy`；保留其他用户。需要保留原密码时加 `--keep-passwords`，此时须已有可用的 admin/root 密码，并自行做客户端登录验证。

## 自动应用和检查

alpha.27 保留 Ubuntu 已启用的 SSH socket 启动方式，避免首次执行 setup 时同时留下采用旧策略的 SSH 监听进程。Ubuntu 24.04.3 已通过真实隔离 iVentoy 交互手动安装、点击重启后进入硬盘系统、setup 配置及再次重启验收。普通 ISO 的完成钩子只为 live 安装介质准备正常卸载；目标系统配置仍需手动执行 setup。

| 项目 | 应用方式 | 完成检查 |
| --- | --- | --- |
| SSH | 启用服务、root 密码登录、admin sudo；放行已启用防火墙中的 SSH 端口 | 检查有效配置，并实际以 root/admin 密码连接本机 SSH |
| 主机名 | 校验、备份、设置、同步本机 hosts；保留 cloud-init 主机名 | 静态名称与运行名称一致 |
| DHCP | 为未配置硬件有线网卡建立持久 DHCP 配置，并激活新增配置 | 检查持久配置和已连接接口的实际 IPv4 地址 |
| 内核锁定 | APT hold/pin 或 yum/dnf `kernel*` 排除 | 比对已安装内核清单、包管理器策略 |
| 禁用自动更新 | APT 周期刷新和 unattended-upgrades；或 yum-cron/dnf 自动升级及缓存刷新 | 持久屏蔽和当前运行状态 |
| CLI | 默认 `multi-user.target` | 读取默认启动目标 |
| E610 | 原生支持则保留；否则精确匹配离线驱动，或用当前内核头文件编译 | 校验 ABI/设备 ID、启动镜像、实际加载模块与 E610 绑定状态 |

已有 DHCP、静态网络、Bond/VLAN/Bridge、USB、Wi-Fi 和当前 SSH 出口保留。新增次要 DHCP 接口不接管默认路由和 DNS。支持 NetworkManager、Netplan、systemd-networkd、ifupdown 的 ISC dhclient / dhcpcd，以及符合检查的传统 ifcfg；复杂或无法确定的配置保留并报告待处理。

安装包自解压，支持 Python 2.7 / Python 3 / EL8 platform-python，不要求最小化系统已有 tar。包内包含已校验的 E610 多系统 r2 离线模块与 Intel 6.4.5 源码；缺少 SSH、sudo 或精确内核构建依赖时可从系统软件源补齐，不做系统或内核升级。`--offline` 禁止下载。

## 检查结果和边界

```bash
sudo bash /opt/ventoy-manual/current/install.sh --check
sudo cat /var/lib/ventoy-center/manual-init/status
sudo journalctl -u ventoy-manual-init.service
```

`VERIFIED` 表示当前适用检查通过；`PENDING` / 退出码 3 表示仍需要驱动重启、证书注册或 DHCP 租约。已安装服务在以后每次启动继续处理和检查，不自动重启机器。没有 E610 时输出 `E610_NOT_PRESENT`，不能据此认定实际 E610 硬件已验收。

更新策略锁定执行脚本时的内核，不把手动安装过程中已更新的内核降回 ISO 版本。E610 适配 ISO 保留原生安装器的手动流程，并独立完成驱动持久化，不依赖执行本配置包；启动内核和安装目标内核必须各有精确匹配的驱动。未经适配的原 ISO 在安装环境缺少 E610 时，不能依靠安装后才执行本包来解决网启问题。

Secure Boot 首次使用外部签名驱动可能需要控制台 MOK 注册；未知内核也可能缺少历史头文件。遇到这些情况明确停止或报告待处理，不强行加载其他 ABI 的驱动，也不将保存配置等同于验证成功。

日志：`/var/log/ventoy-manual-init.log`。账户及配置备份：`/var/lib/ventoy-center/manual-init/`；主机名备份：`/var/backups/safe-server-init/`。驱动按原包说明回退，动态构建路径为 `/var/lib/e610-ixgbe/manual-系统-内核/payload/install.sh --rollback`。

管理端 API 为经过管理员认证的 `/api/linux-manual-setup?format=run`，不含控制器令牌。服务端需要在配置的 `PackageDir` 放置原始 `E610-MultiOS-6.4.5-r2.tar.gz`，固定 SHA-256 为 `8a3b65589e4b55afef48f6fd797f8c8b08bdb2ec342c3954adfc45d44b686c94`；缺失或校验错误时拒绝生成。`/api/linux-manual-setup` 保留 tar.gz 下载。

`/api/linux-manual-setup?format=injection` 生成 `ventoy-manual-preload.tar.gz`。将其绑定到普通 Linux ISO 的启动注入文件，不绑定自动安装答案。注入包包含经过校验的完整 `.run` 文件、Debian d-i 的 `finish-install.d`、Anaconda 的 `post-scripts` 和 Ubuntu 的安装收尾钩子。复制前后核验 SHA-256，不覆盖用户已有的内容不同的同名配置包；目标系统的 `/root/README-ventoy-setup.txt` 提供操作说明。启动配置只属于安装器环境，不会在已安装系统中启用自动执行 setup 的服务。

Ubuntu 20.04.3 的 Subiquity 尚无原生 postinst 目录，因此在安装器的完成阶段增加复制回调，保留原交互流程；Ubuntu 26 桌面版保留原启动参数和全部内置完成钩子，只增加预置复制。两种兼容启动包装都不传入自动安装参数，不修改原始 snap 文件。

更新预置绑定时，只有尚未执行的托管旧包才可刷新：`PRELOADED_ONLY`、原校验文件和实际 SHA-256 必须一致，且不存在 `manual-init` 已应用状态。旧文件以 `.preload-backup.<摘要>` 保留。用户修改、无标记或已应用的文件继续保护，不覆盖；介质内置 E610 驱动不受此更新影响。

用户提供的 `safe-server-init` 保留为独立工具；本项目另加激活和检查封装。此处适配增加 Debian 10 范围，以及仅在 sysfs 确认永久地址未被修改时识别 networkd 的 `PermanentMACAddress`。原脚本未知配置保护仍保留；完整包的新增网卡激活行为不能解释为原脚本单独运行也会全局应用网络。
