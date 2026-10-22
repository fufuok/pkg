package config

import (
	"runtime"
)

// DefaultGOMAXPROCS 缺省的并发配置, 默认取进程启动时运行时给出的 GOMAXPROCS.
//
// Go 1.25 起运行时默认值已遵从 cgroup CPU 限额与 GOMAXPROCS 环境变量, 因此不再强制提升到至少 4:
// CPU 少于 4 核或限额小于 4 时, 默认值随之变小. 依赖旧行为的应用可在 init 中显式赋值,
// 如 config.DefaultGOMAXPROCS = 4. master.Run 只在该值大于 0 且与当前值不同时才设置运行时;
// 小于等于 0 表示不覆盖, 默认协程池回退到 runtime.GOMAXPROCS(0).
var DefaultGOMAXPROCS = runtime.GOMAXPROCS(0)
