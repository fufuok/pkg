package crontab

import (
	"github.com/fufuok/cron"
)

// DefaultParser 默认解析器，支持可选的秒字段
// 支持的表达式格式:
// - 标准格式(5字段): "分钟 小时 日 月 星期"
// - 扩展格式(6字段): "秒 分钟 小时 日 月 星期"
//
// 字段说明:
// 秒 (可选): 0-59
// 分钟: 0-59
// 小时: 0-23
// 日: 1-31
// 月: 1-12 (或 JAN-DEC)
// 星期: 0-6 (0或7表示周日，或 SUN-SAT)
//
// 特殊字符:
// *: 匹配任意值
var DefaultParser = cron.NewParser(
	cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
)

// IsValidSpec 检查定时任务表达式是否有效
//
// 示例:
// "* * * * *"        // 每分钟执行
// "* * * * * *"      // 每秒执行
// "0 */5 * * * *"    // 每5分钟执行
// "0 0 3 * * *"      // 每天凌晨3点执行
// "0 0 3 1 * *"      // 每月1号凌晨3点执行
// "0 0 3 * * 1"      // 每周一凌晨3点执行
//
// 参数:
//
//	spec: 定时任务表达式
//	parser: 可选的解析器，如果不提供则使用默认解析器
//
// 返回值:
//
//	bool: 表达式是否有效
//
// ?: 用于日和星期字段，表示不指定值
// -: 范围，如 1-5
// /: 步长，如 */5 表示每5个单位
// ,: 列表，如 1,3,5 表示1、3、5
func IsValidSpec(spec string, parser ...cron.Parser) bool {
	var err error
	if len(parser) > 0 {
		_, err = parser[0].Parse(spec)
	} else {
		_, err = DefaultParser.Parse(spec)
	}
	return err == nil
}
