# 安装后从中心获取软件

从 alpha.29 开始，新版 `ventoy-linux-setup.run` 可以从中心服务器获取 RPM / DEB，在已安装并启动的 Linux 节点上安装或升级。不重新装系统，不改磁盘；软件模式不重设账户、主机名或网络。

## 服务器目录

本次部署使用用户指定的 `/home/ventoy-center/iventoy/tools/`。管理端 `config.json` 增加：

```json
{
  "software_dir": "/home/ventoy-center/iventoy/tools",
  "software_rpm_min_el": {"fyhocsys-node": 8}
}
```

通过 SFTP 上传 RPM / DEB 到此目录。保留标准文件名，如 `name_version_amd64.deb`、`name-version-release.el9.x86_64.rpm`。先上传成 `.part`，完成后原子改名为 `.deb` / `.rpm`；不要一边下载一边覆盖正在发布的文件。文件应为普通文件，服务账户有目录读取/遍历权限及文件读取权限。服务只读此目录。

打开管理端“软件分发”，查看包清单并下载新版 setup。替换、新增或移除包后无需重启中心、iVentoy，也无需重新下载 setup；节点下一次执行时读取最新清单。

alpha.32 同时分发 tools 根目录中的 `*.tar.gz`：这些文件原样保存到目标机的 `/root/ventoy-tools/`，只传输及校验 SHA-256，不解压、编译或执行。例如 `ipmitool-IPMITOOL_1_8_19.tar.gz` 会保留这个文件名。RPM / DEB 仍按系统匹配并自动安装，源码压缩包不会被当成已安装软件。

未变化的压缩包校验后跳过下载；中心替换同名文件后，下一次运行原子替换节点上由 setup 管理的旧文件。节点文件被用户改动或同名文件不属于 setup 时保留并报错。中心删除文件不会删除节点已有副本。压缩包与软件包合计最多 256 个文件、8 GiB，单文件最多 4 GiB；目录中未完成的 `.part` 文件不发布。`--list-software` 列出两类文件但不传输，`--check-software` 同时回读软件版本与已分发压缩包摘要。

## 节点执行

alpha.31 的预置流程在普通手动安装和 PXE 自动安装结束时，将新版文件放在 `/root/ventoy-linux-setup.run`，无需另行下载。安装器只复制并校验文件，不执行软件安装。已安装的旧系统不会被远程改写，可从管理端下载新版后替换一次。

```bash
# 无参数：完整系统配置 + 匹配的软件安装 / 升级 + 自动检查
sudo /root/ventoy-linux-setup.run

# 仅安装软件；不重设账户、主机名、网络
sudo ./ventoy-linux-setup.run --software

# 查看当前候选包；不安装
sudo bash ventoy-linux-setup.run --list-software

# 检查上次成功软件组是否仍已安装到要求版本或更新版本
sudo bash ventoy-linux-setup.run --check-software

# 完整配置并指定主机名；--apply 保留兼容但无需填写
sudo ./ventoy-linux-setup.run --hostname server01
```

`--offline` 不连接软件库。从 alpha.32 起，附加网卡等待 DHCP、驱动等待重启等 `PENDING` 检查不会跳过用户请求的软件安装；已有网络可用时继续下载、安装并验证。软件失败明确报错并保留请求，用户再次运行或配置恢复服务可以重试；成功后清除请求，正常后续重启不再从软件库更新。新增软件、升级软件都由用户主动再次运行 setup 触发。

## 自动选择与升级

- Ubuntu / Debian 选择本机架构的 DEB；EL 系列选择本机架构的 RPM。`all` / `noarch` 包也可使用。默认根据 RPM 的 EL 大版本匹配；例如 `.el9` 不发给 EL8。
- 名称、版本、Epoch、Release、架构以包内元数据为准。不同发行版之间的依赖兼容性由本机包管理器检查。
- 同一软件存在多个兼容版本时，使用原生 dpkg / RPM 比较选择最高版本。已安装相同或更新版本时跳过，不降级；新旧文件可暂时同时保留。同名、同版本、同架构但内容不同的重复包会停止并提示清理冲突。
- 清单和文件 SHA-256 未变化、已安装版本检查通过时，不重复下载或安装。请正确提高包版本号；原地重打同版本包不视作升级。
- 从服务器移除文件不会卸载节点已安装的软件，也不会设置后台定期软件更新。
- `software_rpm_min_el` 是管理员明确声明的向上兼容规则。本次根据用户确认，`fyhocsys-node` 从 EL8 起向上兼容；不能用于低于其构建版本的 EL。规则只适用于包内同名软件，不会扩大到 `sckoct-fyh` 等其他包。

需要专属软件组时，可使用 `系统/版本/架构/default/`：如 `ubuntu/22.04/amd64/default/`、`debian/12/amd64/default/`、`rocky/9/x86_64/default/`。专属组优先于根目录自动组。Ubuntu 精确匹配 `VERSION_ID`；EL / Debian 优先精确版本，其次大版本。不自动借用另一个 Ubuntu 版本的软件组。

## 依赖与结果

先下载并校验整组软件，读取包内信息，再做依赖和安装事务预检查。优先使用已安装的依赖和同时上传的依赖包；缺少依赖时从节点已配置的软件源获取。APT 仅在本次明确执行的安装需要时刷新索引；不会启用 APT 定时刷新或无人值守升级。普通依赖库可能随安装事务更新；不执行全系统 `upgrade` / `distro-sync`，不添加未知软件源或强制忽略依赖。

内核包不能通过软件分发安装；APT 事务中发现内核修改/软件删除会停止，RPM 依赖解析排除 `kernel*`。安装后再次对比内核包集合，并在已有策略时检查内核锁定和禁用自动更新。失败保留错误记录，不把部分完成标记为成功。包的维护脚本以 root 执行，应上传自己信任的软件；脚本失败后的部分改动不能保证自动回滚。

setup 会输出系统配置检查、APT 事务预演、实际安装及复查日志，因此同一清单可能出现两次。`apt update` 下载的是软件源索引；依赖事务中出现 `upgraded` 则确实表示对应依赖包升级，应按包名核对。登录页的 `New release ... available` 仅为发行版升级提示，不能当作已经升级的证据。以实际运行内核、已安装内核包集合和 APT 历史为准。

校验、下载、架构/EL 不匹配、依赖和安装错误返回非零。没有匹配软件时显示 `SKIPPED`。日志：`/var/log/ventoy-software.log`；最近事务：`/var/lib/ventoy-center/software/last-result.json`。完整配置流程中的日志也进入 `/var/log/ventoy-manual-init.log`。

客户端内置中心 HTTPS 地址、公开 CA 和只读下载凭证，不含管理口令或安装任务凭证。管理口令轮换会使既有只读凭证失效，需要重新下载 setup。下载凭证不授予管理、上传、删除或远程执行权限。

依赖解析参考 [APT 官方手册](https://manpages.ubuntu.com/manpages/noble/man8/apt-get.8.html) 与 [DNF 官方命令参考](https://dnf.readthedocs.io/en/latest/command_ref.html)。

## 验证范围

2026-10-25，用户在 `.235` 新装 Ubuntu 22.04.3 后无参数运行 setup。现场只读核对确认 `fyhoc 1.1.24-1`、`fyhocsys-node 0.8.3-1`、`sckoct-fyh 1:1.1.8-1` 均已安装；为补齐依赖，APT 新装 ipmitool/libatomic1 等包，并将 `gcc-12-base`、`libgcc-s1`、`libstdc++6` 从 `12.3.0-1ubuntu1~22.04` 升到 `12.3.0-1ubuntu1~22.04.3`。未执行全系统升级，运行内核及唯一已安装内核仍为 `5.15.0-78-generic`（包版本 `5.15.0-78.85`），hold 保留，五个 APT/无人值守更新单元全部 masked/inactive，APT 周期配置均为 0。软件请求完成；附加网卡 `enp188s0f1` 的 IPv4 DHCP 租约仍待获取，因此系统总状态是 `PENDING`，不等于软件安装失败或内核已更新。

alpha.32 在 Ubuntu 24.04.3 双盘 RAID1/LVM 实机上直接无参数运行 setup，安装并检查 `fyhoc 1.1.24-1`、`fyhocsys-node 0.8.2-1`、`sckoct-fyh 1:1.1.8-1`；`fyhoc -V` 与依赖 `ipmitool -V` 实际执行成功。`ipmitool-IPMITOOL_1_8_19.tar.gz` 原样传到 `/root/ventoy-tools/`，与中心摘要一致且未解压。重复运行不再下载软件包或压缩包；SSH、DHCP、内核锁定及禁用自动更新复查通过，内核保持 `6.8.0-71-generic`。所有 54 个普通 Linux 入口及 52 个 PXE 入口已更新预置包并核对摘要；此处不代表全部发行版都重新做过实机安装。

alpha.29 在 Ubuntu 24.04.3 隔离虚拟机中实际安装了 `fyhoc 1.1.24-1`、`fyhocsys-node 0.8.2-1`、`sckoct-fyh 1:1.1.8-1`，验证缺失依赖获取、第二次运行跳过、已安装版本回读及原有内核/更新策略。RPM 在隔离的 Rocky 9 根文件系统中使用真实 RPM / DNF 验证整组事务、仓库补齐依赖、原生版本排序、升级、不降级及失败时不进入安装事务。

这些结果验证分发机制；不代表所有软件在全部发行版或真实硬件上的运行功能均已验收。软件自身的接入配置和硬件功能仍按其要求执行。
