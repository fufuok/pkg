package sys

import "github.com/fufuok/pkg/stats"

// runtimeStatsDesc 返回与运行状态层级一致的指标说明.
func runtimeStatsDesc() map[string]any {
	return map[string]any{
		"SYS":     stats.SYSStatsDesc(),
		"METRIC":  stats.MetricStatsDesc(),
		"NETWORK": stats.NetworkStatsDesc(),
		"WEB":     stats.WebStatsDesc(),
	}
}
