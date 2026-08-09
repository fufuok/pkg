package master

import (
	"testing"
	"time"

	"github.com/fufuok/pkg/assert"
)

func TestWaitUntilNtpdate(t *testing.T) {
	preserveMasterPackageState(t)

	assert.False(t, WaitUntilNtpdate(10*time.Millisecond))
	close(ntpFirstDoneChan)
	assert.True(t, WaitUntilNtpdate(10*time.Millisecond))
	assert.True(t, WaitUntilNtpdate(50*time.Millisecond))
}
