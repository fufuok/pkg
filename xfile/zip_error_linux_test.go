package xfile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// TestZipDirFinalizeError 用真实设备触发延迟到 ZIP 收尾的写错误,
// 并确认收尾失败不会覆盖遍历阶段的原始错误.
func TestZipDirFinalizeError(t *testing.T) {
	if _, err := os.Stat("/dev/full"); err != nil {
		t.Skipf("/dev/full is unavailable: %v", err)
	}
	root := t.TempDir()
	err := ZipDir(root, "/dev/full")
	if !errors.Is(err, syscall.ENOSPC) || !strings.Contains(err.Error(), "failed to finalize zip file") {
		t.Fatalf("expected ZIP finalize ENOSPC, got %v", err)
	}
	err = ZipDir(filepath.Join(root, "missing"), "/dev/full")
	if !errors.Is(err, os.ErrNotExist) || !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("expected both walk and finalize errors, got %v", err)
	}
}
