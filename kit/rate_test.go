package kit

import (
	"testing"
	"time"

	"github.com/fufuok/utils/assert"
)

func TestRateState_Rate(t *testing.T) {
	t.Run("首次调用返回0", func(t *testing.T) {
		rs := NewRateState()
		rate := rs.Rate(100)
		assert.Equal(t, float64(0), rate)
	})

	t.Run("计数未改变返回0", func(t *testing.T) {
		rs := NewRateState()
		rs.Rate(100) // 初始化
		rate := rs.Rate(100)
		assert.Equal(t, float64(0), rate)
	})

	t.Run("计数器重置返回-1", func(t *testing.T) {
		rs := NewRateState()
		rs.Rate(100)        // 初始化
		rate := rs.Rate(50) // 小于之前的值，视为重置
		assert.Equal(t, float64(-1), rate)
	})

	t.Run("零值返回-1", func(t *testing.T) {
		rs := NewRateState()
		rs.Rate(100)       // 初始化
		rate := rs.Rate(0) // 零值
		assert.Equal(t, float64(-1), rate)
	})

	t.Run("正常计算速率", func(t *testing.T) {
		rs := NewRateState()
		rs.Rate(100) // 初始化

		// 模拟时间流逝
		rs.lastTime = rs.lastTime.Add(-2 * time.Second)
		rate := rs.Rate(200)               // 增加100个单位，在2秒内
		assert.Equal(t, float64(50), rate) // 100/2 = 50

		// 模拟时间流逝
		rs.lastTime = rs.lastTime.Add(-2 * time.Second)
		rate = rs.Rate(300)                // 增加100个单位，在2秒内
		assert.Equal(t, float64(50), rate) // 100/2 = 50
	})

	t.Run("时间间隔不足1秒返回上次速率", func(t *testing.T) {
		rs := NewRateState()
		rs.Rate(100) // 初始化

		// 模拟时间流逝但不足1秒
		rs.lastTime = rs.lastTime.Add(-500 * time.Millisecond)
		rs.lastRate = 30.0 // 设置上次速率
		rate := rs.Rate(200)
		assert.Equal(t, float64(30), rate) // 返回上次速率
	})
}

func TestRateState_ZeroValue(t *testing.T) {
	t.Run("零值结构体可以直接使用", func(t *testing.T) {
		// 测试零值结构体是否可以直接使用
		var rate RateState

		// 第一次调用应该返回0
		rate1 := rate.Rate(100)
		assert.Equal(t, float64(0), rate1)

		// 模拟时间流逝
		rate.lastTime = rate.lastTime.Add(-2 * time.Second)

		// 第二次调用应该计算速率
		rate2 := rate.Rate(200)
		assert.Equal(t, float64(50), rate2) // (200-100)/2 = 50
	})

	t.Run("零值结构体默认最小时间间隔", func(t *testing.T) {
		var rate RateState

		// 检查默认最小时间间隔是否为1.0秒
		rate.Rate(100) // 触发初始化

		// 模拟时间流逝但不足1秒
		rate.lastTime = rate.lastTime.Add(-500 * time.Millisecond)

		// 应该返回上次速率（0），因为时间间隔不足
		rateResult := rate.Rate(200)
		assert.Equal(t, float64(0), rateResult)
	})
}

func TestNewRateState(t *testing.T) {
	t.Run("默认参数", func(t *testing.T) {
		rs := NewRateState()
		// 默认应该是1秒
		assert.Equal(t, 1.0, rs.minSecond)
	})

	t.Run("自定义参数", func(t *testing.T) {
		rs := NewRateState(2.5)
		// 应该使用传入的参数
		assert.Equal(t, 2.5, rs.minSecond)
	})

	t.Run("多个参数只使用第一个", func(t *testing.T) {
		rs := NewRateState(3.0, 5.0, 7.0)
		// 应该只使用第一个参数
		assert.Equal(t, 3.0, rs.minSecond)
	})

	t.Run("使用自定义时间间隔计算速率", func(t *testing.T) {
		// 创建一个需要至少2秒间隔的速率计算器
		rs := NewRateState(2.0)
		rs.Rate(100) // 初始化

		// 模拟时间流逝但不足2秒
		rs.lastTime = rs.lastTime.Add(-1 * time.Second)
		rate := rs.Rate(200)
		// 应该返回上次速率（0），因为时间间隔不足, 且计数器不改变
		assert.Equal(t, float64(0), rate)

		// 模拟时间流逝超过2秒
		rs.lastTime = rs.lastTime.Add(-1 * time.Second) // 总共2秒
		rate = rs.Rate(300)
		// 应该计算新的速率 (200/2 = 100)
		assert.Equal(t, float64(100), rate)
	})
}

// 新增测试用例：测试 SetMinSecond 方法
func TestRateState_SetMinSecond(t *testing.T) {
	t.Run("设置最小时间间隔", func(t *testing.T) {
		rs := NewRateState()
		// 默认应该是1秒
		assert.Equal(t, 1.0, rs.minSecond)

		// 设置为0.5秒
		rs.SetMinSecond(0.5)
		assert.Equal(t, 0.5, rs.minSecond)

		rs.Rate(100) // 初始化

		// 模拟时间流逝0.6秒，大于0.5秒
		rs.lastTime = rs.lastTime.Add(-600 * time.Millisecond)
		rate := rs.Rate(200)
		// 应该计算新的速率 (100/0.6 ≈ 166.67)
		assert.Equal(t, 166.67, rate)
	})

	t.Run("零值结构体设置最小时间间隔", func(t *testing.T) {
		var rs RateState
		// 设置为2秒
		rs.SetMinSecond(2.0)
		assert.Equal(t, 2.0, rs.minSecond)

		rs.Rate(100) // 初始化

		// 模拟时间流逝1.5秒，小于2秒
		rs.lastTime = rs.lastTime.Add(-1500 * time.Millisecond)
		rate := rs.Rate(200)
		// 应该返回上次速率（0），因为时间间隔不足, 且计数器值不改变
		assert.Equal(t, float64(0), rate)

		// 模拟时间流逝再增加1秒，总共2.5秒，大于2秒
		rs.lastTime = rs.lastTime.Add(-1 * time.Second)
		rate = rs.Rate(300)
		// 应该计算新的速率 (200/2.5 = 80)
		assert.Equal(t, float64(80), rate)
	})

	t.Run("SetMinSecond避免不必要的更新", func(t *testing.T) {
		rs := NewRateState(1.0)
		original := rs.minSecond

		// 设置相同的值，应该不更新
		rs.SetMinSecond(1.0)
		assert.Equal(t, original, rs.minSecond)

		// 设置不同的值，应该更新
		rs.SetMinSecond(2.0)
		assert.Equal(t, 2.0, rs.minSecond)
	})
}

// 新增测试用例：测试 RateWithLastCount 方法
func TestRateState_RateWithLastCount(t *testing.T) {
	t.Run("获取速率和上次计数", func(t *testing.T) {
		rs := NewRateState()
		rate, lastCount := rs.RateWithLastCount(100)
		assert.Equal(t, float64(0), rate)     // 首次调用返回0
		assert.Equal(t, uint64(0), lastCount) // 首次调用返回0

		// 模拟时间流逝
		rs.lastTime = rs.lastTime.Add(-2 * time.Second)
		rate, lastCount = rs.RateWithLastCount(200)
		assert.Equal(t, float64(50), rate)      // (200-100)/2 = 50
		assert.Equal(t, uint64(100), lastCount) // 上次计数是100
	})

	t.Run("计数器重置情况", func(t *testing.T) {
		rs := NewRateState()
		rs.RateWithLastCount(100)                   // 初始化
		rate, lastCount := rs.RateWithLastCount(50) // 计数器重置
		assert.Equal(t, float64(-1), rate)
		assert.Equal(t, uint64(100), lastCount)
	})

	t.Run("计数未改变情况", func(t *testing.T) {
		rs := NewRateState()
		rs.RateWithLastCount(100)                    // 初始化
		rate, lastCount := rs.RateWithLastCount(100) // 计数未改变
		assert.Equal(t, float64(0), rate)
		assert.Equal(t, uint64(100), lastCount) // 上次计数是100
	})
}
