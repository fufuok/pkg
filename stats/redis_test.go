package stats

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/redis/go-redis/v9/maintnotifications"

	"github.com/fufuok/pkg/common"
)

// respFixture 为 go-redis 提供只支持当前统计命令的内存 RESP2 服务.
type respFixture struct {
	listener net.Listener
	dbSize   int64
	info     string
	wg       sync.WaitGroup
	mu       sync.Mutex
	seen     []string
}

// TestRedisStatsUninitializedAndFailure 验证未初始化及 Redis 命令失败回退.
func TestRedisStatsUninitializedAndFailure(t *testing.T) {
	preserveRedisState(t)
	common.InitRedisDB(nil)
	if RedisStats() != nil || RedisInfo() != nil || RedisDBSize() != -1 {
		t.Fatal("uninitialized Redis stats did not use nil/-1 sentinels")
	}

	client := redis.NewClient(&redis.Options{
		Addr:               "fixture.invalid:6379",
		PoolSize:           2,
		MaxRetries:         -1,
		DialerRetries:      1,
		DialerRetryTimeout: time.Nanosecond,
		Dialer: func(context.Context, string, string) (net.Conn, error) {
			return nil, errors.New("fixture dial failed")
		},
	})
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("close failed Redis fixture client: %v", err)
		}
	})
	common.InitRedisDB(client)
	if got := RedisDBSize(); got != -1 {
		t.Fatalf("failed Redis DBSize = %d, want -1", got)
	}
	stats := RedisStats()
	if stats["Addr"] != "fixture.invalid:6379" || stats["PoolSize"] != 2 || stats["DBSize"] != -1 {
		t.Fatalf("failed Redis stats = %#v", stats)
	}
}

// TestRedisStatsWithRESPFixture 验证 DBSize、INFO 解析和连接池选项映射.
func TestRedisStatsWithRESPFixture(t *testing.T) {
	preserveRedisState(t)
	fixture := startRESPFixture(t, 3, "# Server\r\nredis_version:7.2.0\r\nconnected_clients:5\r\n")
	client := redis.NewClient(&redis.Options{
		Addr:            fixture.listener.Addr().String(),
		DB:              2,
		PoolSize:        4,
		Protocol:        2,
		DisableIdentity: true,
		MaintNotificationsConfig: &maintnotifications.Config{
			Mode: maintnotifications.ModeDisabled,
		},
		DialerRetries: 1,
		ReadTimeout:   100 * time.Millisecond,
		WriteTimeout:  100 * time.Millisecond,
	})
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("close Redis fixture client: %v", err)
		}
	})
	common.InitRedisDB(client)

	if got := RedisDBSize(); got != 3 {
		t.Fatalf("Redis DBSize = %d, want 3", got)
	}
	info := RedisInfo()
	if info["redis_version"] != "7.2.0" || info["connected_clients"] != "5" {
		t.Fatalf("Redis INFO = %#v", info)
	}
	stats := RedisStats()
	assertMapValues(t, stats, map[string]any{
		"Addr":     fixture.listener.Addr().String(),
		"DB":       2,
		"PoolSize": 4,
		"DBSize":   3,
	})
	fixture.mu.Lock()
	seen := strings.Join(fixture.seen, ",")
	fixture.mu.Unlock()
	if !strings.Contains(seen, "dbsize") || !strings.Contains(seen, "info") {
		t.Fatalf("RESP fixture commands = %q", seen)
	}
}

// TestRedisInfoSkipsMalformedLines 验证 INFO 夹杂无冒号行时不 panic, 正常键仍保留.
func TestRedisInfoSkipsMalformedLines(t *testing.T) {
	preserveRedisState(t)
	fixture := startRESPFixture(t, 1, strings.Join([]string{
		"# Server",
		"redis_version:7.2.0",
		"broken-without-colon",
		"",
		"# Clients",
		"connected_clients:5",
		"empty_value:",
	}, "\r\n")+"\r\n")
	client := redis.NewClient(&redis.Options{
		Addr:            fixture.listener.Addr().String(),
		Protocol:        2,
		DisableIdentity: true,
		MaintNotificationsConfig: &maintnotifications.Config{
			Mode: maintnotifications.ModeDisabled,
		},
		DialerRetries: 1,
		ReadTimeout:   100 * time.Millisecond,
		WriteTimeout:  100 * time.Millisecond,
	})
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("close Redis fixture client: %v", err)
		}
	})
	common.InitRedisDB(client)

	info := RedisInfo()
	if info["redis_version"] != "7.2.0" || info["connected_clients"] != "5" {
		t.Fatalf("Redis INFO = %#v", info)
	}
	if info["empty_value"] != "" {
		t.Fatalf("empty INFO value = %#v", info["empty_value"])
	}
	if _, ok := info["broken-without-colon"]; ok {
		t.Fatalf("malformed INFO line was stored: %#v", info)
	}
}

// preserveRedisState 保存 common Redis 全局状态并在测试结束时恢复.
// stats 测试会串行替换客户端, 不支持与其他 Redis 状态测试并行执行.
func preserveRedisState(t *testing.T) {
	t.Helper()
	oldDB := common.RedisDB
	oldInited := common.RedisDBInited.Load()
	t.Cleanup(func() {
		common.RedisDB = oldDB
		common.RedisDBInited.Store(oldInited)
	})
}

// startRESPFixture 在回环地址启动最小 RESP2 服务, 测试结束时关闭并等待全部连接.
func startRESPFixture(t *testing.T, dbSize int64, info string) *respFixture {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for Redis fixture: %v", err)
	}
	fixture := &respFixture{listener: listener, dbSize: dbSize, info: info}
	fixture.wg.Go(func() {
		fixture.accept()
	})
	t.Cleanup(func() {
		if err := fixture.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			t.Errorf("close Redis fixture listener: %v", err)
		}
		fixture.wg.Wait()
	})
	return fixture
}

// accept 接收本地 Redis 连接, 每个连接由独立 goroutine 处理.
func (f *respFixture) accept() {
	for {
		conn, err := f.listener.Accept()
		if err != nil {
			return
		}
		f.wg.Go(func() {
			f.serve(conn)
		})
	}
}

// serve 按 RESP2 协议处理 SELECT、DBSIZE 和 INFO, 其他命令返回显式错误.
func (f *respFixture) serve(conn net.Conn) {
	defer func() {
		_ = conn.Close()
	}()
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	for {
		command, err := readRESPCommand(reader)
		if err != nil {
			return
		}
		f.mu.Lock()
		f.seen = append(f.seen, strings.Join(command, " "))
		f.mu.Unlock()
		switch strings.ToLower(command[0]) {
		case "select":
			_, err = writer.WriteString("+OK\r\n")
		case "dbsize":
			_, err = fmt.Fprintf(writer, ":%d\r\n", f.dbSize)
		case "info":
			_, err = fmt.Fprintf(writer, "$%d\r\n%s\r\n", len(f.info), f.info)
		case "ping":
			_, err = writer.WriteString("+PONG\r\n")
		default:
			_, err = writer.WriteString("-ERR unsupported fixture command\r\n")
		}
		if err != nil || writer.Flush() != nil {
			return
		}
	}
}

// readRESPCommand 解析 go-redis 发出的 RESP2 数组命令, 非数组或短读取返回错误.
func readRESPCommand(reader *bufio.Reader) ([]string, error) {
	header, err := readRESPLine(reader)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(header, "*") {
		return nil, fmt.Errorf("invalid RESP array header: %q", header)
	}
	count, err := strconv.Atoi(header[1:])
	if err != nil || count <= 0 {
		return nil, fmt.Errorf("invalid RESP array size: %q", header)
	}
	command := make([]string, count)
	for i := range count {
		bulkHeader, err := readRESPLine(reader)
		if err != nil {
			return nil, err
		}
		if !strings.HasPrefix(bulkHeader, "$") {
			return nil, fmt.Errorf("invalid RESP bulk header: %q", bulkHeader)
		}
		size, err := strconv.Atoi(bulkHeader[1:])
		if err != nil || size < 0 {
			return nil, fmt.Errorf("invalid RESP bulk size: %q", bulkHeader)
		}
		payload := make([]byte, size+2)
		if _, err := io.ReadFull(reader, payload); err != nil {
			return nil, err
		}
		if payload[size] != '\r' || payload[size+1] != '\n' {
			return nil, errors.New("invalid RESP bulk terminator")
		}
		command[i] = string(payload[:size])
	}
	return command, nil
}

// readRESPLine 读取并去除 RESP 行末的 CRLF.
func readRESPLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	if !strings.HasSuffix(line, "\r\n") {
		return "", fmt.Errorf("invalid RESP line terminator: %q", line)
	}
	return strings.TrimSuffix(line, "\r\n"), nil
}
