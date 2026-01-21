package kit

import (
	"math"

	"github.com/fufuok/utils"
	"github.com/fufuok/utils/generic"
)

// CalcThreshold 计算阈值, 始终向上取整且最小为: 1
func CalcThreshold[T generic.Integer](limit T, ratio float64) T {
	if limit <= 0 {
		return 1
	}
	v := T(math.Ceil(float64(limit) * ratio))
	if v <= 0 {
		return 1
	}
	return v
}

// CalcRatio 计算比率 a/b, 支持所有数值类型
// 除数为 0 或被除数为 0 时返回 0
func CalcRatio[T generic.Numeric](a, b T, precision int) float64 {
	if a == 0 || b == 0 {
		return 0
	}

	// 计算比率
	ratio := float64(a) / float64(b)

	return utils.Round(ratio, precision)
}
