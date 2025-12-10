package kit

import (
	"testing"
)

func TestCalcThreshold(t *testing.T) {
	// 测试 int 类型
	testsInt := []struct {
		name     string
		limit    int
		ratio    float64
		expected int
	}{
		{"zero limit", 0, 0.5, 1},
		{"negative limit", -5, 0.5, 1},
		{"normal case", 10, 0.5, 5},
		{"rounding up", 10, 0.33, 4}, // ceil(3.3) = 4
		{"ratio > 1", 10, 1.5, 15},
		{"small ratio", 100, 0.01, 1}, // ceil(1.0) = 1
		{"very small ratio", 100, 0.001, 1}, // ceil(0.1) = 1
	}

	for _, tt := range testsInt {
		t.Run(tt.name, func(t *testing.T) {
			result := CalcThreshold(tt.limit, tt.ratio)
			if result != tt.expected {
				t.Errorf("CalcThreshold(%d, %f) = %d; expected %d", tt.limit, tt.ratio, result, tt.expected)
			}
		})
	}

	// 测试 uint64 类型
	testsUint64 := []struct {
		name     string
		limit    uint64
		ratio    float64
		expected uint64
	}{
		{"zero limit", 0, 0.5, 1},
		{"normal case", 10, 0.5, 5},
		{"rounding up", 10, 0.33, 4}, // ceil(3.3) = 4
		{"ratio > 1", 10, 1.5, 15},
		{"small ratio", 100, 0.01, 1}, // ceil(1.0) = 1
		{"very small ratio", 100, 0.001, 1}, // ceil(0.1) = 1
	}

	for _, tt := range testsUint64 {
		t.Run(tt.name, func(t *testing.T) {
			result := CalcThreshold(tt.limit, tt.ratio)
			if result != tt.expected {
				t.Errorf("CalcThreshold(%d, %f) = %d; expected %d", tt.limit, tt.ratio, result, tt.expected)
			}
		})
	}

	// 测试 int64 类型
	testsInt64 := []struct {
		name     string
		limit    int64
		ratio    float64
		expected int64
	}{
		{"zero limit", 0, 0.5, 1},
		{"negative limit", -5, 0.5, 1},
		{"normal case", 10, 0.5, 5},
		{"rounding up", 10, 0.33, 4}, // ceil(3.3) = 4
		{"ratio > 1", 10, 1.5, 15},
		{"small ratio", 100, 0.01, 1}, // ceil(1.0) = 1
		{"very small ratio", 100, 0.001, 1}, // ceil(0.1) = 1
	}

	for _, tt := range testsInt64 {
		t.Run(tt.name, func(t *testing.T) {
			result := CalcThreshold(tt.limit, tt.ratio)
			if result != tt.expected {
				t.Errorf("CalcThreshold(%d, %f) = %d; expected %d", tt.limit, tt.ratio, result, tt.expected)
			}
		})
	}
}

func TestCalcThreshold_MinimumValue(t *testing.T) {
	// 测试确保结果始终至少为 1
	testCases := []struct {
		name  string
		limit interface{}
		ratio float64
	}{
		{"int small value", 1, 0.1},
		{"int64 small value", int64(1), 0.1},
		{"uint64 small value", uint64(1), 0.1},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			switch v := tc.limit.(type) {
			case int:
				result := CalcThreshold(v, tc.ratio)
				if result < 1 {
					t.Errorf("CalcThreshold(%d, %f) = %d; expected >= 1", v, tc.ratio, result)
				}
			case int64:
				result := CalcThreshold(v, tc.ratio)
				if result < 1 {
					t.Errorf("CalcThreshold(%d, %f) = %d; expected >= 1", v, tc.ratio, result)
				}
			case uint64:
				result := CalcThreshold(v, tc.ratio)
				if result < 1 {
					t.Errorf("CalcThreshold(%d, %f) = %d; expected >= 1", v, tc.ratio, result)
				}
			}
		})
	}
}