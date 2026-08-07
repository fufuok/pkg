package master

import (
	"testing"
	"time"

	"github.com/fufuok/pkg/assert"
)

func TestWaitUntilNtpdate(t *testing.T) {
	// 每轮测试使用独立 channel, 避免关闭包级 channel 后污染重复执行.
	oldChan, oldCancel, oldName := ntpFirstDoneChan, ntpCancel, ntpName
	ntpFirstDoneChan = make(chan struct{})
	ntpCancel = nil
	ntpName = ""
	t.Cleanup(func() {
		ntpFirstDoneChan = oldChan
		ntpCancel = oldCancel
		ntpName = oldName
	})

	assert.False(t, WaitUntilNtpdate(10*time.Millisecond))
	close(ntpFirstDoneChan)
	assert.True(t, WaitUntilNtpdate(10*time.Millisecond))
	assert.True(t, WaitUntilNtpdate(50*time.Millisecond))
}
