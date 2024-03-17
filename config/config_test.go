package config

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"strings"
	"testing"

	"github.com/fufuok/utils/assert"
)

func TestGetIPNetList(t *testing.T) {
	txt := `
0 .0.0.0,0
256.0.0.0,0
`
	ips := readLinesOffsetN([]byte(txt), 0, -1)
	got, err := getIPNetList(ips)
	assert.True(t, err != nil)
	assert.Contains(t, "0.0.0", err.Error())
	assert.Nil(t, got)

	txt = `# 注释
  
__ 注释
#1.2.3.4
0.0.0.0,0
  1.2.3.4  ,  5  , 注释
 ::1
2001::/64,,注释
`
	ips = readLinesOffsetN([]byte(txt), 0, -1)
	got, err = getIPNetList(ips)

	assert.Nil(t, err)
	assert.Equal(t, 4, len(got))
	n, ok := lookupIPNetsString("0.0.0.0", got)
	assert.True(t, ok)
	assert.Equal(t, int64(0), n)
	n, ok = lookupIPNetsString("1.2.3.4", got)
	assert.True(t, ok)
	assert.Equal(t, int64(5), n)
	n, ok = lookupIPNetsString("2001::1", got)
	assert.True(t, ok)
	assert.Equal(t, int64(0), n)

	n, ok = lookupIPNetsString("2001:0:0:1::", got)
	assert.False(t, ok)
	assert.Equal(t, int64(0), n)
	n, ok = lookupIPNetsString("0.0.0.1", got)
	assert.False(t, ok)
	assert.Equal(t, int64(0), n)
}

// Ref: xfile.ReadLinesOffsetN
func readLinesOffsetN(bs []byte, offset uint, n int) []string {
	var ret []string
	r := bufio.NewReader(bytes.NewReader(bs))
	for i := 0; i < n+int(offset) || n < 0; i++ {
		line, err := r.ReadString('\n')
		if err != nil {
			if err == io.EOF && len(line) > 0 {
				ret = append(ret, strings.Trim(line, "\n"))
			}
			break
		}
		if i < int(offset) {
			continue
		}
		ret = append(ret, strings.Trim(line, "\n"))
	}
	return ret
}

// Ref: common.LookupIPNetsString
func lookupIPNetsString(s string, ipNets map[*net.IPNet]int64) (int64, bool) {
	ip := net.ParseIP(s)
	if ip == nil {
		return 0, false
	}

	return lookupIPNets(ip, ipNets)
}

func lookupIPNets(ip net.IP, ipNets map[*net.IPNet]int64) (int64, bool) {
	for ipNet, val := range ipNets {
		if ipNet.Contains(ip) {
			return val, true
		}
	}
	return 0, false
}
