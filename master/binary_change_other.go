//go:build !linux

package master

import "errors"

// sendBinaryChangeSIGTERM 在非Linux上报告不支持, 由调用方回退到已有restart路径.
func sendBinaryChangeSIGTERM() error {
	return errors.New("SIGTERM is unavailable on this platform")
}
