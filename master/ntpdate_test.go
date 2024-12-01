package master

import (
	"testing"
	"time"

	"github.com/fufuok/utils/assert"
)

func TestWaitUntilNtpdate(t *testing.T) {
	go func() {
		time.Sleep(100 * time.Millisecond)
		close(ntpFirstDoneChan)
	}()
	assert.False(t, WaitUntilNtpdate(50*time.Millisecond))
	assert.True(t, WaitUntilNtpdate(120*time.Millisecond))
	assert.True(t, WaitUntilNtpdate(120*time.Millisecond))
	time.Sleep(100 * time.Millisecond)
	assert.True(t, WaitUntilNtpdate(50*time.Millisecond))
	assert.True(t, WaitUntilNtpdate(500*time.Millisecond))
}
