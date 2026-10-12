//go:build linux

package stats

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
)

// collectNetworkStats 只读接口与协议累计计数, 各文件失败仅影响对应字段.
// /proc/net 指向当前网络命名空间, 此处不读取 tcp/udp 连接明细表.
func collectNetworkStats() map[string]any {
	values := make(map[string]any)
	if interfaces, err := readNetworkInterfaces(); err == nil {
		values["Interfaces"] = interfaces
	} else {
		addStatsError(values, "Errors", "interfaces")
	}
	protocols, err := readProtocolCounters("net/snmp", map[string][]string{
		"Udp": {"InDatagrams", "OutDatagrams", "InErrors", "RcvbufErrors", "SndbufErrors", "NoPorts"},
		"Tcp": {"OutSegs", "RetransSegs"},
	})
	if err == nil {
		if udp, ok := protocols["Udp"]; ok {
			values["UDP"] = udp
		}
		if tcp, ok := protocols["Tcp"]; ok {
			values["TCP"] = tcp
		}
	} else {
		addStatsError(values, "Errors", "protocols")
	}
	ext, err := readProtocolCounters("net/netstat", map[string][]string{"TcpExt": {"ListenOverflows", "ListenDrops"}})
	if err == nil {
		tcp, ok := values["TCP"].(map[string]int64)
		if !ok {
			tcp = make(map[string]int64)
			values["TCP"] = tcp
		}
		for name, count := range ext["TcpExt"] {
			tcp[name] = count
		}
	} else {
		addStatsError(values, "Errors", "tcp_ext")
	}
	return values
}

// readNetworkInterfaces 读取 net/dev 的8项必要计数, 不合计虚拟接口或隧道的重复流量.
func readNetworkInterfaces() (map[string]any, error) {
	data, err := os.ReadFile(procPath("net/dev"))
	if err != nil {
		return nil, err
	}
	interfaces := make(map[string]any)
	keys := []string{"BytesRecv", "PacketsRecv", "ErrorsRecv", "DropsRecv", "BytesSent", "PacketsSent", "ErrorsSent", "DropsSent"}
	for line := range strings.SplitSeq(string(data), "\n") {
		end := strings.LastIndexByte(line, ':')
		if end < 0 {
			continue
		}
		fields := strings.Fields(line[end+1:])
		if len(fields) < 16 {
			return nil, errors.New("invalid net/dev format")
		}
		counters := make(map[string]any, len(keys))
		for i, key := range keys {
			index := i
			if i >= 4 {
				index += 4
			}
			value, err := strconv.ParseUint(fields[index], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid interface %s counter: %w", key, err)
			}
			counters[key] = value
		}
		interfaces[strings.TrimSpace(line[:end])] = counters
	}
	return interfaces, nil
}

// readProtocolCounters 按头部列名选择所需协议计数, 忽略内核新增字段与无关协议.
func readProtocolCounters(name string, wanted map[string][]string) (map[string]map[string]int64, error) {
	file, err := os.Open(procPath(name))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	result := make(map[string]map[string]int64, len(wanted))
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		protocol, header, ok := strings.Cut(scanner.Text(), ":")
		keys, wantedProtocol := wanted[protocol]
		if !ok || !wantedProtocol {
			continue
		}
		if !scanner.Scan() {
			return nil, fmt.Errorf("missing %s counters in %s", protocol, name)
		}
		valueProtocol, text, ok := strings.Cut(scanner.Text(), ":")
		if !ok || valueProtocol != protocol {
			return nil, fmt.Errorf("invalid %s header in %s", protocol, name)
		}
		values, err := parseProtocolLine(header, text, keys)
		if err != nil {
			return nil, fmt.Errorf("invalid %s in %s: %w", protocol, name, err)
		}
		result[protocol] = values
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("protocol counters not found in %s", name)
	}
	return result, nil
}

// parseProtocolLine 保持协议原始 int64 类型, 只解析对外暴露的字段.
func parseProtocolLine(header, text string, keys []string) (map[string]int64, error) {
	fields, counts := strings.Fields(header), strings.Fields(text)
	if len(fields) != len(counts) {
		return nil, errors.New("mismatched protocol columns")
	}
	values := make(map[string]int64, len(keys))
	for i, key := range fields {
		if !slices.Contains(keys, key) {
			continue
		}
		value, err := strconv.ParseInt(counts[i], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid %s counter: %w", key, err)
		}
		values[key] = value
	}
	return values, nil
}
