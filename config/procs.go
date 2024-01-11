package config

import (
	"runtime"

	"github.com/fufuok/utils"
)

var (
	// DefaultGOMAXPROCS 缺省的并发配置, 最少 4
	DefaultGOMAXPROCS = utils.MaxInt(runtime.NumCPU(), 4)
)
