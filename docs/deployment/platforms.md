# 服务端平台

管理端采用 Go 标准库和嵌入式网页，发布 Linux / Windows x86-64 二进制，不依赖 fyhocsys 的运行环境或数据库。

## Linux

Rocky 8–10、Debian 12–13、Ubuntu 24.04 / 26.04 使用同一份静态二进制。部署脚本依赖系统现有 `systemd`、`getent`、`groupadd`、`useradd`、`install`。与 Rocky 9 的步骤相同，参见 [Rocky 部署说明](rocky9.md)。

各发行版的软件包、防火墙和 SELinux / AppArmor 策略仍需现场核对。CI 容器只验证二进制初始化、启动与正常停止，不验证完整 systemd、内核、网络广播、引导固件或 iVentoy。

Ubuntu 24–26 范围首先覆盖 24.04 / 26.04 LTS；中间短周期版本不单独做发行验收。

## Windows 10 / 11

直接运行：

```powershell
.\ventoy-center.exe init --dir C:\VentoyCenter\runtime
.\ventoy-center.exe serve --config C:\VentoyCenter\runtime\config.json
```

首次只允许本机回环访问。配置 HTTPS 监听、证书和私有目录后再向局域网开放。路径中反斜杠在 JSON 中写作 `\\`，或使用 `/`。

可在管理员 PowerShell 运行 `deploy/install-windows.ps1 -Binary 完整路径\ventoy-center.exe`。它新建只允许 SYSTEM 和 Administrators 访问的 `C:\ProgramData\VentoyCenter`，注册默认禁用的开机计划任务。配置完成后：

```powershell
Enable-ScheduledTask -TaskName VentoyCenter
Start-ScheduledTask -TaskName VentoyCenter
```

该脚本仅适用于新的目标目录；更新时先停止任务、备份私有运行目录、替换程序，再启动。iVentoy Windows 版独立安装，其 Windows 服务设置遵循官方说明。两个软件不共享进程或升级操作。

Windows 目录必须保持受限 ACL，不能仅依赖 Unix 的 `0600` 模式。手工运行时也需要由管理员控制目录访问。Windows 10 / 11 的真实系统验收尚待完成，Windows Server CI 测试仅证明 Go 代码能在 Windows 运行。
