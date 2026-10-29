# 2026-10-29 本地 Codex 验证交接

本次同步的是基于 `b34bb03`（PR #18）的未发布开发内容，代码版本仍为 `0.3.0-alpha.44`，没有发布新版中心、setup 或生产安装入口。验证分支为 `codex/setup-reinstall-integration`。

## 改动与入口

| 范围 | 实现入口 | 需要验证的行为 |
| --- | --- | --- |
| 已有系统盘强制重装 | `internal/center/templates/storage-raid-guard.sh`、`guard.sh`、`debian-guard.sh`、`storage_plan.go` | 按 VG UUID 核对成员；授权后重查，仅处理所选磁盘的旧同名 VG 和活动 swap；未选盘、跨盘成员和真实挂载保持保护 |
| 本机选盘 | `internal/center/templates/pxe-client.py`、`pxe-debian.sh` | 活动 swap 与真实挂载区分，旧系统盘经明确确认后可重建，取消不领取安装任务 |
| setup DHCP | `internal/center/assets/manual/manual-network.sh` | 链路尚未就绪时也加载新增 DHCP 配置；配置生成/发布/重载/激活失败准确返回待处理；不改已有主接口路由、DNS 或静态配置 |
| 长期密钥接入 | `internal/center/software_join.go`、`config.go`、`server.go`、`assets/manual/node_join.py` | 使用独立私有配置和鉴权接口；安装 0.8.6+ 节点后调用官方 `join --key-file`，检查绑定、证书、worker 和开机服务 |
| 主机名先于绑定 | `internal/center/assets/manual/install.sh`、`node_join.py` | 输入/确认 → 应用并回读 → 安装软件 → 绑定；改名失败、虚假成功、待处理或运行值不一致不绑定；首次常见默认名不绑定 |
| DEB/RPM 保留 | `internal/center/assets/manual/software.py` | 匹配软件组的包同时保留在 `/root/ventoy-tools/`，补齐旧版缺失副本而不重装；失败复用、摘要检查和用户改动保护 |

节点接入协议已核对 sckocstp 0.8.6 的 `1d6310c50341cf3e07e4aef67e3f0ca79502196b` 和 fyhocsys 0.8.6 的 `c66dc8febdbaeda81ee144c9afacbe2e02bfb13d`。这两个仓库仅用于阅读协议，没有代码需要回推到它们。

## 拉取与基础回归

在已有本地仓库中拉取分支；如果已建立同名本地分支，切换后按自己的本地改动情况更新，避免覆盖本地工作。

```sh
git fetch origin
git switch --track origin/codex/setup-reinstall-integration
```

Linux 环境使用 Go 1.25.x、Python 3.10+、Bash 和 Node.js。Windows 可以先跑 Go 测试，涉及 Linux Shell、PTY 和文件权限的检查在 Linux 或 WSL 中完成。以下测试使用临时夹具，不执行真实安装器或主机分区：

```sh
validation_dir=$(mktemp -d)
python3 -m venv "$validation_dir/venv"
"$validation_dir/venv/bin/python" -m pip install pykickstart==3.78 PyYAML==6.0.3
export PYTHONDONTWRITEBYTECODE=1
export VENTOY_TEST_OUTPUT="$validation_dir/fixtures"

go test -count=1 ./...
go vet ./...
go test -race ./...
node --check internal/center/web/app.js

"$validation_dir/venv/bin/python" scripts/validate_manual_setup.py
"$validation_dir/venv/bin/python" scripts/validate_software.py
"$validation_dir/venv/bin/python" scripts/validate_node_join.py
"$validation_dir/venv/bin/python" scripts/validate_templates.py "$VENTOY_TEST_OUTPUT"
"$validation_dir/venv/bin/python" scripts/validate_pxe.py "$VENTOY_TEST_OUTPUT"
"$validation_dir/venv/bin/python" scripts/validate_raid.py "$VENTOY_TEST_OUTPUT"
"$validation_dir/venv/bin/python" scripts/validate_storage_layouts.py "$VENTOY_TEST_OUTPUT"
```

`validate_manual_setup.py` 已包含 DHCP 与主机名顺序检查；`validate_raid.py` 包含存储保护用例。`validate_node_join.py` 的真实证书与属主权限用例在非 root 下跳过，在隔离 Linux 环境另以 root 运行该脚本即可覆盖；脚本使用临时文件和命令替身，不连接真实节点中心。CI 已加入这项 root 检查。

## 真实包管理器与虚拟磁盘检查

`scripts/validate_package_retention.py` 通过本地 HTTP 下载实际测试包，再执行真实 APT/RPM 安装。它只允许在存在 `/run/ventoy-policy-disposable-root` 标记的可丢弃 root 环境中运行，且测试包尚未安装。不要在业务主机上创建该标记并执行此测试。CI 的 `linux-update-policy` 作业给出了完整容器准备方式。

在准备好 Python 和包构建工具的隔离容器中运行：

```sh
touch /run/ventoy-policy-disposable-root
python3 scripts/validate_package_retention.py internal/center/assets/manual/software.py
```

Debian/Ubuntu 使用 `dpkg-deb`；RPM 环境需要 `rpmbuild`。可选第二个参数传入在另一隔离环境构建的 `ventoy-qa-retention` 1.0 测试包。此脚本会安装测试包，重复验证请换一个干净容器。

真实 LVM/MD/swap 检查使用 `scripts/validate_storage_vm.py`，要求匹配的测试内核及模块、QEMU、mkinitramfs、BusyBox、LVM、MD 和文件系统工具。准备方法见[开发与验证](testing.md)。它只向临时虚拟磁盘写入，不映射宿主块设备；不要直接运行配套的 `storage_vm_guest.sh`。

```sh
python3 scripts/validate_storage_vm.py "$VENTOY_TEST_OUTPUT" \
  --kernel /boot/vmlinuz-实际测试版本 \
  --log "$validation_dir/storage-vm.log"
```

## 本地真实节点接入

生产长期密钥没有提交到 Git。按[软件与节点接入](../deployment/node-software.md)的格式在仓库和 `software_dir` 之外配置 `node_join_config_file`，文件权限 `0600`，由运行中心的账户读取。用户已经提供 FYHOCSYS 的接入参数；在本地以私有配置输入，不把它粘贴进源码、测试夹具、PR 或日志。中心和节点均需 0.8.6 或更新。

本地验证时确认以下结果：

1. 节点先输入唯一 hostname，运行值和 `/etc/hostname` 都变为该名称之后才执行绑定。取消或改名失败、仍有 `hostname-pending` 时不发送绑定请求。`--software --hostname NAME` 应明确拒绝，提示先完成完整 setup。
2. 首次接入使用最终 hostname 出现在中心；同机重复运行不新增节点、不重启运行中的节点服务。
3. 改用其他中心或改变已登记名称时保留原绑定并报错。系统重装后的重新绑定仍遵守上游 DMI UUID 与至少 5 分钟离线条件。
4. `--check-software` 核对已装版本、包副本和本机节点配置/worker/服务；中心控制台另确认真实在线和心跳。该命令的本地检查成功不等于远端业务验收。
5. 正常重启后核对节点服务自动启动；绑定失败或主机名待完成时，完整 setup 的恢复服务仍保留重试请求。

完整 setup 打包还需要官方 `E610-MultiOS-6.4.5-r2.tar.gz`（源码固定 SHA-256 校验）。云端没有该正式驱动归档，因此没有生成可交付生产的新 `.run`；不能用 Go 测试中的假驱动/假软件包装机。可用 `VENTOY_E610_ARCHIVE` 配合 `TestManualSetupDownload` 验证真实打包；生产软件地址、只读凭证和 CA 应由实际配置的中心下载接口生成。

## 云端已完成与待确认的边界

| 项目 | 云端结果 | 尚未覆盖 |
| --- | --- | --- |
| Go、vet、race、Shell/Python/JS | 通过；软件客户端 10 个测试方法、root 接入 6 个测试方法 | 本地环境差异和正式发布 CI |
| 旧存储保护 | 单盘/双盘各 39 场景；27 项真实 QEMU 存储检查，另加旧版失败对照共 28 项 | 所有真实控制器、硬件和存储组合 |
| 已装系统覆盖重装 | Debian 13 BIOS 原生安装器对旧系统快照覆盖安装并两次硬盘启动；未选盘 SHA-256 不变 | UEFI、全发行版及真实 iVentoy 网启实装 |
| DHCP | 12 项隔离回归；Ubuntu 22.04.5 用户空间真实 DHCP、重复执行和重启通过，主接口配置/路由/DNS 保持 | Ubuntu 22.04.3 原内核与 E610 实卡；现场问题是否同因 |
| 包原文件保留 | Ubuntu 22.04 APT、Rocky 9.8 RPM 真实下载/安装/保留/补传/改动保护通过 | 用户真实业务包的维护脚本及功能 |
| 长期密钥 | 私有 API、真实子进程和临时密钥文件、参数契约、失败/重试、主机名顺序通过；用户实际配置已在临时本地中心读取验证 | 真实上游节点程序、生产中心绑定、远端心跳、真实开机及压测 |

DHCP 虚拟机使用 Debian 测试内核 `6.12.113+deb13-amd64`，并非 Ubuntu 22.04.3 原内核。上游正式节点构建因 SQLite 依赖下载受云端网络策略限制未完成；公开发布资产读取返回 404。没有绕过限制，也没有改动生产中心。

原始云端日志、镜像和临时测试根位于该次会话的 `/workspace/ventoy-validation/`，不是仓库文件，本地拉取不会带来这些路径。上表与[进度记录](progress.md)保存可移交的结果及边界；本地复测日志请另存于自己的临时目录，不提交密钥、安装介质或大型测试磁盘。
