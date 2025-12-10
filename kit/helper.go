package kit

import (
	"math"

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
