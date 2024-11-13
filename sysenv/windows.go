//go:build !linux
// +build !linux

package sysenv

const (
	BinApt       = "unknown"
	BinDpkg      = "unknown"
	BinNSLookup  = "unknown"
	BinSystemctl = "unknown"
)
