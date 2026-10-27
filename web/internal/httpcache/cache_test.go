package httpcache

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// TestConfigDefaultsAndIsolation 验证安全默认值, 配置去重和构造后的切片隔离.
func TestConfigDefaultsAndIsolation(t *testing.T) {
	vary := []string{"x-tenant", " ACCEPT ", "content-type", "Content-Encoding"}
	c := New(0, Config{Vary: vary})
	if c.Config.Capacity != 256 || c.Config.MaxBodyBytes != 256<<10 ||
		c.Config.MaxRequestBodyBytes != 64<<10 || c.ttl != 10*time.Minute {
		t.Fatalf("unexpected defaults: %+v, ttl %v", c.Config, c.ttl)
	}
	if got := strings.Join(c.Config.Vary, ","); got != "Accept,Accept-Encoding,Accept-Language,Origin,Forwarded,X-Forwarded-Host,X-Forwarded-Proto,X-Tenant" {
		t.Fatalf("unexpected Vary: %s", got)
	}
	req := Request{Header: http.Header{"X-Tenant": {"tenant"}}}
	key := c.Key(req, nil)
	vary[0] = "X-Other"
	c.Config.Vary[0] = "X-Changed"
	if key != c.Key(req, nil) {
		t.Fatal("caller-owned config changed the cache key")
	}
}

// TestInvalidConfig 验证错误预算或 Vary 在注册阶段失败, 不留到请求期静默降级.
func TestInvalidConfig(t *testing.T) {
	for _, cfg := range []Config{
		{MaxBodyBytes: -1},
		{MaxRequestBodyBytes: -1},
		{Capacity: ^uint32(0)},
		{Vary: []string{"*"}},
		{Vary: []string{""}},
		{Vary: []string{"X Header"}},
		{Vary: []string{"X\r\nHeader"}},
	} {
		t.Run(strings.Join(cfg.Vary, ","), func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("config should panic: %+v", cfg)
				}
			}()
			New(time.Second, cfg)
		})
	}
}

// TestCapacityPreallocationBound 验证容量预算在 freelru 分配数组前执行, 且上限值仍可用.
// 极大容量只检查明确的配置失败, 不让测试尝试分配巨量内存.
func TestCapacityPreallocationBound(t *testing.T) {
	cache := New(time.Second, Config{Capacity: maxCapacity})
	if cache.Config.Capacity != maxCapacity {
		t.Fatalf("capacity = %d, want %d", cache.Config.Capacity, maxCapacity)
	}
	for _, capacity := range []uint32{maxCapacity + 1, 1 << 30} {
		t.Run(fmt.Sprint(capacity), func(t *testing.T) {
			defer func() {
				if got := recover(); got != "httpcache: capacity exceeds 65536 entries" {
					t.Fatalf("panic = %v, want capacity limit error", got)
				}
			}()
			New(time.Second, Config{Capacity: capacity})
		})
	}
}

// TestStoreCopiesAndTTL 验证发布的所有权约定: Prepare 隔离原始响应头的 map 与值切片, Set 紧凑复制正文,
// 同一冻结 Header 可发布到多个键且共享而不复制; TTL 受实例上限约束, 非正 TTL 不存储.
func TestStoreCopiesAndTTL(t *testing.T) {
	c := New(time.Minute, Config{MaxBodyBytes: 3})
	live := http.Header{"Content-Type": {"text/plain"}}
	body := []byte("abc")
	entry, ok := c.Prepare(live, body, http.StatusOK)
	if !ok {
		t.Fatal("response rejected")
	}
	entry.TTL = time.Hour
	keys := [][32]byte{{1}, {3}}
	for _, key := range keys {
		c.Set(key, entry)
	}
	live["Content-Type"][0] = "changed"
	live.Set("X-Late", "1")
	body[0] = 'z'
	for _, key := range keys {
		got, ok := c.Get(key)
		if !ok || string(got.Body) != "abc" || got.Header.Get("Content-Type") != "text/plain" ||
			got.Header.Get("X-Late") != "" || got.TTL != time.Minute {
			t.Fatalf("cache should hold an isolated capped entry: %+v, %v", got, ok)
		}
	}
	first, _ := c.Get(keys[0])
	second, _ := c.Get(keys[1])
	if first == second || &first.Header["Content-Type"][0] != &second.Header["Content-Type"][0] ||
		&first.Body[0] == &second.Body[0] {
		t.Fatal("published entries should be distinct, share the frozen header and own their bodies")
	}
	for _, entry := range []*Entry{
		{TTL: 0},
		{TTL: -time.Second},
	} {
		c.Set([32]byte{2}, entry)
		if _, exists := c.Get([32]byte{2}); exists {
			t.Fatalf("invalid entry was stored: %+v", entry)
		}
	}
}

// TestStoreDeductsGenerationAge 验证显式年龄起点会扣减 TTL 并保持回放年龄一致.
func TestStoreDeductsGenerationAge(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := New(time.Minute, Config{})
		start := time.Now()
		time.Sleep(2 * time.Second)
		key := [32]byte{1}
		c.Set(key, &Entry{TTL: 5 * time.Second, StoredAt: start})
		entry, ok := c.Get(key)
		if !ok || entry.TTL != 3*time.Second || entry.Age() != "2" {
			t.Fatalf("generation age not retained: entry=%+v ok=%v", entry, ok)
		}
		time.Sleep(4 * time.Second)
		if _, ok := c.Get(key); ok {
			t.Fatal("generation time extended expiry")
		}
		c.Set(key, &Entry{TTL: time.Second, StoredAt: start})
		if _, ok := c.Get(key); ok {
			t.Fatal("already stale entry was stored")
		}
	})
}

// TestGetRejectsStaleStoreEntry 仅延长底层存储寿命来模拟墙钟回拨, 验证 HTTP
// 到期时间不随存储延长, 生成耗时只扣一次, 且过期检查不按键删除潜在的新条目.
func TestGetRejectsStaleStoreEntry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := New(time.Minute, Config{})
		start := time.Now()
		time.Sleep(2 * time.Second)
		key := [32]byte{1}
		c.Set(key, &Entry{Body: []byte("old"), TTL: 5 * time.Second, StoredAt: start})
		entry, ok := c.store.Get(key)
		if !ok || entry.TTL != 3*time.Second {
			t.Fatalf("unexpected stored entry: %+v, %v", entry, ok)
		}
		// 不修改条目本身, 只让墙钟驱动的 LRU 在 HTTP 期限之后仍报告命中.
		c.store.AddWithLifetime(key, entry, time.Hour)
		time.Sleep(2 * time.Second)
		if _, ok := c.Get(key); !ok {
			t.Fatal("generation age was deducted more than once")
		}
		time.Sleep(time.Second)
		if _, ok := c.Get(key); ok {
			t.Fatal("stale storage entry extended the HTTP lifetime")
		}
		if got, ok := c.store.Get(key); !ok || got != entry {
			t.Fatal("HTTP expiry check removed the entry by key")
		}
		c.Set(key, &Entry{Body: []byte("fresh"), TTL: time.Second})
		if fresh, ok := c.Get(key); !ok || string(fresh.Body) != "fresh" {
			t.Fatalf("replacement entry was not readable: %+v, %v", fresh, ok)
		}
	})
}

// TestLRUAndFixedTTL 验证固定容量按访问顺序淘汰, 命中不能延长到期时间.
func TestLRUAndFixedTTL(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := New(2*time.Second, Config{Capacity: 2})
		entry := &Entry{Body: []byte("ok"), TTL: time.Hour}
		first, second, third := [32]byte{1}, [32]byte{2}, [32]byte{3}
		c.Set(first, entry)
		c.Set(second, entry)
		c.Get(first)
		c.Set(third, entry)
		if _, exists := c.Get(second); exists {
			t.Fatal("least recently used entry was not evicted")
		}
		time.Sleep(time.Second)
		if _, exists := c.Get(first); !exists {
			t.Fatal("entry expired before its TTL")
		}
		time.Sleep(time.Second + time.Millisecond)
		if _, exists := c.Get(first); exists {
			t.Fatal("hit extended fixed TTL")
		}
		c.Set(first, &Entry{TTL: time.Millisecond})
		time.Sleep(2 * time.Millisecond)
		if _, exists := c.Get(first); exists {
			t.Fatal("per-entry shorter TTL was ignored")
		}
	})
}

// TestEntryAge 验证 Age 从存储起点向上取整计算, 不写入缓存条目; 回放头的隔离由各适配器测试覆盖.
func TestEntryAge(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := New(time.Minute, Config{})
		key := [32]byte{1}
		c.Set(key, &Entry{Header: http.Header{"Cache-Control": {"public, max-age=60"}}, TTL: time.Minute})
		entry, _ := c.Get(key)
		if age := entry.Age(); age != "0" {
			t.Fatalf("initial age %s", age)
		}
		time.Sleep(200 * time.Millisecond)
		if age := entry.Age(); age != "1" {
			t.Fatalf("sub-second age %s", age)
		}
		time.Sleep(800 * time.Millisecond)
		if age := entry.Age(); age != "1" {
			t.Fatalf("whole second age %s", age)
		}
		time.Sleep(time.Nanosecond)
		if age := entry.Age(); age != "2" {
			t.Fatalf("fractional second age %s", age)
		}
		if entry.Header.Get("Age") != "" || entry.Header.Get("Cache-Control") != "public, max-age=60" {
			t.Fatal("age leaked into stored headers")
		}
	})
}

// TestAcquireCoalescesAdmittedOnly 验证锁后重查合并成功回源, 未准入结果各自执行.
// 并发请求结束后锁登记必须释放, 不将请求键累积成另一份无界缓存.
func TestAcquireCoalescesAdmittedOnly(t *testing.T) {
	for _, admitted := range []bool{true, false} {
		t.Run(map[bool]string{true: "admitted", false: "rejected"}[admitted], func(t *testing.T) {
			c := New(time.Minute, Config{})
			key := [32]byte{1}
			var calls atomic.Int32
			var workers sync.WaitGroup
			start := make(chan struct{})
			for range 64 {
				workers.Go(func() {
					<-start
					release := c.Acquire(key, nil)
					defer release(false)
					if _, exists := c.Get(key); exists {
						return
					}
					calls.Add(1)
					if admitted {
						c.Set(key, &Entry{Body: []byte("ok"), TTL: time.Minute})
					}
				})
			}
			close(start)
			workers.Wait()
			want := int32(64)
			if admitted {
				want = 1
			}
			if calls.Load() != want || len(c.locks) != 0 {
				t.Fatalf("calls %d want %d, remaining locks %d", calls.Load(), want, len(c.locks))
			}
		})
	}
}

// TestAcquireDifferentKeys 验证回源期间其他请求键仍可独立获得执行资格.
func TestAcquireDifferentKeys(t *testing.T) {
	c := New(time.Minute, Config{})
	releaseFirst := c.Acquire([32]byte{1}, nil)
	defer releaseFirst(false)
	finished := make(chan struct{})
	go func() {
		release := c.Acquire([32]byte{2}, nil)
		release(false)
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("unrelated key was blocked")
	}
}

// TestAcquireReentrantOwner 验证同一 owner 再次获取自己持有的键锁时直接直通而不自锁,
// 其他 owner 与 nil 仍按单飞等待; 持有者释放后身份清除, 复用同一上下文的后续请求正常获取.
func TestAcquireReentrantOwner(t *testing.T) {
	c := New(time.Minute, Config{})
	key := [32]byte{1}
	owner, other := new(int), new(int)
	release := c.Acquire(key, owner)
	inner := make(chan struct{})
	go func() {
		c.Acquire(key, owner)(false)
		close(inner)
	}()
	select {
	case <-inner:
	case <-time.After(5 * time.Second):
		t.Fatal("reentrant owner deadlocked on its own key lock")
	}
	if lockRefs(c, key) != 1 {
		t.Fatalf("reentrant request registered on key lock: refs = %d", lockRefs(c, key))
	}
	waiters := make(chan struct{}, 2)
	for _, o := range []any{other, nil} {
		go func() {
			c.Acquire(key, o)(true)
			waiters <- struct{}{}
		}()
	}
	if !awaitCondition(func() bool { return lockRefs(c, key) == 3 }, 5*time.Second) {
		t.Fatalf("other owners did not wait: refs = %d", lockRefs(c, key))
	}
	c.Set(key, &Entry{Body: []byte("ok"), TTL: time.Minute})
	release(false)
	<-waiters
	<-waiters
	c.store.Remove(key)
	again := c.Acquire(key, owner)
	if lockRefs(c, key) != 1 {
		t.Fatal("released owner identity was retained")
	}
	again(true)
	if len(c.locks) != 0 {
		t.Fatalf("remaining locks %d", len(c.locks))
	}
}

// TestAcquireAbandoned 验证因取消而放弃的领头请求不登记直通, 已排队的等待者依次接任而不是同时回源;
// 未放弃的未准入释放仍登记直通.
func TestAcquireAbandoned(t *testing.T) {
	c := New(time.Minute, Config{})
	key := [32]byte{1}
	release := c.Acquire(key, nil)
	var active, peak atomic.Int32
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			done := c.Acquire(key, nil)
			defer done(true)
			n := active.Add(1)
			defer active.Add(-1)
			for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
			}
			time.Sleep(time.Millisecond)
		})
	}
	if !awaitCondition(func() bool { return lockRefs(c, key) == 5 }, 5*time.Second) {
		t.Fatalf("waiters did not queue: refs = %d", lockRefs(c, key))
	}
	release(true)
	workers.Wait()
	if peak.Load() != 1 || c.passing(key) || len(c.locks) != 0 {
		t.Fatalf("abandoned leader: peak = %d, passing = %v, locks = %d", peak.Load(), c.passing(key), len(c.locks))
	}
	c.Acquire(key, nil)(false)
	if !c.passing(key) {
		t.Fatal("rejected leader did not mark pass")
	}
}

// lockRefs 读取键锁登记的持有者和等待者总数, 用于确认等待者已经在键锁上排队.
func lockRefs(c *Cache, key [32]byte) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if lock := c.locks[key]; lock != nil {
		return lock.refs
	}
	return 0
}

// awaitCondition 轮询等待条件成立, 超时返回 false 由调用方报告.
func awaitCondition(cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for !cond() {
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(time.Millisecond)
	}
	return true
}

// TestAcquireHitForPass 验证领头请求未准入时, 已排队的等待者全部并发放行, 直通窗口内的新请求
// 不再登记键锁, 直通请求写入可准入响应后窗口结束; 领头请求已准入时等待者仍在锁后命中.
func TestAcquireHitForPass(t *testing.T) {
	for _, admitted := range []bool{true, false} {
		t.Run(map[bool]string{true: "admitted", false: "rejected"}[admitted], func(t *testing.T) {
			const waiters = 8
			c := New(time.Minute, Config{})
			key := [32]byte{1}
			release := c.Acquire(key, nil)
			var active, peak, calls atomic.Int32
			var workers sync.WaitGroup
			for range waiters {
				workers.Go(func() {
					done := c.Acquire(key, nil)
					defer done(false)
					if _, ok := c.Get(key); ok {
						return
					}
					calls.Add(1)
					n := active.Add(1)
					defer active.Add(-1)
					for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
					}
					// 以峰值作屏障: 先结束者会降低 active. 串行实现会在此超时, 并发放行时全部等待者同时到达.
					awaitCondition(func() bool { return peak.Load() >= waiters }, time.Second)
				})
			}
			if !awaitCondition(func() bool { return lockRefs(c, key) == waiters+1 }, 5*time.Second) {
				t.Fatalf("waiters did not queue: refs = %d", lockRefs(c, key))
			}
			if admitted {
				c.Set(key, &Entry{Body: []byte("ok"), TTL: time.Minute})
			}
			release(false)
			workers.Wait()
			if admitted {
				if calls.Load() != 0 || c.passing(key) || len(c.locks) != 0 {
					t.Fatalf("admitted leader: calls = %d, passing = %v, locks = %d", calls.Load(), c.passing(key), len(c.locks))
				}
				return
			}
			if calls.Load() != waiters || peak.Load() != waiters || !c.passing(key) || len(c.locks) != 0 {
				t.Fatalf("rejected leader: calls = %d, peak = %d, passing = %v, locks = %d",
					calls.Load(), peak.Load(), c.passing(key), len(c.locks))
			}
			// 窗口内的新请求不持锁也不登记, 同键并发获得资格互不阻塞.
			first, second := c.Acquire(key, nil), c.Acquire(key, nil)
			if len(c.locks) != 0 {
				t.Fatalf("pass request registered key lock: %d", len(c.locks))
			}
			c.Set(key, &Entry{Body: []byte("ok"), TTL: time.Minute})
			first(false)
			second(false)
			if c.passing(key) {
				t.Fatal("admitted pass response did not end the pass window")
			}
		})
	}
}

// TestPassWindowBounded 在虚拟时间验证直通窗口取 min(ttl, passWindow), 到期后恢复单个领头请求回源,
// 之后准入的响应正常写入且不再触发直通.
func TestPassWindowBounded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		for _, tc := range []struct{ ttl, window time.Duration }{
			{time.Minute, passWindow},
			{time.Second, time.Second},
		} {
			c := New(tc.ttl, Config{})
			key := [32]byte{1}
			c.Acquire(key, nil)(false)
			if !c.passing(key) {
				t.Fatal("rejected leader did not mark pass")
			}
			time.Sleep(tc.window - time.Millisecond)
			if !c.passing(key) {
				t.Fatalf("pass window for ttl %v ended early", tc.ttl)
			}
			time.Sleep(time.Millisecond)
			if c.passing(key) {
				t.Fatalf("pass window for ttl %v exceeded %v", tc.ttl, tc.window)
			}
			release := c.Acquire(key, nil)
			if len(c.locks) != 1 {
				t.Fatal("expired pass window did not restore single-flight")
			}
			c.Set(key, &Entry{Body: []byte("ok"), TTL: time.Minute})
			release(false)
			if _, ok := c.Get(key); !ok || c.passing(key) {
				t.Fatal("admitted response after pass window was not cached")
			}
		}
	})
}

// TestPassMarkersBounded 验证直通标记表容量固定为 min(Capacity, maxPassKeys), 不随请求键增长.
func TestPassMarkersBounded(t *testing.T) {
	for _, tc := range []struct {
		capacity uint32
		want     int
	}{
		{2, 2},
		{maxPassKeys * 4, maxPassKeys},
	} {
		c := New(time.Minute, Config{Capacity: tc.capacity})
		for i := range maxPassKeys + 16 {
			var key [32]byte
			key[0], key[1] = byte(i), byte(i>>8)
			c.Acquire(key, nil)(false)
		}
		if got := c.passes.Len(); got != tc.want || len(c.locks) != 0 {
			t.Fatalf("capacity %d: pass markers = %d, want %d, locks = %d", tc.capacity, got, tc.want, len(c.locks))
		}
	}
}
