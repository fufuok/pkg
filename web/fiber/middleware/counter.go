package middleware

import (
	"fmt"

	"github.com/fufuok/utils"
	"github.com/gofiber/fiber/v2"

	"github.com/fufuok/pkg/kit"
)

var (
	// 指定路由名称计数
	httpCounter = make(map[string]*httpCount)

	// 请求速率统计
	httpQPS kit.RateState
)

type httpCount struct {
	In  *kit.UCounter
	OK  *kit.UCounter
	Err *kit.UCounter
}

// HTTPCounter 请求简单计数
func HTTPCounter(name string) fiber.Handler {
	counter, ok := httpCounter[name]
	if !ok {
		counter = &httpCount{
			In:  kit.NewUCounter(),
			OK:  kit.NewUCounter(),
			Err: kit.NewUCounter(),
		}
		httpCounter[name] = counter
	}
	return func(c *fiber.Ctx) (err error) {
		counter.In.Add(1)

		err = c.Next()

		if err != nil || c.Response().StatusCode() >= fiber.StatusInternalServerError {
			counter.Err.Add(1)
		} else {
			counter.OK.Add(1)
		}
		return
	}
}

func CounterStats() map[string]any {
	stats := make(map[string]any)
	var ii, oo, ee, bb uint64
	for k, v := range httpCounter {
		i := v.In.Load()
		o := v.OK.Load()
		e := v.Err.Load()
		b := i - o - e
		ii += i
		oo += o
		ee += e
		bb += b
		stats[k] = fmt.Sprintf("In: %s  Ok: %s  Err: %s  Running: %d",
			utils.Commau(i), utils.Commau(o), utils.Commau(e), b)
	}

	// 总请求计数
	stats["/"] = fmt.Sprintf("In: %s  Ok: %s  Err: %s  Running: %d",
		utils.Commau(ii), utils.Commau(oo), utils.Commau(ee), bb)

	// 请求速率
	stats["QPS"] = httpQPS.Rate(ii)
	return stats
}

// ResetStatistics 重置统计数据
func ResetStatistics() {
	for _, counter := range httpCounter {
		counter.In.Store(0)
		counter.OK.Store(0)
		counter.Err.Store(0)
	}
}
