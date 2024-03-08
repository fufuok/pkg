package common

import (
	"testing"
	"time"

	"github.com/fufuok/utils"
	"github.com/fufuok/utils/assert"
)

func TestGenSign(t *testing.T) {
	key := utils.RandString(18)
	ts := time.Now().Unix()
	sign := GenSign(ts, key)
	t.Log("sign:", sign)
	assert.True(t, VerifySignTTL(key, sign, 1))
}
