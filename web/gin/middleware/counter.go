package middleware

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/fufuok/utils"
	"github.com/gin-gonic/gin"
)

var (
	// 指定路由名称计数
	httpCounter = make(map[string]*httpCount)

	// 上一次总请求计数和值对应的时间点
	httpTotal     uint64
	httpTotalTime time.Time
)

type httpCount struct {
	In  atomic.Uint64
	OK  atomic.Uint64
	Err atomic.Uint64
}

// HTTPCounter 请求简单计数
func HTTPCounter(name string) gin.HandlerFunc {
	counter, ok := httpCounter[name]
	if !ok {
		counter = &httpCount{}
		httpCounter[name] = counter
	}
	return func(c *gin.Context) {
		counter.In.Add(1)

		c.Next()

		if len(c.Errors) > 0 || c.Writer.Status() >= http.StatusInternalServerError {
			counter.Err.Add(1)
		} else {
			counter.OK.Add(1)
		}
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

	// 请求速率 (前后两次刷新时间)
	stats["QPS"] = calcQPS(ii)
	return stats
}

func calcQPS(n uint64) (rate float64) {
	now := time.Now()
	if httpTotal > 0 && n > httpTotal {
		rate = float64(n-httpTotal) / now.Sub(httpTotalTime).Seconds()
		rate = utils.Round(rate, 2)
	}
	httpTotal = n
	httpTotalTime = now
	return
}

// ResetStatistics 重置统计数据
func ResetStatistics() {
	for _, counter := range httpCounter {
		counter.In.Store(0)
		counter.OK.Store(0)
		counter.Err.Store(0)
	}
}
