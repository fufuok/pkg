//go:build !linux

package stats

import "errors"

// systemFileHandles 在不支持 /proc 的平台返回未知, 不伪造零值.
func systemFileHandles() (used, limit uint64, err error) {
	return 0, 0, errors.New("system file handles are unsupported")
}

// readTCPExtCounters 在不支持 Linux TcpExt 的平台返回未知.
func readTCPExtCounters() (map[string]int64, error) {
	return nil, errors.New("TcpExt counters are unsupported")
}
