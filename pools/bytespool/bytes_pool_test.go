package bytespool

import (
	"bytes"
	"fmt"
	"testing"
)

// TestCapacityPools 验证容量刻度、长度和超限回收语义.
// 测试修改包级容量上限, 所有退出路径都必须恢复调用前状态.
func TestCapacityPools(t *testing.T) {
	originalMaxSize := defaultMaxSize
	t.Cleanup(func() {
		defaultMaxSize = originalMaxSize
	})

	maxSize := 2048
	if !SetMaxSize(maxSize) {
		t.Fatalf("set test max size to %d", maxSize)
	}
	tests := []struct {
		size        int
		scaleSize   int
		bytesLength int
		releaseOK   bool
	}{
		{-1, minCapacity, 0, true},
		{0, minCapacity, 0, true},
		{minCapacity, minCapacity, minCapacity, true},
		{2, 2, 2, true},
		{64, 64, 64, true},
		{128, 128, 128, true},
		{2000, 2048, 2000, true},
		{2047, 2048, 2047, true},
		{maxSize, maxSize, maxSize, true},
		{4096, 0, 4096, false},
		{5000, 0, 5000, false},
	}
	for _, v := range tests {
		t.Run(fmt.Sprintf("bytes.Get(%d)", v.size), func(t *testing.T) {
			buf := Make(v.size)
			if buf == nil {
				t.Fatal("expect  buf != nil")
			}
			if len(buf) != 0 {
				t.Fatalf("expect buffer len is 0, but got %d", len(buf))
			}
			if cap(buf) < v.scaleSize {
				t.Fatalf("expect buffer cap >= %d, but got %d", v.scaleSize, cap(buf))
			}

			buf = Get(v.size)
			if len(buf) != v.bytesLength {
				t.Fatalf("expect buffer len is %d, but got %d", v.bytesLength, len(buf))
			}
			if cap(buf) < v.scaleSize {
				t.Fatalf("expect buffer cap >= %d, but got %d", v.scaleSize, cap(buf))
			}

			ok := Release(buf)
			if ok != v.releaseOK {
				t.Fatalf("expect to release the buffer result is %v, but got %v", v.releaseOK, ok)
			}
		})
	}
}

// TestCapacityPools_Default 验证默认容量上限、回收契约和调整上限后的行为.
// sync.Pool 可随时丢弃对象, 因此测试只验证返回切片的公开语义, 不要求复用具体数组或旧数据.
func TestCapacityPools_Default(t *testing.T) {
	originalMaxSize := defaultMaxSize
	t.Cleanup(func() {
		defaultMaxSize = originalMaxSize
	})

	buf := Make(defaultMaxSize + 1)
	if len(buf) != 0 {
		t.Fatalf("expect buffer len is 0, but got %d", len(buf))
	}
	if cap(buf) <= defaultMaxSize {
		t.Fatalf("expect buffer cap > %d, but got %d", defaultMaxSize, cap(buf))
	}
	if Release(buf) {
		t.Fatal("expect to release the buffer failure, but not")
	}

	buf = Make(defaultMaxSize)
	if len(buf) != 0 {
		t.Fatalf("expect buffer len is 0, but got %d", len(buf))
	}
	if cap(buf) != defaultMaxSize {
		t.Fatalf("expect buffer cap is %d, but got %d", defaultMaxSize, cap(buf))
	}

	len0 := make([]byte, 0, 8)
	if !Release(len0) {
		t.Fatal("expect to release the buffer successfully, but not")
	}

	var cap0 []byte
	if Release(cap0) {
		t.Fatal("expect to release the buffer failure, but not")
	}

	abc := []byte("abc")
	buf = append(buf, abc...)

	if !Release(buf) {
		t.Fatal("expect to release the buffer successfully, but not")
	}

	newBuf := Get(defaultMaxSize)
	if len(newBuf) != defaultMaxSize || cap(newBuf) != defaultMaxSize {
		t.Fatalf("expect newBuf len and cap to be %d, but got len=%d cap=%d", defaultMaxSize, len(newBuf), cap(newBuf))
	}

	if !Release(newBuf) {
		t.Fatal("expect to release the buffer successfully, but not")
	}

	buf8 := Get(8)
	if len(buf8) != 8 || cap(buf8) != 8 {
		t.Fatalf("expect buf8 len and cap to be 8, but got len=%d cap=%d", len(buf8), cap(buf8))
	}
	copy(buf8, "12345678")
	if string(buf8) != "12345678" {
		t.Fatal("expect copy result is 12345678, but not")
	}

	buf8 = append(buf8, '9')
	Put(buf8)

	buf16 := Get(16)
	if len(buf16) != 16 || cap(buf16) != 16 {
		t.Fatalf("expect buf16 len and cap to be 16, but got len=%d cap=%d", len(buf16), cap(buf16))
	}

	if !Release(buf16) {
		t.Fatal("expect to release buf16 successfully, but not")
	}

	if !SetMaxSize(smallBufferSize) {
		t.Fatalf("set test max size to %d", smallBufferSize)
	}

	buf = Make(3)
	if len(buf) != 0 {
		t.Fatalf("expect buffer len is 0, but got %d", len(buf))
	}
	if cap(buf) != 4 {
		t.Fatalf("expect buffer cap is 4, but got %d", cap(buf))
	}
	buf = Make(smallBufferSize + 3)
	if len(buf) != 0 {
		t.Fatalf("expect buffer len is 0, but got %d", len(buf))
	}
	if cap(buf) != smallBufferSize+3 {
		t.Fatalf("expect buffer cap is smallBufferSize+3, but got %d", cap(buf))
	}
	if Release(buf) {
		t.Fatal("expect to release the buffer failure, but not")
	}
	buf = append(buf, '1')
	if Release(buf) {
		t.Fatal("expect to release the buffer failure, but not")
	}
}

func TestNewBytesString(t *testing.T) {
	s := "Fufu 中文-123"
	bs := []byte(s)

	buf := NewString(s)
	if cap(buf) != 16 {
		t.Fatalf("expect buffer cap is 16, but got %d", cap(buf))
	}
	if string(buf) != s {
		t.Fatalf("expect buf to be equal to %s, but not", s)
	}

	buf = NewBytes(bs)
	if cap(buf) != 16 {
		t.Fatalf("expect buffer cap is 16, but got %d", cap(buf))
	}
	if !bytes.Equal(buf, bs) {
		t.Fatalf("expect buf to be equal to %s, but not", bs)
	}
}

func TestUnalignedCapacity(t *testing.T) {
	bs := make([]byte, 0, 7)
	bs = append(bs, "123"...)
	if !Release(bs) {
		t.Fatal("expect to release the buffer successfully, but not")
	}
	buf := Make(3)
	if cap(buf) != 4 {
		t.Fatalf("expect buffer cap is 4, but got %d", cap(buf))
	}
	if !Release(buf) {
		t.Fatal("expect to release the buffer successfully, but not")
	}
}

// TestAppend 验证扩容 append 会切换底层数组, 容量足够时 AppendString 会复用原数组.
// 这些语义不依赖 sync.Pool 命中或全局 GC 设置.
func TestAppend(t *testing.T) {
	buf := Get(4)
	if len(buf) != 4 || cap(buf) != 4 {
		t.Fatalf("expect buf cap is 4, but got %d", cap(buf))
	}

	copy(buf, "1234")
	bbuf := Append(buf, '+')
	if len(bbuf) != 5 || cap(bbuf) != 8 {
		t.Fatalf("expect bbuf cap is 8, but got %d", cap(bbuf))
	}
	// Warning: you should stop using (buf)!
	if len(buf) != 4 || cap(buf) != 4 {
		t.Fatalf("expect buf cap is 4, but got %d", cap(buf))
	}

	if !bytes.EqualFold(bbuf, []byte("1234+")) || !bytes.EqualFold(buf, []byte("1234")) {
		t.Fatalf("expect bbuf is 1234+, buf is 1234")
	}

	if &bbuf[0] == &buf[0] {
		t.Fatal("expect bbuf and buf to not be the same array")
	}

	cbuf := AppendString(bbuf, "+2")
	if len(bbuf) != 5 || cap(bbuf) != 8 {
		t.Fatalf("expect bbuf len=5 cap=8, but got len=%d cap=%d", len(bbuf), cap(bbuf))
	}
	if len(cbuf) != 7 || cap(cbuf) != 8 {
		t.Fatalf("expect cbuf len=7 cap=8, but got len=%d cap=%d", len(cbuf), cap(cbuf))
	}
	if string(cbuf) != "1234++2" {
		t.Fatalf("expect cbuf is 1234++2, but got %q", cbuf)
	}

	if &cbuf[0] != &bbuf[0] {
		t.Fatal("expect cbuf and bbuf to be the same array")
	}
}
