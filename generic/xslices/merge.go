package xslices

// Merge 浅拷贝合并多个切片, 不影响原切片.
// 没有后续切片时返回原切片, 保留 nil 与空切片的区别; 首切片为空仍合并后续输入.
func Merge[E any](s []E, ss ...[]E) []E {
	if len(ss) == 0 {
		return s
	}
	n := len(s)
	for _, v := range ss {
		n += len(v)
	}
	d := make([]E, 0, n)
	d = append(d, s...)
	for _, v := range ss {
		d = append(d, v...)
	}
	return d
}
