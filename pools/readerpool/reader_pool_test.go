package readerpool

import (
	"fmt"
	"io"
	"sync"
	"testing"

	"github.com/fufuok/pkg/assert"
)

// TestNewAndRelease 验证 New 会完整重置读取内容, Release 会清空调用方仍持有的 Reader.
// sync.Pool 可以随时丢弃对象, 因此测试只检查公开读取语义, 不要求返回同一指针.
func TestNewAndRelease(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{name: "nil", data: nil},
		{name: "empty", data: []byte{}},
		{name: "binary", data: []byte{0, 1, 2, 0xff}},
		{name: "utf8", data: []byte("Fufu 中文-123")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := New(tt.data)
			assert.NotNil(t, reader)
			assert.Equal(t, len(tt.data), reader.Len())
			assert.Equal(t, int64(len(tt.data)), reader.Size())

			got, err := io.ReadAll(reader)
			if err != nil {
				t.Fatalf("read pooled reader: %v", err)
			}
			assert.Equal(t, string(tt.data), string(got))

			Release(reader)
			assert.Equal(t, 0, reader.Len())
			assert.Equal(t, int64(0), reader.Size())
			_, err = reader.ReadByte()
			assert.Equal(t, io.EOF, err)
		})
	}
}

// TestConcurrentNewAndRelease 验证多个调用方并发获取、读取和归还 Reader 时内容不会串扰.
// worker 不直接调用 testing.T, 所有错误都回传给主测试 goroutine 统一断言.
func TestConcurrentNewAndRelease(t *testing.T) {
	const (
		workers = 16
		rounds  = 100
	)

	var wg sync.WaitGroup
	errors := make(chan error, workers)
	for worker := range workers {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for round := range rounds {
				want := fmt.Sprintf("worker=%d round=%d", worker, round)
				reader := New([]byte(want))
				got, err := io.ReadAll(reader)
				Release(reader)
				if err != nil {
					errors <- fmt.Errorf("worker %d round %d read: %w", worker, round, err)
					return
				}
				if string(got) != want {
					errors <- fmt.Errorf("worker %d round %d got %q want %q", worker, round, got, want)
					return
				}
			}
		}(worker)
	}
	wg.Wait()
	close(errors)

	for err := range errors {
		assert.Nil(t, err)
	}
}
