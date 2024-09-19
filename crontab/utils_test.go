package crontab

import (
	"testing"

	"github.com/fufuok/cron"
	"github.com/fufuok/utils/assert"
)

func TestIsValidSpec(t *testing.T) {
	cases := []struct {
		in string
		ok bool
	}{
		{"* * * * *", true},
		{"* * * * * *", true},
		{"1 2 3 4 5 6", true},
		{"0 */3 * * * *", true},
		{"*/3 */3 2 * * *", true},
		{"* * 1,15 * *", true},
		{"* * */10 * Sun", true},
		{"30 08 ? Jul Sun", true},
		{"TZ=America/New_York 0 30 2 11 Mar ?", true},
		{"CRON_TZ=America/New_York 0 0 * * * ?", true},

		{"* * * *", false},
		{"* * * * * * *", false},
		{"61 * * * * *", false},
		{"6 * 25 * * *", false},
		{"6 * * 32 * *", false},
		{"6 * * 3 13 *", false},
	}
	for _, c := range cases {
		assert.Equal(t, c.ok, IsValidSpec(c.in), c.in)
	}

	standardParser := cron.NewParser(
		cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
	)
	assert.Equal(t, true, IsValidSpec("1 2 3 4 5", standardParser))
	assert.Equal(t, false, IsValidSpec("1 2 3 4 5 6", standardParser))
	assert.Equal(t, true, IsValidSpec("1 2 3 4 5"))
	assert.Equal(t, true, IsValidSpec("1 2 3 4 5 6"))
}
