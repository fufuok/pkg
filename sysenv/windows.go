//go:build !unix

package sysenv

const (
	BinApt       = "unknown"
	BinDpkg      = "unknown"
	binNSLookup  = "unknown"
	BinSystemctl = "unknown"
)
