package common

import (
	"testing"
	"time"

	"github.com/fufuok/utils/assert"

	"github.com/fufuok/pkg/config"
)

func TestGTimeNow(t *testing.T) {
	t.Log(GTimeNow())
	ts := GTimeNowString(time.RFC3339)
	assert.Equal(t, "+08:00", ts[len(ts)-6:])

	tm := time.Date(1, 1, 1, 0, 0, 0, 0, config.CSTTimeLocation)
	assert.Equal(t, "0001-01-01T00:00:00+08:00", tm.Format(time.RFC3339))

	// 对于 公元1年 这种极早日期, 某些系统的时区数据库可能使用当时的 本地平均时间(LMT), 结果: +08:05:43 而非标准 +08:00
	// tm = time.Date(1, 1, 1, 0, 0, 0, 0, config.DefaultTimeLocation)
	// assert.Equal(t, "0001-01-01T00:00:00+08:00", tm.Format(time.RFC3339))
}
