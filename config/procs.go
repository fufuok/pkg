package config

import (
	"runtime"
)

// DefaultGOMAXPROCS 是应用可在启动前覆盖的并发配置, 默认取 runtime.GOMAXPROCS(0).
// 正数直接作为配置值使用; 非正数不覆盖运行时, 默认协程池回退到当前运行时值.
// 此变量不随之后的 CPU 配额变化自动刷新, 应用应在 master.Run 或 common 启动前完成赋值.
var DefaultGOMAXPROCS = runtime.GOMAXPROCS(0)
