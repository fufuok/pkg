package xhash

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

// TestMD5ReaderReadBoundaries 验证末次数据与 EOF 同时返回时仍计入摘要, 读取失败不返回部分摘要.
func TestMD5ReaderReadBoundaries(t *testing.T) {
	t.Parallel()
	readErr := errors.New("read failed")
	longInput := strings.Repeat("abc", 30000)
	tests := []struct {
		name    string
		reader  io.Reader
		input   string
		wantErr error
	}{
		{name: "empty", reader: strings.NewReader("")},
		{name: "separate EOF", reader: strings.NewReader("abc"), input: "abc"},
		{name: "data with EOF", reader: iotest.DataErrReader(strings.NewReader("abc")), input: "abc"},
		{name: "multiple reads with EOF", reader: iotest.DataErrReader(strings.NewReader(longInput)), input: longInput},
		{name: "read error", reader: iotest.ErrReader(readErr), wantErr: readErr},
		{
			name:    "data with read error",
			reader:  iotest.DataErrReader(io.MultiReader(strings.NewReader("abc"), iotest.ErrReader(readErr))),
			wantErr: readErr,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MD5Reader(tt.reader)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("MD5Reader error = %v, want %v", err, tt.wantErr)
			}
			want := ""
			if tt.wantErr == nil {
				sum := md5.Sum([]byte(tt.input))
				want = hex.EncodeToString(sum[:])
			}
			if got != want {
				t.Fatalf("MD5Reader digest = %q, want %q", got, want)
			}
		})
	}
}
