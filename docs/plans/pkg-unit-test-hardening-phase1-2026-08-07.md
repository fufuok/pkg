# pkg 核心包单元测试深化一期方案

> 方案日期: 2026-08-07
> pkg 基线: `feature/yf/260803_整合utils包` @ `0f77364ab739ab84edc71a905499e37705dac394`
> 主题边界: 单元测试保护网与测试能力建设, 不属于 pkg 瘦身或生命周期重构方案
> 审查范围: pkg 源码、迁移后的 utils 及 XY.NodeAgent、xy-data-router、xy-data-plugins、XY.IPIP-TXTX 四个生产项目
> 实施状态: P0-A 至 P0-D 已完成代码、五维审查和双平台门禁; 一期整体验收仍受 NodeAgent Windows 既有路径基线与长期跨仓 CI 条件约束

## 1. 最终建议摘要

第一阶段应先建立 `config`、`master`、`common` 的确定性行为保护网, 不应同时引入 `StartE/RunE/StopE`、loader、controller 或 runtime 等生产架构重构.

推荐执行边界如下:

1. 默认只新增或修改 `*_test.go`、测试数据和 pkg 自身 CI 门禁. 跨仓一次性执行器不进入 Git; 非测试源码只允许用于已有测试助手收敛或经不可测证据确认的未导出最小测试缝, 且不得改变正常应用启动路径.
2. 使用包内测试直接覆盖未导出逻辑, 使用包外测试冻结公共契约, 使用受控子进程隔离 `log.Fatal`、`os.Exit` 和当前无法等待的完整生命周期入口.
3. 新增测试的网络依赖使用本地 HTTP/UDP/RESP fixture, 文件依赖使用 `t.TempDir`. 现有 `common.InitTester` 触发公网 IP 探测的行为必须在第 0 步收敛, 不能作为稳定性门禁的默认前提.
4. 迁移后的 `utils` 当前覆盖率已经达到 88.1%, 一期只保持现有契约和稳定性门禁, 不为追求数字重复补低价值用例.
5. 若某个关键分支确实被硬编码时钟、休眠或外部命令阻断, 必须先提交不可测证据, 再单独评审未导出的最小测试缝. 不允许为了测试新增公共 API 或可变包级打桩入口.

因此, 一期不会改变四个生产项目的 import path、生产启动入口、Pipeline 接口、函数签名、配置结构、默认值或调用顺序. 推荐在 pkg 内部保持 `InitTester/StopTester` 签名不变并修正实现, 四个生产项目不需要配套修改生产代码或测试调用点.

复审后的决策分为三个层次, 避免把“方案可行”误写成“当前门禁已全部通过”:

- 方案可行性: Go. 范围、顺序、兼容约束和验收标准已经具备可执行性.
- 开工决策: Go. 可以立即从第 0 步开始修复测试污染、测试助手和门禁基线.
- P0 代码与 pkg 门禁: Go. `config/master/common` 核心行为矩阵和迁移后 utils 稳定性边界已经落地, 覆盖率达到 84.5%/52.7%/82.7%, 双平台普通、shuffle、`std_json`、vet、模块和分包 race 门禁通过.
- 一期整体验收状态: Conditional Go. 长期跨仓 CI 仍无法在当前 GitHub Actions 中取得四个生产项目, NodeAgent Windows 仍有既有 Unix 绝对路径断言失败; 因此不能宣称四项目双平台全绿.

## 2. 现状与判定依据

### 2.1 实测覆盖率

使用方案基线 `0f77364` 的 Git 已跟踪 Go 包执行 `go test -count=1 -cover` 的结果如下:

| 范围 | 覆盖率 | 判定 |
| --- | ---: | --- |
| 全模块加权 | 69.0% | 由合并 coverprofile 实测; 迁入工具包的高覆盖率明显抬高结果, 不能代表运行期核心质量 |
| `config` | 27.6% | 配置加载、环境合并、远端配置和失败边界覆盖不足 |
| `master` | 1.6% | Pipeline、reload、restart、watcher 和退出顺序几乎没有保护 |
| `common` | 5.0% | 日志、请求客户端、Redis、IP 和后台发送链路覆盖不足 |
| `crontab` | 69.0% | 已有较完整任务契约, 一期只验证与 master 的交互 |
| `json` / `logger` / `stats` | 0% | 是后续优先项, 但不扩大本期核心范围 |
| `utils` | 88.1% | 已具备较强基础保护, 不按统一目标继续堆覆盖率 |
| `xdaemon` / `xfile` | 68.3% / 88.2% | 已有真实子进程和真实文件测试, 遗留资源生命周期问题单独处理 |

第 0 步收口后的 2026-08-08 实测快照为 `config 62.0%`、`master 1.6%`、`common 13.0%`. `config` 的数值增长主要来自测试助手状态恢复契约, 只代表第 0 步保护网生效, 不代表配置加载、环境合并和失败边界矩阵已经完成. 后续仍按“状态隔离 -> config -> master -> common”推进, 并以必测行为完成度优先于覆盖率数字.

### 2.2 低覆盖不是唯一依据

一期优先级同时依据以下生产风险:

- `config` 决定全部生产项目的主配置、env、白名单、黑名单、日志和 Web 派生值.
- `master` 决定框架 Pipeline 与业务 Pipeline 的启动、热加载和逆序停止.
- `common` 在启动阶段初始化日志、IP、HTTP 客户端、协程池和日志发送 goroutine, 被业务热路径广泛调用.
- 四个生产项目都通过 `master.Register` 注册业务模块并最终调用 `master.Main`.
- DataRouter 的真实业务测试直接使用 `config.InitTester` 和 `common.InitTester`, 说明测试助手本身已经是下游测试兼容面.

### 2.3 当前可测性阻力

| 阻力 | 真实位置 | 一期处理方式 |
| --- | --- | --- |
| 全局可变状态 | `config/default.go`, `config/config.go`, `master/pipeline.go`, `common/*.go` | 在同包测试中集中快照并用 `t.Cleanup` 恢复, 相关用例禁止并行 |
| 深层进程退出 | `config/init.go`, `master/init.go`, Web engine | 子进程 helper 测试验证退出码和日志, 不在一期改变退出语义 |
| 永久 goroutine / Ticker | `master/init.go:mainScheduler`, `master/watcher.go:startWatcher`, `common/log_sender.go` | 优先测试可终止 helper; 无法回收的入口不在同进程直接启动 |
| 条件可终止 goroutine | `common/redis.go:ClockOffsetChanRedis`, `master/remote.go:GetRemoteConf` | 使用可控周期和持续消费验证最终取消; 当前 ticker 等待和 channel 发送不响应取消, 不承诺即时退出 |
| 网络与外部服务 | 配置远端获取、IP、日志上报、Redis、NTP | 使用 `httptest`, 本地 UDP fixture 和内存 Redis, 禁止公网 |
| 文件与系统命令 | watcher、deb、日志文件 | 使用临时目录和子进程; `apt/dpkg` 成功路径不作为 Windows 单元测试前提 |
| 测试助手会启动真实组件 | `config/common/crontab/test_only.go` | 保持签名, 先让 `common.InitTester` 自包含且默认不启动公网 IP/永久 sender; 完整 `M.Start/Stop` 放到子进程验证 |

### 2.4 审查发现的落地前置问题

以下问题在新增大批测试前必须先处理:

1. `master/ntpdate_test.go` 会关闭包级 `ntpFirstDoneChan` 且不恢复, 实测 `go test -count=2 -shuffle=on ./master` 第二轮失败.
2. DataRouter 的 `TestProcessForwardClone` 只调用 `common.InitTester`; 单独运行时因 `config.Config()` 为 nil 在 `common.loadLogger` panic, 说明该测试依赖其他测试先初始化配置.
3. `common.InitTester` 当前直接执行完整 `M.Start`, 会启动公网 IP 和日志 sender goroutine; `StopTester` 不等待两者结束.
4. 当前忽略的 `tmp/demo-hasher` 会进入 `go list ./...`, 导致 canonical `go test ./...` 因缺少 `go-pretty` 失败. pkg 门禁必须在 clean worktree 中运行, 或使用固定的 tracked-package 脚本.
5. 四个生产项目当前通过各自 `go.work` replace 到本地 pkg, 而 `go.mod` 版本尚不能独立编译迁移后的 import. 门禁必须显式校验实际 pkg 来源.
6. clean WSL Go 1.26 race 基线不是全绿: `config`、`master` 通过; `common` 即使 `-run '^$'` 也在 PASS 后 segmentation fault; `crontab` 在 `job_test.go` 的未同步计数器上报告真实 data race. 附件只推测潜在竞态, 没有覆盖这两个已实测阻断.
7. 原门禁同时要求临时 `go.work use` pkg clean worktree和校验 `Replace.Dir`, 两者语义冲突. pkg 作为 workspace `use` 模块时应校验 `Main/Dir`; 本期统一改为只 `use` 生产项目并 `replace github.com/fufuok/pkg => <clean-worktree>`, 再校验 `Replace.Dir`.
8. `config/common/crontab/test_only.go` 没有 build tag, `go list` 确认它们属于普通 `GoFiles` 并进入生产构建. 收敛助手会改变生产二进制内容, 但四项目生产调用链不执行这些函数; 因此必须独立提交、保持签名并执行非测试代码覆盖与下游兼容门禁.
9. `common/initPool` 使用 ants v1.11.9 的 `SetDefaultPool`; 锁定版本源码确认该函数替换默认池后会立即 `Release` 旧池. 因此测试助手不能直接调用 `initPool` 后再恢复旧池, 否则恢复的是已关闭资源. pool 必须使用独立的构造复用与测试侧 swap 所有权协议.

## 3. 一期目标与非目标

### 3.1 目标

1. 冻结四个生产项目真正依赖的公共契约和启动顺序.
2. 让核心成功、失败、取消和可终止清理分支可以在本地重复验证.
3. 建立统一的全局状态恢复、子进程和本地 fixture 模式; 对无法等待的现有生命周期明确使用进程隔离, 不虚假声明同进程零泄漏.
4. 让后续 AI 或开发者修改核心代码时能从测试名称、表格用例和失败信息直接判断行为边界.
5. 将覆盖率从结果指标改为风险覆盖指标, 防止用简单 getter 或重复表格用例抬高数字.

### 3.2 非目标

- 不新增 `config.StartE` 或 `master.StartE/RunE/StopE`.
- 不重构 `config` loader、`master` controller/registry 或 `common` runtime.
- 不改变 `Pipeline` 接口、Stage 常量、注册 API 或业务模块实现.
- 不修复配置半发布、启动失败回滚、停止错误汇总或重复 Start/Stop 幂等性.
- 不改变 Fatal、Exit、默认公网 IP、日志发送协议、批量阈值和重试策略.
- 不以删除测试助手、迁移包路径或清理历史 API 为一期目标.

这些事项可能有独立价值, 但会改变生产行为或架构边界, 必须在测试保护网建立后另立方案.

## 4. 测试架构

### 4.1 三层测试

| 层次 | 位置 | 责任 |
| --- | --- | --- |
| 包内单元测试 | `package config/master/common` | 覆盖未导出解析、顺序、状态转换和错误分支 |
| 包外契约测试 | `package xxx_test` | 只使用公开 API, 防止测试依赖内部实现 |
| 下游兼容测试 | 四个生产项目 | 验证真实 main 注册链、业务 Pipeline 和测试助手调用保持可编译可运行 |

### 4.2 全局状态隔离

每个核心包新增一个仅存在于 `*_test.go` 的状态快照助手, 必须遵守以下约束:

- 快照测试会修改的全部包级变量, 不能只恢复当前断言涉及的一个字段.
- 注册 `t.Cleanup` 后再修改状态, 防止 `Fatalf` 或提前返回泄漏环境.
- 环境变量使用 `t.Setenv`, 文件使用 `t.TempDir`.
- 修改包级状态、默认 flag、全局 logger 或 Pipeline 列表的测试不得调用 `t.Parallel`.
- 状态恢复后执行第二次等价用例, 证明用例不依赖执行顺序.
- `master` 快照必须包含 `ntpFirstDoneChan`、`ntpCancel`、`ntpName` 和 Pipeline 列表; 每轮创建新 channel, 禁止复用已关闭 channel.

### 4.3 测试助手第 0 步

在补覆盖率前先保持现有签名并收敛测试助手:

1. `common.InitTester` 在 `config.Config()==nil` 时自行建立测试配置, `common.StopTester` 只清理本次助手拥有的配置、pool、IP 和其他资源状态, 不无条件调用完整 `M.Stop`.
2. `common.InitTester` 使用确定性的测试 IP 和最小运行组件, 默认不启动公网 IP 探测与永久日志 sender. 四项目扫描未发现测试依赖 `LogChan` 或真实公网 IP; DataRouter 路径至少依赖可用的默认 pool, 其余组件必须以真实调用证据决定.
3. `config.InitTester/StopTester` 保存并恢复助手实际触达的全部包级状态、`mainConf` 指针和受影响环境变量, 包括初始化标记、默认路径、派生配置、名单与 env 状态, 禁止只恢复当前三个显式赋值. 助手只承诺串行 `Init-Stop-Init-Stop`, 不支持嵌套或并发调用, 测试必须显式冻结该边界.
4. pool 是特殊所有权资源. 抽取未导出的 pool 构造函数供生产和助手复用; 生产 `initPool` 继续调用 `ants.SetDefaultPool`, 保持替换并释放旧池的现有语义. `common.InitTester` 必须用 `ants.SwapDefaultAntsPool` 保存旧池, `common.StopTester` 先 swap 恢复旧池, 再释放 swap 返回的助手池. 该 API 非线程安全, 助手只支持串行调用; 恢复后必须断言 `ants.Submit` 仍能执行, 防止把已关闭的旧池装回默认位置.
5. 除 pool 外, 助手的最小组件必须调用 `M.Start` 已使用的内部组件函数, 必要时只抽取未导出的共享初始化核心, 不得复制实现. `M.Start` 的组件集合和顺序保持不变; 助手用显式后置状态断言防止共享组件行为漂移.
6. 不在同一进程直接执行 `M.Start` 后与助手做“全状态等价”比较, 因为这会重新引入公网和不可等待 goroutine. 完整生产生命周期只通过受控子进程 smoke 验证; 助手用契约测试验证其明确承诺的最小状态.
7. 助手仍位于原包、保持无参数和无返回值签名. `test_only.go` 是生产编译文件, 其改动必须独立提交、满足变更行覆盖门槛并通过四项目兼容门禁.
8. 完整 `common.M.Start/Stop` 不由测试助手代替, 使用子进程测试现有生产路径.

第 0 步已按以上约束落地. `config.InitTester/StopTester` 现在使用临时目录和内存配置, 只接管实际触达的环境键, 能区分“不存在”和“存在但为空”, 并恢复配置指针、路径、名单、env 跟踪、密钥派生值、Node 状态及 Alarm 开关. `common.InitTester/StopTester` 在需要时拥有并清理 config, 默认使用确定性 IP 和独立 helper pool, 不启动公网 IP、文件 logger、HTTP 客户端或永久日志 sender. 助手只释放自己创建的 pool, 生命周期内由调用方替换的 pool 保持可用. 串行重复生命周期已冻结, 嵌套调用按约定 panic.

### 4.4 子进程测试

对 `log.Fatal`、`os.Exit` 和无法等待的完整 `common.M.Start/Stop` 使用标准子进程 helper 模式:

1. 父测试通过环境变量选择 helper 场景.
2. 子进程只执行一个目标入口并输出稳定诊断.
3. 父测试断言退出码、关键错误上下文和最大执行时间.
4. 子进程覆盖完整 common 生命周期时, 临时替换并恢复 `http.DefaultTransport`, 把公网 URL 重定向到本地 fixture; 父进程只验证可观察结果和受控退出.
5. 不断言时间戳、临时路径或完整日志行, 避免脆弱测试.

一期通过测试描述当前退出语义, 不把退出函数替换成可变包级函数.

### 4.5 本地 fixture

- HTTP: `httptest.Server`, 显式覆盖成功、非 2xx、无效 JSON、超时和取消.
- UDP/NTP: 复用现有本地 UDP fixture 模式.
- Redis: 一期优先在 `_test.go` 内实现满足 TIME、SET NX 和 PTTL 的最小 RESP fixture. 若引入 miniredis 等测试依赖, 必须明确允许修改 `go.mod/go.sum` 并单独审查依赖.
- 文件: 使用真实临时文件验证 MD5、配置优先级和 watcher 变化.
- 日志: 使用 `bytes.Buffer` 或临时日志文件, 解析 JSON 字段, 不匹配整行文本.

## 5. 分包实施清单

### 5.1 `config`: P0-A

必须覆盖的行为矩阵:

1. 默认配置文件解析优先级: 显式 `ConfigFile`、bootstrap env、二进制名和默认后缀.
2. `AppConfigBody` 与真实文件两条加载路径.
3. env 文件新增、覆盖、移除后的环境清理.
4. `SYSConf` 时长按三类验证: 空值使用默认值, 低于最小值静默回退, 格式错误返回带上下文的 error.
5. 日志配置 env 覆盖、批量阈值和派生 duration/bytes.
6. Web 主配置与 group 归一化, 特别是端口不继承契约.
7. 白名单、黑名单内联与文件加载, 非法 CIDR 的错误上下文.
8. `FilesConf` 的 secret、method、path、interval 和 random wait.
9. NodeInfo 正常、备份和本地 HTTP IP 获取分支.
10. `GetDataSource*` 的请求、响应、shouldUpdate 和错误分支.
11. `InitTester/StopTester` 的签名、自包含初始化、重复执行和状态恢复契约.

失败路径测试不得把当前“部分全局值已被改写”固化为期望行为. 若发现失败后状态被部分发布, 只记录为独立缺陷, 不在一期顺带修复.

### 5.2 `master`: P0-B

必须覆盖的行为矩阵:

1. 先修复现有 NTP 测试对关闭 channel 的污染, 证明 `-count=2 -shuffle=on` 通过后再增加其他 master 测试.
2. `Register` 只验证 ConfigStage/MainStage, `RegisterWithContext` 只验证 RemoteStage, 获取结果是快照而非原切片. `Register(RemoteStage, Pipeline)` 当前静默 no-op 仅记录为 legacy 行为, 不冻结为长期公共契约.
3. 框架 Pipeline 与业务 Pipeline 的真实顺序:
   - Config 启动: `config.M -> common.M -> 业务 ConfigStage`.
   - Main 启动: `crontab.M -> 业务 MainStage -> addons`.
   - Stop: Config 与 Main 合并后整体逆序.
4. 分别验证私有 `runtimeConfigPipeline` 和 `runtimePipeline` 按各自注册顺序执行, 单个错误记录后继续后续 Pipeline; 不描述不存在的统一 `master.Runtime` 入口.
5. `Watcher.Start/Stop` 只验证 xsync.Map 注册/删除, Always、自定义 hash 和文件内容变化; `startWatcher` 永久 loop 不在一期同进程启动, 改测 `mainWatcher/appWatcher/configWatcher` 等可终止 helper.
6. 分别验证主程序二进制 MD5 重启链路, 以及主配置、白黑名单、env、extra watcher 和 NodeInfoFile 的配置 MD5 组合链路; 两条链路不得混为一个 hash.
7. 配置未变化、加载失败、加载成功的 watcher 分支.
8. deb canary 的 0/边界/100 阈值, 不执行真实安装命令.
9. RemoteStage 的禁用配置和最终 context 取消分支; 当前两段 `time.Sleep` 不可中断, 不承诺即时取消.
10. `Main` 的 version 快速返回分支.
11. Fatal/Exit 相关失败路径使用子进程测试, 同进程不启动永久 scheduler.

一期不承诺覆盖 `apt/dpkg` 真实成功路径、daemon 重启循环和永久 watcher loop. 这些属于集成测试或需要后续最小依赖缝.

`getPipelines/getPipelinesWithContext` 当前在加锁前读取共享切片头, 与并发注册存在真实竞态窗口. 四项目均在 `init` 阶段完成注册, 一期不新增“运行期并发注册安全”契约测试; 该竞态作为独立生产缺陷登记, 后续可用不改公共 API 的锁范围修复和定向 race 测试处理.

### 5.3 `common`: P0-C

必须覆盖的行为矩阵:

1. Logger 未初始化时返回 nop logger, 初始化后 level、caller、字段名和采样配置正确.
2. Alarm writer 的 level 过滤、AlarmOn 开关、JSON 生成和异步任务提交.
3. HTTP 客户端的 user-agent、timeout、retry、debug 开关和上传/下载 body 隐藏策略.
4. `InitRedisDB`, `TryLock`, `LockKeyTTL` 的未初始化、成功和过期行为.
5. Redis 时钟偏移 channel 的 nil、预取消、最小周期和 channel 关闭. `ClockOffsetChanRedis` 仅在消费方持续读取时条件可终止: ticker 等待和向容量 1 channel 发送均未 `select ctx.Done()`, 因此测试必须使用可控周期并持续消费, 不得声称取消即时生效或任意消费方式下无泄漏.
6. `myip` 已有本地 fixture 继续覆盖内外网 IP 行为; `common.initServerIP` 的组合与 fallback 只在受控子进程验证, 不直接访问公网.
7. 日志 sender 的空 API、批次数量、批量字节、定时 flush 和 HTTP 失败分支.
8. pool 构造、生产安装、助手 swap/save/restore/release、提交和 panic handler; 助手恢复旧池后必须再次成功提交任务.
9. `M.Runtime` 执行后 logger 与 request 的可观察状态; 不为不可观察的内部调用顺序增加脆弱断言.
10. `InitTester/StopTester` 与 DataRouter 现有调用方式兼容, 且每个相关测试可以独立运行.

日志 sender 的永久 goroutine 在当前实现中无法可靠等待. 一期只在同进程覆盖 `PostLog/postLog` 等可终止单元, 完整生命周期使用子进程隔离并记录“未证明同进程零泄漏”的残余风险.

### 5.4 迁移后的 utils: P0-D

一期策略是“守住而非扩张”:

- 保留现有普通、`std_json`、race、shuffle 和双平台门禁.
- 对已知高风险包按行为而不是覆盖率补缺口, 例如 xdaemon 日志句柄生命周期和 xfile 边界; 缺陷修复必须独立提交.
- 不为 88.1% 到更高数字补 getter、别名或标准库透传测试.
- 不恢复已经归档到 `tmp/_utils-phase1-migration` 的一次性来源/迁移门禁到公共模块.

## 6. 生产调用兼容约束

一期必须维持以下源码级契约:

```go
type Pipeline interface {
	Start() error
	Runtime() error
	Stop() error
}

func Register(stage Stage, sf ...Pipeline)
func RegisterWithContext(stage Stage, sf ...ContextFunc)
func Main()
func Run()
func Start()
func Stop()
func config.InitTester()
func config.StopTester()
func common.InitTester()
func common.StopTester()
```

以下内容不得变化:

- 四个项目 `init` 中的注册代码.
- 四个项目 `main` 中的 `master.Main()`.
- 业务 `M.Start/Runtime/Stop` 方法签名.
- 配置结构、JSON tag、默认值和环境变量名.
- 正常启动、热加载和停止时的 Pipeline 顺序.
- DataRouter 测试对 `config/common` 测试助手的调用方式.

## 7. 覆盖率与质量门槛

一期不采用“所有包 80%”的统一目标. 建议门槛如下:

| 项目 | 一期门槛 | 说明 |
| --- | ---: | --- |
| `config` | >= 60% | 解析和派生逻辑可在不改生产代码前提下深入覆盖 |
| `master` | >= 35% | 排除真实 daemon、apt/dpkg 和永久 loop 后的现实目标 |
| `common` | >= 40% | 优先覆盖可终止单元和公开运行契约 |
| 例外修改的非测试 Go 代码 | 变更行 >= 80% | 仅在增加最小测试缝或收敛测试助手时适用; 纯测试提交为 N/A |
| 必测行为矩阵 | 100% 完成 | 优先级高于包总覆盖率 |

`config >= 65%`、`master >= 60%`、`common >= 55%` 可以作为后续阶段目标, 但通常需要重构不可终止生命周期和硬编码依赖, 不应冒充纯单元测试一期的验收条件.

### 7.1 race 门禁决策

race 失败不能用“只检查新增路径”做泛化豁免, 因为一次包测试无法可靠区分既有路径和新增路径. 本期采用以下可审计规则:

1. 第 0 步先保存每个包的独立 race 基线、完整诊断和运行环境. 当前 Linux 实测为 `config/master` 通过, `common` 测试进程退出时 segmentation fault, `crontab` 现有测试报告 data race.
2. 新增或修改的测试必须独立执行 `-race` 且零诊断; 不得以“基线已有问题”为新增 race 开脱.
3. `common` 的退出崩溃和 `crontab` 的测试计数器 race 必须分别作为阻断缺陷闭环. 若需要修改生产 Go 文件, 使用独立缺陷提交并重新做生产影响审查.
4. 最终验收仍要求 `config/common/master/crontab` 包级 race 全绿. 临时豁免只能逐项记录具体测试、完整诊断、负责人和失效条件, 且有豁免时不得宣称 race 门禁通过.
5. 一期不主动构造未承诺的运行期并发 `Register` 场景; `getPipelines` 锁外读竞态另立缺陷处理, 不把当前行为固化为安全契约.

## 8. 验证门禁

### 8.1 pkg

1. 优先在 clean checkout/独立 worktree 执行标准 `go test -count=1 ./...`、`go test -count=1 -tags=std_json ./...` 和 `go vet ./...`.
2. 当前开发工作区若保留被忽略的 `tmp`, 必须使用仓库脚本从 `git ls-files '*.go'` 派生 tracked package 列表; 不得手工维护列表或把失败的 `go test ./...` 宣称为通过.
3. 执行 `go mod verify` 和 `go mod tidy -diff`.
4. 先要求现有核心测试通过 `-shuffle=on -count=2`, 再把新增核心矩阵提升到 `-shuffle=on -count=50`.
5. Linux 对 `config/common/master/crontab` 分包执行 `-race`, 避免一个包失败掩盖其他包诊断. 第 0 步先处理 7.1 节记录的 `common/crontab` 基线; 最终四包必须全部通过.
6. Windows 与 Ubuntu Go 1.26.x 双平台执行.
7. 新增测试不得访问公网或依赖执行顺序; 无法等待的既有生命周期只允许在子进程中运行, 不将子进程退出等同于同进程 goroutine 清理证明.

### 8.2 四个生产项目

四个项目分别使用一个位于所有仓库之外的临时 `go.work`. 该文件只 `use` 当前生产项目 `src`, 并通过 `replace github.com/fufuok/pkg => <指定 pkg clean worktree>` 固定 pkg 来源; 执行命令时显式设置 `GOWORK=<临时文件绝对路径>` 和 `-mod=readonly`. 不采用“同时 use pkg 却校验 Replace.Dir”的混合协议. 验证前必须记录:

- 生产项目 HEAD、tree 和完整工作树摘要.
- `go env GOWORK`.
- `go list -m -json github.com/fufuok/pkg` 的 Version、Replace.Dir.
- Replace.Dir 的 pkg HEAD、tree 和 Go 源码脏状态.

随后依次执行:

1. `go test -run=^$ -count=1 ./...`.
2. `go test -count=1 ./...`.
3. `go build ./...`.
4. `go vet ./...`.
5. DataRouter 的两个测试助手用例分别单独运行, 再执行 `-shuffle=on -count=50`; 只有完成第 0 步助手收敛后才可把结果视为密闭稳定性证据.
6. 验证后再次比对四项目 HEAD、tree 和完整工作树摘要, 必须零写入.

NodeAgent 当前 Windows 基线因 Unix 路径与 `filepath.IsAbs` 语义不一致而失败. 该既有失败不阻塞第 0 步和核心测试建设开工; 实施期间只允许以“同一测试、同一诊断”的 baseline/overlay 等价作为增量门禁, Ubuntu 必须全绿. 它阻塞最终宣称“四项目 Windows 全绿”: 最终验收前必须在 NodeAgent 独立修复, 或取得显式豁免并明确声明 Windows 未全绿.

## 9. 推荐提交顺序

1. `test: 修复核心测试重复与竞态基线`: 修复 NTP channel 污染和 `crontab` 测试计数器 race; 单独诊断 `common -race -run '^$'` 退出崩溃.
2. 验证阶段: 固定 clean worktree 和单项目临时 `go.work use + replace` 协议; 跨仓执行器只保留在 Git 忽略目录, 不形成 pkg 提交.
3. `test: 收敛核心包测试助手状态`.
4. `test: 建立核心包测试状态隔离工具`.
5. `test: 补齐配置加载与归一化契约`.
6. `test: 补齐主流程管线与监控契约`.
7. `test: 补齐公共运行能力契约`.
8. `test: 增加核心包稳定性与四项目门禁`.

每个提交必须独立通过定向测试, 不能等全部用例完成后一次性修复全局状态污染.

当前执行进度:

| 顺序 | 状态 | 证据 |
| --- | --- | --- |
| 1. 修复核心测试重复与竞态基线 | 已完成 | `1ff066f`; master 重复污染、crontab race 和 common race 退出基线已闭环 |
| 2. 固定核心包与四项目验证来源 | 已完成, 不入 Git | clean worktree、完整 `go.work`、`Replace.Dir` 和四仓零写入协议已实测; 执行器归档到 `tmp/_pkg-unit-test-hardening/coretestgate` |
| 3. 收敛核心包测试助手状态 | 已完成 | `5997c4a`、`23c446d`、`593b52a`; 三轮五维审查后 0 finding |
| 4. 建立核心包测试状态隔离工具 | 已完成 | `09792fb`; `config/master/common` 快照与 cleanup 契约通过阶段审查 |
| 5. 补齐 config 行为矩阵 | 已完成 | `63e9a77`; 最终覆盖率 84.5% |
| 6. 补齐 master 行为矩阵 | 已完成 | `4ac3e9a`; Pipeline、生命周期、watcher 和失败边界已冻结, 最终覆盖率 52.7% |
| 7. 补齐 common 行为矩阵 | 已完成 | `cc4cf33`; 日志、请求、Redis、sender、IP 和运行期契约已冻结, 最终覆盖率 82.7% |
| 8. 迁移 utils 与最终稳定门禁 | 已完成 P0 | `e7a4306` 稳定 NTP 本地超时 fixture; `1fb2692` 是 master race Skip 中间诊断提交, `9c0373c` 改用未导出事件工厂完成无 race 专项豁免收口; 跨仓执行器继续不入 Git |

## 10. 生产影响与改造收益

### 10.1 对现有生产的影响

- 正常生产启动和业务逻辑: 无变化.
- 公共 API 与函数签名: 无变化.
- 使用侧调用链: 无变化.
- 配置与默认值: 无变化.
- 性能、goroutine、网络和文件行为: 无变化.
- 测试助手内部行为: 允许变为自包含、可恢复且默认离线; 当前四项目只在测试文件使用这些入口. 由于 `test_only.go` 参与普通构建, 生产二进制内容会变化, 但四项目正常生产调用路径不会执行这些函数.
- 四项目生产源码改造: 不需要. NodeAgent 平台测试修复属于既有测试债务, 与 pkg 生产兼容无关.

新增测试会增加 CI 时间和测试维护成本. 通过定向矩阵、核心包门禁和不重复覆盖 utils 来控制总时长.

### 10.2 改造后的优势

- 修改配置或 Pipeline 时能立即看到真实生产顺序和兼容性回归.
- Fatal、Exit、网络和全局状态不再迫使测试跳过关键失败分支.
- 测试名称和表格用例形成可被 AI 准确读取的行为规格.
- 后续若实施生命周期或错误边界重构, 可以区分预期改进与意外回归.
- 四个生产项目的注册链和测试助手用法会成为长期兼容门禁.

### 10.3 逻辑是否变化

一期对正常生产逻辑的结论是“不变化”. 允许变化的只有测试助手内部初始化/恢复行为和既有测试状态污染; 其目的正是让每个测试可独立、离线、重复执行. 四项目生产调用链不经过这些助手.

若后续确需增加未导出的最小测试缝, 必须满足:

1. 默认实现仍调用当前同一个标准库或第三方函数.
2. 对外 API、默认依赖和调用顺序不变.
3. 生产路径新增间接层有基准或编译证据证明无可见影响.
4. 测试缝与对应测试同提交, 且四项目门禁通过.

## 11. 审查记录

### 11.1 审查结论

原始草案 As-Is 为 No-Go. 本文完成源码、四项目调用链和分阶段五维复审后, 方案设计、开工决策及 P0 代码验收均为 Go. 第 0 步、核心行为矩阵、迁移后 utils 稳定性和 pkg 双平台门禁已闭环; 一期整体验收仍为 Conditional Go, 因为长期跨仓 CI 和 NodeAgent Windows 既有路径基线尚未闭环.

覆盖率从方案基线提升到 P0 最终结果如下:

| 包 | 方案基线 `0f77364` | P0 最终 | 门槛 | 判定 |
| --- | ---: | ---: | ---: | --- |
| `config` | 27.6% | 84.5% | 60% | 通过 |
| `master` | 1.6% | 52.7% | 35% | 通过 |
| `common` | 5.0% | 82.7% | 40% | 通过 |

这些增量来自配置解析、Pipeline、watcher、logger、request、Redis 和可终止运行单元, 未新增公共 API.

### 11.2 四项目真实调用链

| 项目 | 当前审查 HEAD | 业务 ConfigStage | 业务 MainStage | RemoteStage | 测试助手 |
| --- | --- | --- | --- | --- | --- |
| XY.NodeAgent | `803a23369d21` | `conf.M` | `node.M -> metric.M -> web.M -> helper.M -> nodeops.M` | `nodeops.FetchRemoteConf` | 无 |
| xy-data-router | `e1e18a8105bc` | `conf.M -> es.M` | `model.M -> rdb.M -> datarouter.M -> tunnel.M -> cluster.M -> udp.M -> web.M -> helper.M` | `helper.FetchRemoteConf` | `config/common.InitTester/StopTester` |
| xy-data-plugins | `76b732604825` | `conf.M -> driver.M` | `datarouter.M -> onlineusers.M -> kpinetwork.M -> web.M -> helper.M` | `helper.FetchRemoteConf` | 无 |
| XY.IPIP-TXTX | `a4f4022f8cd6` | `conf.M` | `asn.M -> ipip.M -> ipdat.M -> web.M -> udp.M -> helper.M` | 无 | 无 |

框架实际在 ConfigStage 前置 `config.M -> common.M`, 在 MainStage 前置 `crontab.M` 并后置 `addons`; Stop 将 Config 与 Main 合并后整体逆序. 四项目均只在 `main` 调用 `master.Main`, 未直接调用 `master.Start/Run/Stop`, 未在生产代码实例化 `config.M/common.M`.

### 11.3 实际验证

- 方案基线 `0f77364` 的 tracked package 合并 coverprofile 全部通过, 总覆盖率 69.0%; 当时 `config/master/common/utils` 分别为 27.6%/1.6%/5.0%/88.1%.
- 四项目当前 `go.work` 均 replace 到本地 pkg. 当前入口包 compile-only 全部通过, DataRouter 正确初始化配置的测试助手用例通过.
- DataRouter `TestProcessForwardClone` 单独运行真实复现 nil pointer panic, 证明其依赖其他测试残留; 这是第 0 步必须消除的测试债务.
- DataPlugins/IPIP 使用外部临时 `go.work` 指向 pkg `0f77364`, compile/full test/build/vet 全部通过且生产仓库零写入.
- NodeAgent/DataRouter 使用本地 replace 时 compile/build/vet 通过, DataRouter 全量测试通过; NodeAgent Windows 全量测试只存在已确认的 Unix 绝对路径基线失败.
- clean WSL Go 1.26 分包 race 复验: `config`、`master` 通过; `common` 即使 `-run '^$'` 也在 PASS 后 segmentation fault; `crontab` 在 `job_test.go` 的测试计数器上报告 data race. 这两项已升级为第 0 步阻断, 不采用模糊 baseline 豁免.
- 当前 pkg 与四项目工作区均包含任务前已有的忽略文件或未提交修改. 本次审查没有写入四个生产仓库, 结论同时记录 HEAD 和工作树状态, 不把当前脏树冒充发布提交.
- 第 0 步修复后, Windows 的助手 50 次 shuffle、核心包重复测试和 vet 均通过; WSL2 Go 1.26.5 的 `config/common/crontab` 分包 race 均通过; clean pkg worktree 的普通/`std_json` 全量测试、vet 和模块门禁均通过.
- 第 0 步四项目 overlay 复验中, compile-only、build 和 vet 全部通过, DataRouter 两个助手用例各自及 50 次 shuffle 均通过, DataRouter/DataPlugins/IPIP 全量通过. NodeAgent Windows 全量只保留同一既有路径断言失败; 四仓 HEAD、索引树和 tracked diff 指纹前后一致.
- P0 最终代码 `9c0373c` 在 Windows/WSL2 通过普通、shuffle、`std_json`、vet 和五包分包 race, 无 race 专项豁免或诊断; JSON 输出仅有 `internal/ntp` 6 个默认离线的既有 opt-in 公网 smoke Skip. 四项目基于包含 `master/init.go` 生产增量的候选执行 compile-only/build/vet 全部通过, DataRouter/DataPlugins/IPIP 全量通过, 四仓状态前后一致; NodeAgent Windows 全量仍只保留同一既有路径断言失败.

### 11.4 最终判定

- 方案可行性: Go, 文档已达到可开始实施标准.
- 当前实施状态: P0-A 至 P0-D Go, 7 个代码提交均完成阶段审查、真实性核验和复审; `1fb2692` 的 Skip 方案已被 `9c0373c` 取代.
- pkg 最终门禁: Go, Windows/WSL2 clean worktree 的普通、两轮 shuffle、`std_json`、vet 和模块门禁通过; WSL2 五个相关包分包 race 通过.
- 一期整体验收状态: Conditional Go, NodeAgent Windows 既有路径测试和长期跨仓 CI 未闭环, 不得宣称四项目双平台全绿.
- 使用侧调用链: 不变.
- 公共函数签名和 Pipeline 接口: 不变.
- 正常生产逻辑: 不变.
- 测试侧逻辑: 测试助手已变为自包含、离线和可恢复; 现有依赖测试顺序的行为不保留.
- 后续条件: 跨仓协议继续用于阶段验收, 只有在 CI 能安全取得四个项目时才建设长期自动门禁. P1 优先处理默认 HTTP debug dump 的正文脱敏边界, 再补 `stats/json/logger` 等低覆盖运行包.

### 11.5 附件审查建议采纳决策

| 报告内容 | 真实性 | 决策 | 原因 |
| --- | --- | --- | --- |
| Redis 不是永久 goroutine | 部分属实 | 调整后采纳 | 有 ctx 退出路径, 但 ticker 等待和 channel 发送不响应取消, 只有持续消费时条件可终止 |
| Watcher.Start/Stop 不启动 loop | 属实 | 采纳 | 永久 loop 在 `startWatcher`, Start/Stop 可直接同进程测试 |
| 主程序与配置 MD5 是双链路 | 属实 | 采纳 | 分别对应 restart 与 reload, 不应混写 |
| master Runtime 是两条私有链 | 属实 | 采纳 | 使用真实函数名可避免测试错误入口 |
| race 只约束新增路径 | 依据不足 | 拒绝 | 无法审计区分; 改为分包基线、阻断缺陷和最终全绿 |
| 冻结 RemoteStage Pipeline 静默丢弃 | 事实属实、建议不当 | 拒绝 | 四项目使用 `RegisterWithContext`; no-op 只记 legacy, 不固化误用 |
| test_only.go 进入生产二进制 | 属实 | 采纳 | 明确生产二进制内容变化, 但真实生产调用路径不变 |
| 助手与 M.Start 做等价断言 | 方向合理、方法不当 | 调整后采纳 | 普通组件复用相同内部 init 函数; pool 只复用未导出构造函数, 生产保持 `SetDefaultPool`, 助手使用 `SwapDefaultAntsPool` 保存和恢复旧池; 完整 M.Start 放子进程 |
| NodeAgent Windows 失败先修复 | 属实 | 分阶段采纳 | 不阻塞开工, 但阻塞最终宣称 Windows 全绿 |
| 方案当前无条件 Go | 不成立 | 拒绝 | 方案可开工, 当前代码仍有助手、workspace、race 和平台门禁未闭环 |

## 12. 第 0 步实施与复审结论

### 12.1 落地内容

- `master/ntpdate_test.go` 不再污染包级 channel; `crontab/job_test.go` 的测试计数器已改为同步状态; `common` race 退出崩溃随最小助手路径收敛而消除.
- `config.InitTester/StopTester` 与 `common.InitTester/StopTester` 保持原签名, 实现自包含、默认离线、状态恢复和精确资源所有权.
- 生产 `common.initPool` 继续使用 `SetDefaultPool`; 助手使用 `SwapDefaultAntsPool` 保存和恢复调用方 pool. 生产安装语义、正常启动组件集合与顺序均未改变.
- 第 0 步修改的非测试 Go 语句覆盖率为 `118/128`, 即 92.2%, 高于 80% 门槛. 当前包覆盖率为 `config 62.0%`、`common 13.0%`; 后者尚未达到一期最终 40% 目标, 应在 P0-C 阶段继续补齐.

### 12.2 审查闭环

| 轮次 | 审查提交 | 结论 | 决策 |
| --- | --- | --- | --- |
| 初审 | `5997c4a` | 8 条 P2 | 全部真实, 修复环境所有权、pool 身份、调用方 config、cleanup、完整状态和临时目录证明 |
| 第一轮复审 | `23c446d` | 原 8 条关闭, 新增 2 条 P2 | 两条均真实, 修复 helper pool 精确所有权并补齐标量状态断言 |
| 最终复审 | `593b52a` | 五维 0 finding | 无需继续修复, 第 0 步达到可落地验收标准 |

最终报告为 `.codereview/reports/cr_MSJ2LGD84hUbz9FDUA4.md`, sidecar 的 `findings_count` 为 0. 审查未发现需要通过 Context7 判定的新第三方 API; ants 语义由锁定版本源码和定向测试验证.

### 12.3 生产影响与逻辑变化

- 公共 API、函数签名、配置结构、默认值、Pipeline、四项目生产注册链和启动调用顺序均无变化.
- `test_only.go` 仍参与普通生产编译, 因此二进制内容发生变化; 四项目生产源码不调用测试助手, 正常生产执行路径、网络、goroutine 和文件行为不变.
- 逻辑变化严格限定在测试助手和既有测试: 助手从依赖调用顺序、触发公网副作用和不完整清理, 改为独立、离线、可重复并恢复调用方状态. 依赖其他测试残留的旧行为不作为兼容契约保留.
- 改造后每个下游助手用例可独立运行, helper pool 和调用方资源所有权可被测试证明, 全局状态污染和重复/race 门禁由稳定用例拦截.

### 12.4 阶段判定与下一步

- 第 0 步验收: Go.
- 一期整体验收: Conditional Go.
- 下一步: 执行推荐顺序第 4 项“建立核心包测试状态隔离工具”. 只在 `config/master/common` 的 `*_test.go` 中建立完整快照与 `t.Cleanup` 恢复协议, 每包独立验证重复、shuffle 和 race 后再进入行为矩阵.

## 13. 验证来源阶段收口

### 13.1 边界决策

- clean pkg worktree、仓外单项目 `go.work use + replace`、完整 `go.work` 内容校验、`Replace.Dir` 固定和四项目零写入协议均已通过真实 compile-only、build、vet 验证.
- 阶段执行器属于 `package main`, 四个生产项目和 pkg 业务包均不导入. 它不会进入生产依赖闭包或二进制, 但会扩大 pkg 源码发布面并被仓内 `go test ./...`、`go vet ./...` 编译.
- 执行器净增加约 1,998 行, 当前 GitHub Actions 无法取得四个生产仓库且没有调用入口, 因此保留为 Git tracked 工具不能形成真实自动门禁, 维护成本高于长期收益.
- 最终决策是不把跨仓执行器提交到 pkg. 完整实现保留在 Git 忽略且 Go 工具链跳过的 `tmp/_pkg-unit-test-hardening/coretestgate`, 供一期后续阶段本地复验.

### 13.2 影响与下一步

- 当前 Git 提交只保留 pkg 自身源码、测试和方案文档; 公共 API、配置、Pipeline、生产调用链及四项目代码均无变化.
- 跨仓验证协议仍是每阶段验收要求, 但其执行证据不冒充 pkg 自身单元测试覆盖.
- 后续状态隔离与 P0 行为矩阵已按完整快照、精确所有权和串行恢复原则落地, 未新增生产 API 或常驻工具包.

## 14. P0 落地与最终复审

### 14.1 代码提交与范围

| 阶段 | 提交 | 结果 |
| --- | --- | --- |
| 状态隔离 | `09792fb` | 建立 `config/master/common` 包内快照与 `t.Cleanup` 恢复协议 |
| P0-A config | `63e9a77` | 覆盖加载、env、归一化、节点、远端配置与失败边界 |
| P0-B master | `4ac3e9a` | 覆盖 Pipeline、生命周期、watcher、远端循环、NTP 和退出语义 |
| P0-C common | `cc4cf33` | 覆盖 logger、request、Redis、sender、IP 和运行能力 |
| P0-D utils | `e7a4306` | 使用收到请求后不响应的本地 UDP fixture 稳定 NTP 读取超时契约 |
| race 中间诊断 | `1fb2692` | 用互斥构建标签定位 WSL2 logger 子进程问题; 该 Skip 方案不满足最终门禁, 后续已删除 |
| P0 最终收口 | `9c0373c` | 用未导出日志事件工厂替代子进程和 race Skip, 修正请求安全契约与 canary 边界测试 |

P0 相对 `d9e833f` 修改两处生产 Go 文件. `common/init.go` 新增两个未导出的 IP 查询函数变量, 默认值仍分别为 `myip.InternalIPv4` 和 `myip.ExternalIPv4`; `initServerIP` 的调用顺序、回退条件、赋值目标和 `//go:norace` 均未改变. `master/init.go` 新增未导出的 `pipelineRuntimeErrorEvent`, 默认值仍为 `alarm.Error`; 两条 Runtime 链仍在原函数、原错误分支执行 `.Err(err).Msg(...)`, 消息、错误字段、Pipeline 顺序、错误后继续执行和 zerolog caller 调用栈均不变. 两个测试缝都不导出, 不改变四项目编译接口.

### 14.2 生产影响与优势

- 公共 API、函数签名、配置结构、默认值、Pipeline、四项目注册链和生产启动顺序不变.
- 正常生产网络、goroutine、文件和错误处理逻辑不变; 未导出函数变量只替换测试中的硬编码外部依赖或日志事件来源, 默认实现和调用点保持原值; 新增测试不会进入下游应用调用链.
- 核心包覆盖率达到 `config 84.5%`、`master 52.7%`、`common 82.7%`, 均高于一期门槛.
- 全局状态、环境变量、默认 pool、logger、HTTP/Redis fixture 和 watcher 文件状态具有显式所有权与 cleanup, 测试可独立、离线、重复和 shuffle 执行.
- 迁移后 utils 不为覆盖率数字扩张; 本次只修复 NTP 超时 fixture 在高负载下可能于 UDP 写入前过期的真实 flake.

### 14.3 审查闭环

- config、master、common 各阶段均经过五维初审、发现真实性核验、修复和最终复审; common 最终报告 `.codereview/reports/cr_MSLT199YxcerzzmsZbQ.md` 为 0 finding.
- NTP 初审发现 1 条低优先级测试耗时问题, 从 1s 收敛到 250ms 后双平台 50 轮由约 54s 降至约 17s; 最终报告 `.codereview/reports/cr_MSLUCJJK6an9YqdAPvk.md` 为 0 finding.
- master race 初审发现“事件断言冒充日志断言”和复审注释不一致, 两项均属实; `1fb2692` 的显式 Skip 只保留为中间诊断证据, 最终未采纳. `9c0373c` 改用未导出 zerolog event 工厂, 同时断言固定消息、原始 error 和两类错误后的继续执行, WSL2 race 不再 Skip.
- P0 总审查初审发现默认 HTTP 客户端敏感正文契约、canary 自证边界、过宽 race Skip 和文档/生产增量不一致等真实问题, 均完成修复. 最终五维报告 `.codereview/reports/cr_MSLXK8LSaBLoEJsW0FU.md` 为 0 finding; Context7 未配置且没有第三方 API 争议, 验证覆盖标记为 `NONE`.
- `.codereview/` 为本地忽略的审查 sidecar, 不进入 pkg Git 提交.

### 14.4 最终门禁与剩余条件

- clean `9c0373c` 在 Windows 与 WSL2 Go 1.26.5 通过普通全量、`-shuffle=on -count=2`、`std_json` 和 vet; `go mod verify` 与 `go mod tidy -diff` 通过且 worktree 零写入.
- WSL2 对 `config/common/master/crontab/internal/ntp` 逐包 race 均通过, 无 race 专项豁免或 race 诊断. JSON 复核确认所有 Skip 都是 6 个显式 opt-in 的既有 NTP 在线测试: 5 个公网 smoke 使用 `PKG_ONLINE_TESTS=1`, 认证用例使用 `-args test_auth` 并要求本地认证 NTP 服务. `TestPipelineRuntimeErrorsAreReported` 正常执行, 在同一进程注入内存 zerolog event, 直接验证四条日志/错误文本和 ConfigStage/MainStage 错误后的继续执行.
- 四项目固定到包含 `master/init.go` 最终生产增量的候选执行 compile-only/build/vet 全部通过, DataRouter、DataPlugins、IPIP 全量测试通过, 四仓零写入. NodeAgent compile-only/build/vet 通过, 因此生产兼容结论不再沿用 `cc4cf33` 的旧结果.
- NodeAgent Windows 全量仍只失败于既有 `conf.TestValidateAbsPaths` Unix 路径断言; Ubuntu 必须全绿. 该外部项目债务不阻塞 pkg P0, 但阻塞一期宣称四项目 Windows 全绿.
- `common.loadReq` 在 `ReqDebug=true` 时对默认客户端调用 `EnableDumpAll`, 仍可能输出请求或响应正文. P0 为保持生产逻辑不变不调整该策略, 测试只冻结 debug 开关和元数据可观察性, 不把敏感正文可见性固化为契约; P1 应评审脱敏或禁用正文 dump.

最终判定: pkg P0 代码与门禁为 Go; 四项目生产兼容为 Go; 一期跨项目双平台总验收为 Conditional Go.
