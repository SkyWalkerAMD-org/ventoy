# 开发记录

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
