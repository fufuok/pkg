package config

import (
	"runtime"

	"github.com/fufuok/pkg/utils"
)

// DefaultGOMAXPROCS 缺省的并发配置, 最少 4
var DefaultGOMAXPROCS = utils.MaxInt(runtime.NumCPU(), 4)
