# P95 CPU 分组验收记录（2026-10-30）

## 问题与修复

目标是 Rocky 9.2 / AMD Threadripper PRO 9995WX，192 个逻辑 CPU。原节点使用 cgroup v2，服务路径存在，但 CPU 控制器未启用，节点下没有 `cpu.weight`。P95 的 nice 调回 0 后，仍只有约 4.5 个逻辑 CPU 的计算时间；192 个通过独立会话启动的 memtester 形成 192 个 autogroup，而 P95 的线程同属一个 autogroup。

助手 1.0.3 在自己的 systemd unit 中请求默认 `CPUWeight=100` / `CPUShares=1024`，由 systemd 为相关父级和同层服务启用 CPU 控制器。安装、启动和检查会核对节点真实 CPU 控制器，拒绝把仅有 cgroup 路径或 helper active 当成成功。保留节点配置及全局 autogroup 开关；其他同层服务也会开始受各自已有或默认 CPU 权重约束，没有设置 CPU 上限。

参考：[systemd 资源控制](https://www.freedesktop.org/software/systemd/man/latest/systemd.resource-control.html)、[Linux 调度与 autogroup](https://www.man7.org/linux/man-pages/man7/sched.7.html)。

## 有限时长实测

用户明确授权不超过 60 秒的 P95 + memtester 对比测试，内存占 1%。实际测试共 **33.259 秒**，使用节点已安装的二进制、相同 P95 配置，在节点服务 cgroup 中的独立目录执行；未创建中心网页批次。P95 配置 192 计算线程（96 workers × 2），memtester 192 个单线程进程，合计 4992 MiB。

| 指标 | 1.0.2（CPU 控制器未启用） | 1.0.3（CPU 控制器启用） |
| --- | ---: | ---: |
| 采样窗口 | 7.279 秒 | 10.096 秒 |
| P95 平均逻辑 CPU 时间份额 | 4.517 | 97.384 |
| memtester 平均逻辑 CPU 时间份额 | 187.623 | 93.504 |
| P95 采样期有 CPU 时间进展的线程 | 81 | 192 |
| memtester 采样期有 CPU 时间进展的线程 | 192 | 192 |

修复后快照中 P95 有 192 个 R 状态线程、99 个 S 状态辅助线程；memtester 有 192 个 R 状态线程。全部 P95 线程 nice 为 0。**384 个可运行工作线程共享 192 个逻辑 CPU**，与约 191 个逻辑 CPU 的实际总计算时间相符。持续运行时 load average 可能接近 384，但该值是 1/5/15 分钟的运行/等待任务平均值，33 秒短测不能要求它立即达到 384。[Linux load average 定义](https://www.man7.org/linux/man-pages/man5/proc_loadavg.5.html)

测试结束后所有本次创建的负载均已停止。节点服务 PID 保持 `3809`，内核保持 `5.14.0-284.11.1.el9_2.x86_64`，`sched_autogroup_enabled` 保持 `1`。原始结果保存在测试节点 `/root/.ventoy-setup47-stage/p95-103-short-compare.json`。

这证明此次 CPU 时间争用得到解决；没有完成 P95 的完整算法轮次、中心网页端到端批次或长时间硬件稳定性验收。也没有据此认定其他 CPU、发行版和自定义服务布局都已实测通过。

## 分发与复查

完整 `ventoy-linux-setup.run` 继续仅复制文件，不包含 P95 修复。中心独立文件位于 `/home/ventoy-center/iventoy/tools/p95-priority-setup.sh`。节点同步 tools 后，按需执行：

```bash
bash /root/ventoy-tools/p95-priority-setup.sh
bash /root/ventoy-tools/p95-priority-setup.sh --check
```

从中心开始正常混合测试后，可用下面命令只读采样；该命令不会启动压测：

```bash
/usr/local/sbin/p95-priority-fyhocsys status --seconds 10
```

同时看实际工作线程状态、两类工具各自的 CPU 时间进展和算法日志。`steps: []` 仅表示没有识别到活动负载，不能作为混合压测通过的依据。
