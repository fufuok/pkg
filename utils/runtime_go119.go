package utils

import (
	_ "unsafe"
)

//go:linkname FastRand64 runtime.fastrand64
func FastRand64() uint64

//go:linkname FastRandu runtime.fastrandu
func FastRandu() uint
