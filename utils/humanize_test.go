package utils

import (
	"math"
	"testing"

	"github.com/fufuok/pkg/assert"
)

// TestHumanBaseBytes 验证固定两位的舍入, 单位边界和自定义单位上限.
func TestHumanBaseBytes(t *testing.T) {
	sizes := []string{"B", "KB", "MB"}
	tests := []struct {
		in   uint64
		base float64
		want string
	}{
		{0, 1000, "0 B"},
		{42, 1000, "42 B"},
		{42000000, 1000, "42.00 MB"},
		{44040192, 1024, "42.00 MB"},
		{44040192123, 1024, "42000.00 MB"},
		{31450000, 1000, "31.45 MB"},
		{31500000, 1000, "31.50 MB"},
		{30500000, 1000, "30.50 MB"},
		{32500000, 1000, "32.50 MB"},
		{33500000, 1000, "33.50 MB"},
		{32977715, 1024, "31.45 MB"},
		{34078720, 1024, "32.50 MB"},
		{9950, 1000, "9.95 KB"},
		// 半分边界按原始整数四舍五入, 避免浮点表示略小于半分而向下取整.
		{1004, 1000, "1.00 KB"},
		{1005, 1000, "1.01 KB"},
		{1015, 1000, "1.02 KB"},
		{1025, 1000, "1.03 KB"},
		{1045, 1000, "1.05 KB"},
		{1995, 1000, "2.00 KB"},
		{9995, 1000, "10.00 KB"},
		{1152, 1024, "1.13 KB"},
		// 舍入后的单位不再进位, 自定义基数与单位上限继续有效.
		{999999, 1000, "1000.00 KB"},
		{MiByte - 1, 1024, "1024.00 KB"},
		{12345, 10, "123.45 MB"},
		{26, 2.5, "4.16 MB"},
	}
	for _, v := range tests {
		got := HumanBaseBytes(v.in, v.base, sizes)
		if got != v.want {
			t.Errorf("%s != %s", got, v.want)
		}
	}
}

// TestHumanGBMB 验证组合显示只提高余量精度, 整数 GB 与原始 B 的格式保持.
func TestHumanGBMB(t *testing.T) {
	tests := []struct {
		in   uint64
		want string
	}{
		{0, "0 B"},
		{42, "42 B"},
		{42000000, "40.05 MB"},
		{44040192, "42.00 MB"},
		{44040192123, "41 GB 16.00 MB"},
		{GiByte, "1 GB 0 B"},
		{GiByte + 42, "1 GB 42 B"},
	}
	for _, v := range tests {
		got := HumanGBMB(v.in)
		if got != v.want {
			t.Errorf("%s != %s", got, v.want)
		}
	}
}

// TestParseHumanBytes 验证原始整数和多位小数语法, 包括 TiB/GiB 显示值的字节截断.
func TestParseHumanBytes(t *testing.T) {
	tests := []struct {
		in  string
		exp uint64
	}{
		{"42", 42},
		{"9007199254740993", 9007199254740993},
		{"9,007,199,254,740,993 B", 9007199254740993},
		{"18446744073709551615", math.MaxUint64},
		{"18446744073709551615 B", math.MaxUint64},
		{"18446744073709551615 bps", math.MaxUint64},
		{"18446744073709551 KB", 18446744073709551000},
		{"18014398509481983 KiB", 18446744073709550592},
		{"18446744073709 Mbps", 18446744073709000000},
		{"42MB", 42000000},
		{"42MiB", 44040192},
		{"42mb", 42000000},
		{"42mbps", 42000000},
		{"42mib", 44040192},
		{"42MIB", 44040192},
		{"42 MB", 42000000},
		{"42 MiB", 44040192},
		{"42 mb", 42000000},
		{"42 mib", 44040192},
		{"42 MIB", 44040192},
		{"42.5MB", 42500000},
		{"42.5MiB", 44564480},
		{"42.5 MB", 42500000},
		{"42.5\tMB\n", 42500000},
		{".5 B", 0},
		{"42. B", 42},
		// 两位小数按单位换算后截去不足一字节的部分, 不将展示文本当作无损存储.
		{"2.03 TiB", 2232008604385},
		{"2.03TiB", 2232008604385},
		{"2.030 tib", 2232008604385},
		{"707.12 GiB", 759264318586},
		{"707.12GiB", 759264318586},
		{"707.120 GIB", 759264318586},
		{"2.00 TiB", 2 * TiByte},
		{"0.00 GiB", 0},
		{"0.99 B", 0},
		{"707.12 Gbps", 707120000000},
		{"42.5 Mbps", 42500000},
		{"42.5 MiB", 44564480},
		// No need to say B
		{"42M", 42000000},
		{"42Mi", 44040192},
		{"42m", 42000000},
		{"42mi", 44040192},
		{"42MI", 44040192},
		{"42 M", 42000000},
		{"42 Mi", 44040192},
		{"42 m", 42000000},
		{"42 mi", 44040192},
		{"42 MI", 44040192},
		{"42.5M", 42500000},
		{"42.5Mi", 44564480},
		{"42.5 M", 42500000},
		{"42.5 Mi", 44564480},
		{"1,005.03 MB", 1005030000},
		{"1,005.03 Mbps", 1005030000},
		// Large testing, breaks when too much larger than this.
		{"12.5 EB", uint64(12.5 * float64(EByte))},
		{"12.5 E", uint64(12.5 * float64(EByte))},
		{"12.5 Ebps", uint64(12.5 * float64(EByte))},
		{"12.5 EiB", uint64(12.5 * float64(EiByte))},
	}

	for _, p := range tests {
		got, err := ParseHumanBytes(p.in)
		if err != nil {
			t.Errorf("Couldn't parse %v: %v", p.in, err)
		}
		if got != p.exp {
			t.Errorf("Expected %v for %v, got %v",
				p.exp, p.in, got)
		}
	}
}

// TestMustParseHumanBytes 验证两位小数解析成功时不使用默认值, 错误时才回退.
func TestMustParseHumanBytes(t *testing.T) {
	assert.Equal(t, uint64(44564480), MustParseHumanBytes("42.5Mi"))
	assert.Equal(t, uint64(123), MustParseHumanBytes("-42.5Mi", 123))
	assert.Equal(t, uint64(0), MustParseHumanBytes("x"))
	assert.Equal(t, uint64(0), MustParseHumanBytes(""))
	assert.Equal(t, uint64(math.MaxUint64), MustParseHumanBytes("18446744073709551615", 123))
	assert.Equal(t, uint64(123), MustParseHumanBytes("18446744073709551616", 123))
	assert.Equal(t, uint64(2232008604385), MustParseHumanBytes("2.03 TiB", 123))
	assert.Equal(t, uint64(759264318586), MustParseHumanBytes("707.12 GiB", 123))
	assert.Equal(t, uint64(0), MustParseHumanBytes("0.00 GiB", 123))
	assert.Equal(t, uint64(123), MustParseHumanBytes("16.00 EiB", 123))
	assert.Equal(t, uint64(123), MustParseHumanBytes("2.03 invalid", 123))
}

// TestParseHumanBytesErrors 验证格式错误和溢出仍被拒绝, 包括舍入后超界的显示值.
func TestParseHumanBytesErrors(t *testing.T) {
	// 保留既有非法输入策略, 并覆盖精确整数乘法的溢出边界.
	for _, input := range []string{
		"84 JB", "", "16 EiB", "18446744073709551616", "184467440737095516150 EB",
		"18446744073709552 KB", "18014398509481984 KiB", "18446744073710 Mbps",
		"18.5 EB", "18446744073709551615.0 B", " 42 MB", "-42 MB", "42 RB", "42 QB",
		"16.00 EiB", "18.45 EB", "2.03 invalid", "2.0.3 TiB",
	} {
		if got, err := ParseHumanBytes(input); err == nil {
			t.Errorf("Expected error for %q, got %v", input, got)
		}
	}
}

// TestHumanBytes 验证容量和速率入口共享固定两位规则, 原始单位仍输出整数.
func TestHumanBytes(t *testing.T) {
	for _, v := range []struct {
		title    string
		actual   string
		expected string
	}{
		{"HumanBytes(0)", HumanBytes(0), "0 B"},
		{"HumanBytes(1)", HumanBytes(1), "1 B"},
		{"HumanBytes(803)", HumanBytes(803), "803 B"},
		{"HumanBytes(999)", HumanBytes(999), "999 B"},

		{"HumanBytes(1023)", HumanBytes(1023), "1.02 KB"},
		{"HumanBytes(1024)", HumanBytes(1024), "1.02 KB"},
		{"HumanBytes(9999)", HumanBytes(9999), "10.00 KB"},
		{"HumanBytes(1MB - 1)", HumanBytes(MByte - Byte), "1000.00 KB"},

		{"HumanBytes(1MB)", HumanBytes(1024 * 1024), "1.05 MB"},
		{"HumanBytes(1GB - 1K)", HumanBytes(GByte - KByte), "1000.00 MB"},

		{"HumanBytes(5.5GB)", HumanBytes(5.5 * GByte), "5.50 GB"},
		{"HumanBytes(1GB)", HumanBytes(GByte), "1.00 GB"},
		{"HumanBytes(1TB - 1M)", HumanBytes(TByte - MByte), "1000.00 GB"},
		{"HumanBytes(10MB)", HumanBytes(9999 * 1000), "10.00 MB"},

		{"HumanBytes(1TB)", HumanBytes(TByte), "1.00 TB"},
		{"HumanBytes(1PB - 1T)", HumanBytes(PByte - TByte), "999.00 TB"},

		{"HumanBytes(1PB)", HumanBytes(PByte), "1.00 PB"},
		{"HumanBytes(1PB - 1T)", HumanBytes(EByte - PByte), "999.00 PB"},

		{"HumanBytes(1EB)", HumanBytes(EByte), "1.00 EB"},
		{"HumanBytes(MaxUint64)", HumanBytes(math.MaxUint64), "18.45 EB"},

		{"HumanIBytes(0)", HumanIBytes(0), "0 B"},
		{"HumanIBytes(1)", HumanIBytes(1), "1 B"},
		{"HumanIBytes(803)", HumanIBytes(803), "803 B"},
		{"HumanIBytes(1023)", HumanIBytes(1023), "1023 B"},

		{"HumanIBytes(1024)", HumanIBytes(1024), "1.00 KiB"},
		{"HumanIBytes(1MB - 1)", HumanIBytes(MiByte - IByte), "1024.00 KiB"},

		{"HumanIBytes(1MB)", HumanIBytes(1024 * 1024), "1.00 MiB"},
		{"HumanIBytes(1GB - 1K)", HumanIBytes(GiByte - KiByte), "1024.00 MiB"},

		{"HumanIBytes(5.5GiB)", HumanIBytes(5.5 * GiByte), "5.50 GiB"},
		{"HumanIBytes(1GB)", HumanIBytes(GiByte), "1.00 GiB"},
		{"HumanIBytes(1TB - 1M)", HumanIBytes(TiByte - MiByte), "1024.00 GiB"},

		{"HumanIBytes(1TB)", HumanIBytes(TiByte), "1.00 TiB"},
		{"HumanIBytes(1PB - 1T)", HumanIBytes(PiByte - TiByte), "1023.00 TiB"},

		{"HumanIBytes(1PB)", HumanIBytes(PiByte), "1.00 PiB"},
		{"HumanIBytes(1PB - 1T)", HumanIBytes(EiByte - PiByte), "1023.00 PiB"},

		{"HumanIBytes(1EiB)", HumanIBytes(EiByte), "1.00 EiB"},
		{"HumanIBytes(MaxUint64)", HumanIBytes(math.MaxUint64), "16.00 EiB"},

		{"HumanIntBytes(5.5GB)", HumanIntBytes(5.5 * GByte), "5.50 GB"},
		{"HumanIntIBytes(5.5GiB)", HumanIntIBytes(5.5 * GiByte), "5.50 GiB"},

		{"HumanKbps(0)", HumanKbps(0), "0 bps"},
		{"HumanKbps(1023)", HumanKbps(1023), "1.02 Kbps"},
		{"HumanKbps(1024)", HumanKbps(1024), "1.02 Kbps"},
		{"HumanKbps(31450000)", HumanKbps(31450000), "31.45 Mbps"},
		{"HumanIntKbps(5.5GB)", HumanIntKbps(5.5 * GByte), "5.50 Gbps"},
	} {
		assert.Equal(t, v.expected, v.actual, v.title)
	}
}

// TestHumanIBytesDisplayParsing 验证两位显示与解析衔接, 不把有损显示当作原始字节数.
func TestHumanIBytesDisplayParsing(t *testing.T) {
	for _, test := range []struct {
		input, parsed uint64
		text          string
	}{
		{2232008604385, 2232008604385, "2.03 TiB"},
		{759264318586, 759264318586, "707.12 GiB"},
		// 原始值的微小差异会被展示舍入, 解析结果应取展示文本代表的整数值.
		{2232008604385 + 123456789, 2232008604385, "2.03 TiB"},
		{759264318586 + 1024, 759264318586, "707.12 GiB"},
	} {
		formatted := HumanIBytes(test.input)
		if formatted != test.text {
			t.Errorf("HumanIBytes(%d) = %q, want %q", test.input, formatted, test.text)
		}
		parsed, err := ParseHumanBytes(formatted)
		if err != nil || parsed != test.parsed {
			t.Fatalf("ParseHumanBytes(%q) = (%d, %v), want (%d, nil)", formatted, parsed, err, test.parsed)
		}
		if got := HumanIBytes(parsed); got != test.text {
			t.Errorf("HumanIBytes(%d) = %q, want stable display %q", parsed, got, test.text)
		}
	}
}

// TestHumanBaseBytesUnscaled 验证未缩放值不经浮点转换, 包括单单位表的大整数.
func TestHumanBaseBytesUnscaled(t *testing.T) {
	if got := HumanBaseBytes(math.MaxUint64, 1000, []string{"B"}); got != "18446744073709551615 B" {
		t.Errorf("Unscaled MaxUint64 = %q", got)
	}
	if got := HumanBaseBytes(99, 2, []string{"B", "K"}); got != "49.50 K" {
		t.Errorf("Custom integer unit = %q, want 49.50 K", got)
	}
	// 大整数只缩放一级时, 不得因整体乘 100 溢出或转换 float64 而丢失个位.
	if got := HumanBaseBytes(math.MaxUint64, 2, []string{"B", "K"}); got != "9223372036854775807.50 K" {
		t.Errorf("Custom capped MaxUint64 = %q", got)
	}
}

func BenchmarkParseBytes(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _ = ParseHumanBytes("16.5 GB")
	}
}

func BenchmarkBytes(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = HumanBytes(16.5 * GByte)
	}
}
