package pprof

import (
	"net/http"
	_ "net/http/pprof" // #nosec G108

	"github.com/fufuok/pkg/config"
)

// Run 开启统计和性能工具
func Run() {
	addr := config.Config().WebConf.PProfAddr
	if addr != "" {
		_ = http.ListenAndServe(addr, nil) // #nosec G114
	}
}
