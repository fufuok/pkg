# 时区配置与美国时区示例

## 运行极简示例

在仓库根目录运行:

```shell
go run ./examples/timezone
```

示例在 `main` 中选择 `America/New_York`, 调用 `common.InitTimeLocation()` 后输出标准库和 pkg 业务时钟, 随即退出. 不需要配置文件、env 文件或独立 Go module. 内嵌 `time/tzdata` 使 Linux 和 Windows 都能使用相同的 IANA 时区数据.

冬夏样本输出固定为:

```text
winter: 2026-01-15T07:00:00-05:00 EST
summer: 2026-07-15T08:00:00-04:00 EDT
```

纽约有夏令时, 不能用全年固定 UTC-5 代替. 实际业务时钟还保留 pkg 原有应用时钟偏移.

示例源码见 [main.go](main.go). 示例结束时调用 `ants.ReleaseTimeout` 等待后台计时退出; 常驻应用应在停止业务后按自身生命周期清理资源.

## 需求范围与生效时机

应用拥有时区决定权: 未主动选择时, Linux/Windows 均默认固定 UTC+8, 忽略宿主时区及 `TZ`, 无需 systemd、env 文件或修改整机时区. 应用显式选择后, 标准库默认本地时区和 pkg 业务时区统一采用该 Location.

**全局时区在受控启动初始化成功后生效. 仅导入 pkg 不会改写 `time.Local`, 不保证依赖初始化阶段就已采用 UTC+8.** `master.Main()` / `Run()` / `Start()` 自动调用 `common.InitTimeLocation()`, `common.M.Start()` 也在本模块的日志和后台任务前调用这个唯一入口. 手工启动流程同样使用 `common.InitTimeLocation()`.

这里的“全局”指当前 Go 进程的默认 `time.Local`. 初始化后, 应用和依赖随后调用的 `time.Now()`、默认 pretty 日志、本地时间文件名和 `LocalTime: true` 的日志轮转采用所选时区. 显式调用 UTC、指定其他 Location 或自带时区配置的依赖仍遵循自身规则. 本方案只修改 pkg, 不修改依赖源码或版本, 不更改操作系统时区, 也不把 Go 内存中的时区设置传递给外部子进程; 同一应用被 daemon 重新执行时会再次初始化.

## 选择方式

只设置 `config.DefaultTimeLocation`, `DefaultTimeZone` 在初始化时自动派生, 无需分别设置两个值.

| 应用选择 | 启动前的设置 | 初始化后的默认时区 |
| --- | --- | --- |
| 不设置 | 保留默认值 | 固定 UTC+8, 所有年份均为 +08:00 |
| 自定义 | `time.LoadLocation("America/New_York")` 的返回值 | 纽约规则, 冬季 -05:00, 夏季 -04:00 |
| 宿主优先 | `config.DefaultTimeLocation = time.Local` | Go 原本的本地时区规则 |
| UTC | `config.DefaultTimeLocation = time.UTC` | UTC |

完整应用选择纽约的最小用法:

```go
package main

import (
	"log"
	"time"
	_ "time/tzdata"

	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/master"
)

// main 只在启动前选择时区, 由框架初始化后再启动业务.
func main() {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		log.Fatalf("Failed to load application time location: %v", err)
	}
	config.DefaultTimeLocation = loc
	master.Main()
}
```

`_ "time/tzdata"` 为应用内嵌 IANA 数据, 使 Windows 和精简 Linux 环境也可解析纽约, 无需安装外部时区数据库. 默认固定 UTC+8 不需要该导入. 纽约不能用全年固定 UTC-5 代替; `Asia/Shanghai` 包含历史 LMT/DST, 也不等同于固定 UTC+8.

选择宿主优先必须在覆盖 `time.Local` 之前赋值. 此模式下, Linux/Unix 采用 Go 首次加载的 `TZ` 规则, 未设置时采用 `/etc/localtime`, 空 `TZ` 表示 UTC; Windows 采用系统时区, Go 不读取 `TZ`. 默认模式始终忽略这些来源. 仅设置 `TZ`, 或仅给 `DefaultTimeZone` 赋名称, 都不是应用显式选择 Location.

## 启动顺序与安全边界

顺序为: 选择 Location -> 初始化全局时区 -> 加载配置、构建日志/cron -> 启动业务任务. 手工加载配置的应用应导入 `github.com/fufuok/pkg/common`, 在这些操作前显式初始化并处理错误:

```go
if err := common.InitTimeLocation(); err != nil {
    log.Fatalf("Failed to initialize application time location: %v", err)
}
```

`master.Run/Start` 在配置 Pipeline 前初始化. 直接 `common.M.Start()` 只能保证早于本模块创建的任务; 普通配置加载可能已启动 IP 查询等后台任务, 因此手工流程仍应更早显式初始化. 自定义 `master.FlagParser` 在自动初始化前执行, 应只解析参数和选择时区, 若启动任务也须更早初始化. 在 ConfigStage 中才选择时区已经太晚.

初始化必须早于应用 goroutine、首次 `ants.Submit`、自建协程池、日志使用及 cron 构建, 且不得提前替换或关闭 ants 原生默认池. pkg 通过现有 API 停止并等待该默认池后台计时, 成功后设置一次 `time.Local` 再恢复池. 即使后台任务只取 Unix 时间戳, `time.Now()` 也会读取 `time.Local`, 因而仍需协调读写; 这与改变依赖自身的时区配置无关. 停池失败不写全局时区、不恢复池, 必须终止启动. 同一 Location 的重复调用不重复赋值, 不支持运行期切换.

本入口不会暂停其他依赖的后台任务. 若应用或依赖已在 `init()` 中启动时间读取, 应用须调整启动顺序或协调这些读者; **只升级 pkg 不能保证任意现有应用的导入期竞态都已消除**. `sync.Once` 仅防重复赋值, 无法同步第三方裸读. 原来在 config 导入期间就假定 `time.Now()` 为 UTC+8 的代码也需要迁移.

时区选择是代码级启动选项, pkg 未提供对应的 env 或 JSON 配置字段. `.default.env`、运行期 env 和配置 Pipeline 均晚于 master 的时区入口, 初始化后修改 `TZ` 也不会可靠刷新已缓存规则. 应用可在受控引导阶段自行读取启动选项, 解析为 Location 后再初始化; 不要在初始化后直接修改 `time.Local` 或 `config.DefaultTimeLocation`.

## 工具函数与兼容语义

`utils.ChinaTimeZone` 和 `ChinaTimeOffset` 是名称与秒偏移常量; `ChinaTimeLocation` 是可复用的固定 UTC+8 Location, 供 config 默认值和显式 `.In(...)` 转换使用, 引用本身不改全局. `utils.InitLocation(name)` 只解析 Location, 失败返回当前本地 Location 和 false, 调用者应检查结果. `utils.InitChinaLocation()` 保留直接写 `time.Local` 的旧 setter 行为, 未新增弃用标记, 直接调用仍须保证没有并发时间读取. `InitCSTLocation()` 也保留原行为和既有弃用说明.

`GTimeNow()` 继续使用所选时区及应用时钟偏移, crontab 使用同一 Location 和业务时间源. 选择时区不改变系统时钟或 Unix 时间戳, 应用偏移也不会自动作用于标准库和轮转时间. 已产生的时间值及已构建 cron 不会自动迁移. `common.StartTime` 保留原始采样及单调时钟用于耗时计算, 直接格式化时仍使用采样时的 Location; 展示应用时区应使用 `common.StartTime.In(config.DefaultTimeLocation)`, `SYSStats` 已转换这个展示副本. `ZeroTimeCST` 始终保留固定 UTC+8 哨兵语义.

默认 UTC+8 保持旧版轮转文件名口径. 若应用主动切换到其他时区, 应先将旧备份归档到清理器不扫描的目录或使用独立日志路径, 避免不带时区偏移的文件名混用导致 `MaxBackups` 清理时删新留旧.
