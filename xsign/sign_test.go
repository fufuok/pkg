package xsign

import (
	"testing"
	"time"

	"github.com/fufuok/pkg/assert"
	"github.com/fufuok/pkg/utils"
)

func TestGenSign(t *testing.T) {
	key := utils.RandString(18)
	ts := time.Now().Unix()
	sign := GenSign(ts, key)
	t.Log("sign:", sign)
	assert.True(t, VerifySignTTL(key, sign, 1))
}

func TestGenSignRejectsInvalidInput(t *testing.T) {
	if got := GenSignString("123", "key"); got != "" {
		t.Fatalf("short timestamp signed: %q", got)
	}
	if got := GenSignString("1234567890", ""); got != "" {
		t.Fatalf("empty key signed: %q", got)
	}
	if VerifySign("", GenSign(time.Now().Unix(), "key")) {
		t.Fatal("empty key verified")
	}
	if VerifySign("key", "short") {
		t.Fatal("short sign verified")
	}
}

func TestVerifySignTTLAtWindow(t *testing.T) {
	key := "ttl-key"
	now := int64(1700000000)
	sign := GenSign(now, key)
	if !VerifySignTTLAt(key, sign, 1, now) {
		t.Fatal("current timestamp rejected")
	}
	if !VerifySignTTLAt(key, sign, 1, now+1) {
		t.Fatal("timestamp within +1s rejected")
	}
	if VerifySignTTLAt(key, sign, 1, now+2) {
		t.Fatal("timestamp outside window accepted")
	}
}
