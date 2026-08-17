package ntp

import (
	"context"
	"sort"
	"sync"
	"time"
)

const (
	// 特殊值: 无效的 ClockOffset 响应
	invalidClockOffset = 123456 * time.Hour

	// 缺省的请求间隔, 每 3 次返回 1 次结果
	defaultInterval = 2 * time.Hour

	// 最少请求间隔
	defaultMinInterval = 20 * time.Second
)

// 缺省的 NTP Host
var defaultNTPHosts = []string{
	"ntp.aliyun.com",
	"ntp.tencent.com",
	"time.cloudflare.com",
	"time.windows.com",
	"time.apple.com",
	"pool.ntp.org",
}

type HostResponse struct {
	Host string
	Resp *Response
}

// ClockOffsetChan 启动 Simple NTP (SNTP), 周期性获取时钟偏移值.
// interval 等待和向容量 1 通道发送都会观察 ctx, 取消后关闭通道并退出.
func ClockOffsetChan(ctx context.Context, interval time.Duration, hosts ...string) chan time.Duration {
	if interval == 0 {
		interval = defaultInterval
	} else if interval < defaultMinInterval {
		interval = defaultMinInterval
	}
	var offsets []int
	ch := make(chan time.Duration, 1)
	go func() {
		host := ""
		ticker := time.NewTicker(interval)
		defer func() {
			ticker.Stop()
			close(ch)
		}()
		for {
			if ctx.Err() != nil {
				return
			}
			offset := invalidClockOffset
			if host == "" {
				hs := HostPreferred(hosts)
				if hs != nil {
					host = hs.Host
					offset = hs.Resp.ClockOffset
				}
			} else {
				if resp := GetResponse(host); resp != nil {
					offset = resp.ClockOffset
				}
			}
			if offset != invalidClockOffset {
				offsets = append(offsets, int(offset))
				if len(offsets) == 3 {
					// 去头尾, 取中间值; 发送也观察 ctx, 避免容量 1 通道无人消费时卡住退出.
					sort.Ints(offsets)
					if !sendClockOffset(ctx, ch, time.Duration(offsets[1])) {
						return
					}
					offsets = offsets[:0]
				}
			}
			// ticker 等待必须可取消, 生产周期默认 2h, 否则 stopTimeSync 只能干等下一拍.
			if !waitTickerOrDone(ctx, ticker.C) {
				return
			}
		}
	}()
	return ch
}

// TimeChan 启动 Simple NTP (SNTP), 周期性获取最新时间.
// interval 等待和发送都会观察 ctx, 取消后关闭通道并退出.
func TimeChan(ctx context.Context, interval time.Duration, hosts ...string) chan time.Time {
	if interval == 0 {
		interval = defaultInterval
	} else if interval < defaultMinInterval {
		interval = defaultMinInterval
	}
	ch := make(chan time.Time, 1)
	go func() {
		host := ""
		ticker := time.NewTicker(interval)
		defer func() {
			ticker.Stop()
			close(ch)
		}()
		for {
			if ctx.Err() != nil {
				return
			}
			if host == "" {
				hs := HostPreferred(hosts)
				if hs != nil {
					host = hs.Host
					if !sendTimeSample(ctx, ch, hs.Resp.Time) {
						return
					}
				}
			} else if resp := GetResponse(host); resp != nil {
				if !sendTimeSample(ctx, ch, resp.Time) {
					return
				}
			}
			// ticker 等待必须可取消, 与 ClockOffsetChan 保持同一退出语义.
			if !waitTickerOrDone(ctx, ticker.C) {
				return
			}
		}
	}()
	return ch
}

// HostPreferred 并发查询候选 NTP Host, 返回有效响应中 RTT 最短的一项.
// hosts 为空时使用默认列表; 所有请求失败时返回 nil. 相同 RTT 不保证固定 Host 顺序.
func HostPreferred(hosts []string) *HostResponse {
	if len(hosts) == 0 {
		hosts = defaultNTPHosts
	}

	responses := make(map[string]*Response, len(hosts))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, host := range hosts {
		wg.Add(1)
		go func(host string) {
			defer wg.Done()
			if resp := GetResponse(host); resp != nil {
				mu.Lock()
				responses[host] = resp
				mu.Unlock()
			}
		}(host)
	}
	wg.Wait()

	if len(responses) == 0 {
		return nil
	}

	var optimal *HostResponse
	for host, resp := range responses {
		if optimal == nil || optimal.Resp.RTT > resp.RTT {
			optimal = &HostResponse{
				Host: host,
				Resp: resp,
			}
		}
	}
	return optimal
}

// GetResponse 获取 NTP 响应, 无效值返回 nil
func GetResponse(host string) *Response {
	resp, err := Query(host)
	if err == nil && resp.Validate() == nil {
		return resp
	}
	return nil
}

// sendClockOffset 向容量 1 的偏移通道发送一次结果.
// 消费方未读取时不能无限阻塞, 取消后必须立刻放弃发送并退出.
func sendClockOffset(ctx context.Context, ch chan<- time.Duration, offset time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case ch <- offset:
		return true
	}
}

// sendTimeSample 向容量 1 的时间通道发送一次结果, 语义与 sendClockOffset 相同.
func sendTimeSample(ctx context.Context, ch chan<- time.Time, sample time.Time) bool {
	select {
	case <-ctx.Done():
		return false
	case ch <- sample:
		return true
	}
}

// waitTickerOrDone 等待下一拍或 ctx 取消.
// 只读 ticker.C 会让热更新/Stop 卡在整个 interval 上.
func waitTickerOrDone(ctx context.Context, tickerC <-chan time.Time) bool {
	select {
	case <-ctx.Done():
		return false
	case <-tickerC:
		return true
	}
}
