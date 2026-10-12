package sys

import (
	"net/http"
	"strings"
	"time"

	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/json"
	"github.com/fufuok/pkg/stats"
	"github.com/fufuok/pkg/sysenv"
	"github.com/gin-gonic/gin"

	"github.com/fufuok/pkgapp/service"
)

// runtimeStatsHandler 分发运行状态和说明.
func runtimeStatsHandler(c *gin.Context) {
	// 前缀完全由 web_conf.stats_path 决定, 未配置返回 404.
	// 它只是可热更的路径密码. 对外监听时须换掉公开示例值, URL 也可能留在访问日志中.
	path := c.Param("path")
	cfg := config.Config()
	if cfg == nil || cfg.WebConf.StatsPath == "" || !strings.HasPrefix(path, cfg.WebConf.StatsPath) {
		c.String(http.StatusNotFound, "404")
		return
	}

	switch strings.TrimPrefix(path, cfg.WebConf.StatsPath) {
	case "", "/":
		writeJSON(c, runtimeStats())
	case "/desc":
		writeJSON(c, runtimeStatsDesc())
	default:
		c.String(http.StatusNotFound, "404")
	}
}

// runtimeStats 返回 pkg/stats 和本示例的进程内观察值.
func runtimeStats() map[string]any {
	start := time.Now()
	web := stats.WebStats()
	web["Elapsed"] = time.Since(start).String()
	return map[string]any{
		"SYS":     stats.SYSStats(),
		"METRIC":  stats.MetricStats(),
		"NETWORK": stats.NetworkStats(),
		"WEB":     web,
		"APP": map[string]any{
			"Ticks":      service.Ticks(),
			"Generation": service.Generation(),
			"ConfigFile": config.ConfigFile,
			"OS": map[string]any{
				"Linux":     sysenv.IsLinux(),
				"Windows":   sysenv.IsWindows(),
				"Systemctl": sysenv.BinSystemctl,
			},
		},
	}
}

// writeJSON 使用 pkg/json 缩进输出.
func writeJSON(c *gin.Context, value any) {
	c.Data(http.StatusOK, "application/json; charset=utf-8", json.MustJSONIndent(value))
}
