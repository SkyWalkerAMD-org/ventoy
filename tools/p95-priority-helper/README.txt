P95 独立优先级工具 1.0.3
========================

目的
----
修正 fyhocsys / sckocstp 节点中，mprime 30.19 的压测工作线程默认
nice 19、与普通优先级 memtester 同跑时很难获得 CPU 时间的问题。
把符合范围检查的 P95 线程从 nice 19 调到普通优先级 nice 0。
每秒重新检查，新线程以及被 mprime 再次降低优先级的线程也会处理。
1.0.3 同时解决 CPU 控制器未启用时的 autogroup 争用：一个多线程 P95
与大量 setsid() 启动的 memtester 会形成数量悬殊的调度组，nice 0 仍
不足以公平分配 CPU。助手服务请求默认 CPUWeight=100 / CPUShares=1024，
systemd 据此启用同层服务的 CPU 控制器；启动及检查时核对节点的实际
CPU cgroup。没有启用 CPU 控制器时，不再只凭“助手 active”报告通过。

独立性与适用范围
----------------
本工具通过 Linux setpriority 调整线程，并在自己的 systemd 服务中启用
默认 CPU 调度权重。systemd 会同时为父级及同层服务启用 CPU 控制器，
因此同层服务也开始受各自已有/默认 CPU 权重约束；没有设置 CPU 上限。
不修改 fyhocsys / sckocstp 源码、RPM/DEB 文件、manifest、prime.txt、
中心配置、节点凭据、测试计划或日志；不启动压测，不重启节点服务。

当前范围：Linux x86_64、systemd 219 或以上，默认安装目录。
依据已核对的 0.8.6 节点布局设计，包括 CentOS 7.9 的 EL7 工具目录。
可执行文件必须为：
  /opt/<product>/workloads/0.1.0/mprime/mprime
  /opt/<product>/workloads/0.1.0/memtester/memtester
另外接受以下固定的包内路径：
  /opt/<product>/workloads/0.1.0-el7/mprime/mprime
  /opt/<product>/workloads/extra-0.2.0/memtester/memtester
进程必须属于 systemctl 查询到的 <product>-node.service cgroup，
工作目录必须在 /var/lib/<product>/node 下。
P95 还必须有匹配工作目录的 -t 和 -W 参数。
只调整 nice 19 且采用 SCHED_OTHER / SCHED_BATCH 的线程。
手工设置为其他 nice 值或实时调度策略的线程不会被改动。
不接受其他版本的任意路径；容器和自定义数据目录需另行验证。

CentOS 7.9 兼容
--------------
systemd 219 不支持 RuntimeDirectoryPreserve 和 systemctl show --value。
本版读取有字段名的 LoadState，分开执行 enable/start、stop/disable。
219-234 上由助手创建并验证 root:root 0700 的 /run 状态目录，崩溃或
重启助手时保留撤销日志；整机重启后 /run 自然清空。235+ 继续使用
RuntimeDirectoryPreserve=yes。两种模式都使用 Type=notify、真实权限
自检和 cgroup 归属检查；不通过放宽进程识别或取消保护来兼容旧系统。

一键部署：在被测节点执行
------------------------
只需复制 p95-priority-setup.sh 到被测节点，以 root 执行：

  bash p95-priority-setup.sh

自动识别 fyhocsys 或 sckocstp 节点。若不能唯一识别，指定产品：
  bash p95-priority-setup.sh fyhocsys
  bash p95-priority-setup.sh sckocstp

脚本已内嵌程序、完整源码、校验清单和本说明，不需要互联网下载或现场编译。
安装后，源码、说明和可重新构建发布包的脚本保存在：
  /usr/local/share/p95-priority-helper/1.0.3/
重复运行会先校验；相同版本且服务正常时不重启助手或节点服务。
安装失败不报告成功，保留具体错误。只读检查：
  bash p95-priority-setup.sh --check
只解出源码及全部文件，目标目录须不存在，不安装：
  bash p95-priority-setup.sh --extract /root/p95-priority-source
重新构建需要 Python 3 和本地 Go 工具链：
  python3 build-release.py --output /root/p95-release

Ventoy tools 分发
-----------------
Ventoy alpha.47 起，完整 setup 不包含、不执行 P95 安装或检查功能。
中心 tools/p95-priority-setup.sh 与其他普通文件一样，下载校验后仅复制到
/root/ventoy-tools/p95-priority-setup.sh。需要时单独执行：
  bash /root/ventoy-tools/p95-priority-setup.sh
后续替换中心 tools 中的独立脚本，再运行新版完整 setup 可以同步文件；
已有助手不会自动升级、重启或卸载，仍需手动执行这个独立脚本。

压缩包部署（兼容原用法）
------------------------------------------
将 p95-priority-helper-1.0.3-linux-amd64.tar.gz 传到节点 /root，然后：

  tar -xzf /root/p95-priority-helper-1.0.3-linux-amd64.tar.gz -C /root
  bash /root/p95-priority-helper/install.sh fyhocsys

使用 sckocstp 的客户节点，最后一个参数改为 sckocstp。
无须改动中心。每个需要修正的测试节点各安装一次。
安装时先核对包的 SHA-256、节点服务、工具目录，再用工具自身的一个
线程实测 nice 19 -> 0 的权限；这个自检不启动硬件压测。
systemd 服务在开机后自动运行。节点没有 P95 任务时只等待。

运行效果检查
------------
先从网页启动短时间混合测试；可先用 P95 p95-no_m1 + memtester 50%
验证并发，确认正常后再恢复需要的内存比例。50% 只降低内存容量压力，
不是优先级修复，也不保证减少 memtester 进程数量。

测试开始、P95 工作线程建立后，在被测节点执行：

  /usr/local/sbin/p95-priority-fyhocsys status --seconds 10
  journalctl -u p95-priority-fyhocsys.service -n 30 --no-pager

若是 sckocstp，执行：
  /usr/local/sbin/p95-priority-sckocstp status --product sckocstp --seconds 10

status 只读取 /proc，不会启动或改变测试。关注同一个 step_directory：
  helper_service_active: true
  cpu_scheduling.cpu_controller_active: true
  p95.thread_nice_counts: 活跃 P95 工作线程应以 "0" 为主；
    若持续有 "19"，检查 helper 日志。偶尔新建线程可能赶上采样瞬间。
  p95.average_cpu_cores: P95 在这 10 秒内实际消耗的平均 CPU 核数。
  memtester.average_cpu_cores: memtester 同期实际消耗的平均 CPU 核数。
  p95.threads_with_cpu_progress: 采样期间 CPU 时间有增长的 P95 线程数。
  both_made_cpu_progress: true 表示同一步中两类程序在采样窗口都获得了 CPU。
  processes_present_for_whole_sample: 两次采样都在的进程数；若步骤切换，重采。

64 核机器中，average_cpu_cores=25 表示平均消耗约 25 个逻辑 CPU 的时间，
不是 25%。两个工具正常共享同一组 CPU，不要求各自同时占满 64 核。
两者都获得 CPU 时间也不等于算法测试通过。还需查看 P95 的 Test 1/2/3
及 completed ... tests 是否推进、有无 FATAL ERROR，以及 memtester
各项测试的输出。若 nice 已是 0，但 P95 仍无 CPU 时间，需要继续查
亲和性、cgroup 限额、内存等待或 worker 错误，不能宣称完全修复。
status 同时列出 cpu_cgroups、autogroups 和 sched_autogroup_enabled。
若负载处于 CPU cgroup 根组、系统启用了 autogroup，每次 setsid 还可能
导致不同任务组之间分配不均；仅把 nice 改成 0 不保证解决组间配额问题。
需要结合实际 CPU 核数评估，不能只看到 both_made_cpu_progress=true 就
断言两类负载已经获得合理份额。1.0.3 在助手 unit 中请求默认 CPU 权重，
不关闭全机 autogroup，不修改节点 unit、CPU 亲和性、实时策略或测试配置。
若定制 Slice/DisableControllers 阻止节点 CPU 分组，检查会明确失败。

steps: [] 表示采样时没有识别到目标负载，不是“修复成功”。
状态中不把不同步骤的 P95 和 memtester 合并为一个并发成功结果。

停用与恢复
----------
  systemctl stop p95-priority-fyhocsys.service
  systemctl disable p95-priority-fyhocsys.service

正常停止会核对进程和线程的启动时间、路径及 cgroup，将本工具调整过、
目前仍为 nice 0 的存活线程恢复到原先的 nice 19。
不会停止压测，也不会改动后来由其他程序设成其他 nice 值的线程。
CPU 控制器的启停由 systemd 管理；助手停止后，若没有其他单元请求它，
原先未启用的 CPU 控制器会恢复关闭。不会删除其他服务的 CPU 配置。
如果停止日志显示恢复失败，停止服务后重试：
  /usr/local/sbin/p95-priority-fyhocsys restore

记录保存在 /run/p95-priority-fyhocsys/state.json，目录 root:root 0700。
记录写入发生在优先级变更之前；记录含本次开机 ID 和进程/线程启动时间，
旧 PID 的记录不会用于一个已复用的新进程。
Linux 的 setpriority 接口只接收数值 TID，因此身份校验与系统调用
之间仍存在极短竞态窗口；不宣称它是内核级原子身份绑定。
机器重启后原有进程已结束，运行时记录也自然失效。

独立文件
--------
  /usr/local/sbin/p95-priority-fyhocsys
  /etc/systemd/system/p95-priority-fyhocsys.service
  /run/p95-priority-fyhocsys/
  /var/backups/p95-priority-helper/
sckocstp 用对应的产品名。卸载前先停用并确认恢复完成，再删除该产品的
上述独立程序和 unit；不要删除 /etc/fyhocsys、/var/lib/fyhocsys 或 /opt/fyhocsys。

验证范围
--------
源码和测试随包提供在 src/。本地使用 Windows Go 工具链执行 /proc
夹具测试及 Linux 静态交叉构建，并用 Git Bash 检查脚本语法、离线解包
和校验流程；构建本身不启动压力测试。
2026-10-30 在 Rocky 9.2 / 9995WX 的 192 逻辑 CPU 节点完成一次
33 秒的隔离对比测试，使用已安装的 mprime/memtester 和 1% 内存。
修复后 P95 与 memtester 分别有 192 个 R 状态工作线程，两者都取得
CPU 时间进展，平均分别使用约 97.4 和 93.5 个逻辑 CPU 的计算时间。
这 384 个可运行任务共享 192 个逻辑 CPU；load average 是时间平均值，
短测不能要求其立即等于 384，也不能用它代替各工具的进展检查。
本次未通过中心网页启动批次，也未完成长时间算法/硬件稳定性验收。
安装脚本的 selfcheck 会在目标 Linux 上实际验证线程优先级系统调用，
安装成功仍需按目标平台完成混合测试实测。

依据
----
已核对的 mprime 30.19b20 官方源代码：
  commonc.c: Priority 默认 1。
  commonb.c: EnableSetPriority 默认开启，压测线程也调用 SetPriority。
  linux64/os_routines.c: (8-Priority)*19/7；Priority 1 -> nice 19。
  官方来源：https://download.mersenne.ca/gimps/v30/30.19/
Linux nice 是逐线程属性：
  https://man7.org/linux/man-pages/man2/setpriority.2.html
调度权重设计：
  https://www.kernel.org/doc/html/latest/scheduler/sched-nice-design.html
CPU 控制器及 autogroup：
  https://www.freedesktop.org/software/systemd/man/latest/systemd.resource-control.html
  https://www.man7.org/linux/man-pages/man7/sched.7.html
Linux load average：
  https://www.man7.org/linux/man-pages/man5/proc_loadavg.5.html
