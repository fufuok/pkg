package stats

import (
	"fmt"

	"github.com/fufuok/pkg/utils"
)

// addStatsError 沿用分组失败标识, 不把平台不支持的可选能力当作采集失败.
func addStatsError(values map[string]any, key, group string) {
	errs, _ := values[key].([]string)
	values[key] = append(errs, group)
}

// setSystemMemory 同时保留展示字段与 uint64 原始值, 使用量为总量减可用量的粗略估计.
func setSystemMemory(values map[string]any, total, available uint64) {
	available = min(total, available)
	used := total - available
	percent := float64(0)
	if total > 0 {
		percent = 100 * float64(used) / float64(total)
	}
	values["MemTotal"], values["MemAvailable"] = utils.HumanIBytes(total), utils.HumanIBytes(available)
	values["MemUsed"], values["MemUsedPercent"] = utils.HumanIBytes(used), fmt.Sprintf("%.2f%%", percent)
	values["MemTotalBytes"], values["MemAvailableBytes"] = total, available
}

// setProcessMemory 仅在内存信息读取成功后组装字段, 不将读取失败伪装为零占用.
func setProcessMemory(values map[string]any, rss, vms, swap uint64) {
	values["MemRSS"], values["MemVMS"], values["MemSwap"] = utils.HumanIBytes(rss), utils.HumanIBytes(vms), utils.HumanIBytes(swap)
	values["MemRSSBytes"] = rss
}
