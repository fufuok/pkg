//go:build linux

package stats

import (
	"bufio"
	"errors"
	"os"
	"strconv"
	"strings"
)

// systemFileHandles 读取内核文件句柄总量. file-nr 三列依次为已分配、未使用和上限,
// 这里只返回前两列相减后的使用量, 与单个进程 FD 不是同一统计口径.
func systemFileHandles() (used, limit uint64, err error) {
	data, err := os.ReadFile("/proc/sys/fs/file-nr")
	if err != nil {
		return 0, 0, err
	}
	return parseSystemFileHandles(string(data))
}

// readTCPExtCounters 读取内核 TcpExt 中与监听溢出相关的累计计数.
func readTCPExtCounters() (map[string]int64, error) {
	file, err := os.Open("/proc/net/netstat")
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		header := scanner.Text()
		if !strings.HasPrefix(header, "TcpExt:") {
			continue
		}
		if !scanner.Scan() {
			break
		}
		values := scanner.Text()
		result, err := parseTcpExtLine(header, values)
		if err != nil {
			return nil, err
		}
		if len(result) == 0 {
			return nil, errors.New("TcpExt listen counters not found")
		}
		return result, nil
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, errors.New("TcpExt counters not found")
}

// parseSystemFileHandles 解析 file-nr 的固定三列, 对列缺失、非数字和计数回退返回错误.
func parseSystemFileHandles(text string) (used, limit uint64, err error) {
	fields := strings.Fields(text)
	if len(fields) < 3 {
		return 0, 0, errors.New("invalid file-nr format")
	}
	allocated, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	unused, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	limit, err = strconv.ParseUint(fields[2], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	if allocated < unused {
		return 0, 0, errors.New("invalid file-nr counters")
	}
	return allocated - unused, limit, nil
}

// parseTcpExtLine 按 TcpExt 头部列名匹配所需字段, 忽略内核版本新增列.
func parseTcpExtLine(header, values string) (map[string]int64, error) {
	headerFields := strings.Fields(strings.TrimPrefix(header, "TcpExt:"))
	valueFields := strings.Fields(strings.TrimPrefix(values, "TcpExt:"))
	if len(headerFields) != len(valueFields) {
		return nil, errors.New("invalid TcpExt format")
	}
	result := make(map[string]int64, 2)
	for i, name := range headerFields {
		if name != "ListenOverflows" && name != "ListenDrops" {
			continue
		}
		value, err := strconv.ParseInt(valueFields[i], 10, 64)
		if err != nil {
			return nil, err
		}
		result[name] = value
	}
	return result, nil
}
