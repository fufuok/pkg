package config

import (
	"testing"
	"time"

	"github.com/fufuok/utils/assert"
)

func TestZeroTimeCST(t *testing.T) {
	assert.False(t, ZeroTimeCST.IsZero())
	assert.True(t, ZeroTimeUTC.IsZero())
	assert.Equal(t, time.UTC, ZeroTimeUTC.Location())
	assert.Equal(t, "0001-01-01T00:00:00+08:00", ZeroTimeCST.Format(time.RFC3339))
	assert.Equal(t, "0001-01-01T00:00:00Z", ZeroTimeUTC.Format(time.RFC3339))

	now := time.Now()
	t.Log(now.In(DefaultTimeLocation).Format(time.RFC3339))
	t.Log(now.In(CSTTimeLocation).Format(time.RFC3339))
}
