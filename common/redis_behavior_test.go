package common

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// redisBehaviorStub 通过嵌入接口保留完整 UniversalClient 契约, 只实现本组测试触达的方法.
// 未覆盖的方法若被意外调用会因 nil 嵌入接口而失败, 从而暴露行为范围扩张.
type redisBehaviorStub struct {
	redis.UniversalClient
	setNXValue  bool
	ttlValue    time.Duration
	timeOffset  time.Duration
	timeOffsets []time.Duration
	timeCalls   atomic.Int64
}

// SetNX 返回预设加锁结果.
func (s *redisBehaviorStub) SetNX(ctx context.Context, _ string, _ any, _ time.Duration) *redis.BoolCmd {
	cmd := redis.NewBoolCmd(ctx)
	cmd.SetVal(s.setNXValue)
	return cmd
}

// PTTL 返回预设剩余生命周期.
func (s *redisBehaviorStub) PTTL(ctx context.Context, _ string) *redis.DurationCmd {
	cmd := redis.NewDurationCmd(ctx, time.Nanosecond)
	cmd.SetVal(s.ttlValue)
	return cmd
}

// Time 返回基于调用时刻的固定偏移, 避免测试依赖墙钟绝对值.
func (s *redisBehaviorStub) Time(ctx context.Context) *redis.TimeCmd {
	call := int(s.timeCalls.Add(1)) - 1
	offset := s.timeOffset
	if call < len(s.timeOffsets) {
		offset = s.timeOffsets[call]
	}
	cmd := redis.NewTimeCmd(ctx)
	cmd.SetVal(time.Now().Add(offset))
	return cmd
}

// TestRedisInitializationAndLockContract 验证未初始化、成功和锁 TTL 三类公开行为.
func TestRedisInitializationAndLockContract(t *testing.T) {
	preserveCommonPackageState(t)
	InitRedisDB(nil)
	if TryLock("lock", time.Second) {
		t.Fatal("uninitialized Redis unexpectedly acquired a lock")
	}
	if got := LockKeyTTL("lock"); got != 0 {
		t.Fatalf("uninitialized lock TTL = %s", got)
	}

	stub := &redisBehaviorStub{setNXValue: true, ttlValue: 750 * time.Millisecond}
	InitRedisDB(stub)
	if !RedisDBInited.Load() || RedisDB != stub {
		t.Fatal("InitRedisDB did not publish the provided client")
	}
	if !TryLock("lock", time.Second) {
		t.Fatal("initialized Redis did not return the stub lock result")
	}
	if got := LockKeyTTL("lock"); got != 750*time.Millisecond {
		t.Fatalf("lock TTL = %s", got)
	}

	busy := &redisBehaviorStub{setNXValue: false, ttlValue: 750 * time.Millisecond}
	InitRedisDB(busy)
	if TryLock("lock", time.Second) {
		t.Fatal("occupied Redis lock was unexpectedly acquired")
	}

	expired := &redisBehaviorStub{setNXValue: true, ttlValue: -2 * time.Nanosecond}
	InitRedisDB(expired)
	if !TryLock("lock", time.Second) {
		t.Fatal("expired Redis lock was not reacquired")
	}
	if got := LockKeyTTL("lock"); got != -2*time.Nanosecond {
		t.Fatalf("expired lock TTL = %s", got)
	}
}

// TestClockOffsetChanRedisContract 验证 nil、预取消、最小周期、三点中值和最终关闭语义.
func TestClockOffsetChanRedisContract(t *testing.T) {
	preserveCommonPackageState(t)
	ClockOffsetMinInterval = 20 * time.Millisecond
	ClockOffsetInterval = 30 * time.Millisecond
	if ch := ClockOffsetChanRedis(context.Background(), time.Millisecond, nil); ch != nil {
		t.Fatal("nil Redis client returned a channel")
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	closed := ClockOffsetChanRedis(canceled, time.Millisecond, &redisBehaviorStub{})
	select {
	case _, ok := <-closed:
		if ok {
			t.Fatal("pre-canceled clock channel emitted a value")
		}
	case <-time.After(time.Second):
		t.Fatal("pre-canceled clock channel did not close")
	}

	stub := &redisBehaviorStub{timeOffsets: []time.Duration{30 * time.Millisecond, 150 * time.Millisecond, 90 * time.Millisecond}}
	ctx, stop := context.WithCancel(context.Background())
	t.Cleanup(stop)
	started := time.Now()
	ch := ClockOffsetChanRedis(ctx, time.Millisecond, stub)
	select {
	case offset := <-ch:
		if offset < 70*time.Millisecond || offset > 110*time.Millisecond {
			t.Fatalf("clock offset = %s", offset)
		}
		if elapsed := time.Since(started); elapsed < 30*time.Millisecond {
			t.Fatalf("three clock samples completed in %s, minimum interval was not applied", elapsed)
		}
	case <-time.After(time.Second):
		t.Fatal("clock offset channel did not emit the three-sample median")
	}
	if calls := stub.timeCalls.Load(); calls != 3 {
		t.Fatalf("Redis TIME calls = %d", calls)
	}
	stop()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("clock channel emitted after cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("clock channel did not close after cancellation")
	}
}
