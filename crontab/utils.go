package crontab

import (
	"github.com/fufuok/cron"
)

// DefaultParser 等于 WithSecondOptional()
var DefaultParser = cron.NewParser(
	cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
)

// IsValidSpec 检查定时任务标识是否有效
func IsValidSpec(spec string, parser ...cron.Parser) bool {
	var err error
	if len(parser) > 0 {
		_, err = parser[0].Parse(spec)
	} else {
		_, err = DefaultParser.Parse(spec)
	}
	return err == nil
}
