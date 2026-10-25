package httpcache

import (
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// TestConfigDefaultsAndIsolation 验证安全默认值、配置去重和构造后的切片隔离.
func TestConfigDefaultsAndIsolation(t *testing.T) {
	vary := []string{"x-tenant", " ACCEPT ", "content-type", "Content-Encoding"}
	c := New(0, Config{Vary: vary})
	if c.Config.Capacity != 256 || c.Config.MaxBodyBytes != 256<<10 ||
		c.Config.MaxRequestBodyBytes != 64<<10 || c.ttl != 10*time.Minute {
		t.Fatalf("unexpected defaults: %+v, ttl %v", c.Config, c.ttl)
	}
	if got := strings.Join(c.Config.Vary, ","); got != "Accept,Accept-Encoding,Accept-Language,Origin,X-Tenant" {
		t.Fatalf("unexpected Vary: %s", got)
	}
	req := Request{Header: func(name string) string { return name }}
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

// TestStoreCopiesAndBounds 验证 Set 隔离来源缓冲区, 同时拒绝过大条目和无效 TTL.
func TestStoreCopiesAndBounds(t *testing.T) {
	c := New(time.Minute, Config{MaxBodyBytes: 3})
	key := [32]byte{1}
	source := &Entry{Header: http.Header{"Content-Type": {"text/plain"}}, Body: []byte("abc"), TTL: time.Hour}
	c.Set(key, source)
	source.Body[0] = 'z'
	source.Header.Set("Content-Type", "changed")
	got, ok := c.Get(key)
	if !ok || string(got.Body) != "abc" || got.Header.Get("Content-Type") != "text/plain" || got.TTL != time.Minute {
		t.Fatalf("cache should hold an isolated capped entry: %+v, %v", got, ok)
	}
	for _, entry := range []*Entry{
		nil,
		{Body: []byte("abcd"), TTL: time.Second},
		{TTL: 0},
		{TTL: -time.Second},
		{Header: http.Header{"X-Large": {strings.Repeat("x", maxHeaderBytes)}}, TTL: time.Second},
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
		if !ok || entry.TTL != 3*time.Second || entry.ReplayHeader().Get("Age") != "2" {
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

// TestReplayHeaderAge 验证命中保留缓存指令、保守计算 Age, 且回放头与缓存互不别名.
func TestReplayHeaderAge(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := New(time.Minute, Config{})
		key := [32]byte{1}
		c.Set(key, &Entry{Header: http.Header{"Cache-Control": {"public, max-age=60"}}, TTL: time.Minute})
		entry, _ := c.Get(key)
		if age := entry.ReplayHeader().Get("Age"); age != "0" {
			t.Fatalf("initial age %s", age)
		}
		time.Sleep(200 * time.Millisecond)
		header := entry.ReplayHeader()
		if header.Get("Age") != "1" || header.Get("Cache-Control") != "public, max-age=60" {
			t.Fatalf("unexpected replay headers: %v", header)
		}
		header.Set("Cache-Control", "private")
		time.Sleep(800 * time.Millisecond)
		if age := entry.ReplayHeader().Get("Age"); age != "1" {
			t.Fatalf("whole second age %s", age)
		}
		time.Sleep(time.Nanosecond)
		if age := entry.ReplayHeader().Get("Age"); age != "2" {
			t.Fatalf("fractional second age %s", age)
		}
		if entry.Header.Get("Age") != "" || entry.Header.Get("Cache-Control") != "public, max-age=60" {
			t.Fatal("replay mutated stored headers")
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
					release := c.Acquire(key)
					defer release()
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
	releaseFirst := c.Acquire([32]byte{1})
	defer releaseFirst()
	finished := make(chan struct{})
	go func() {
		release := c.Acquire([32]byte{2})
		release()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("unrelated key was blocked")
	}
}
