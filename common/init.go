package common

import (
	"fmt"

	"github.com/fufuok/utils/myip"
)

var (
	// InternalIPv4 服务器 IP
	InternalIPv4 string
	ExternalIPv4 string
)

type M struct{}

// Start 程序启动时初始化
func (m *M) Start() error {
	initNow(StartTime)

	// 初始化日志环境
	initLogger()

	// 初始化本机 IP
	go initServerIP()

	// 同步全局秒级时间值
	go syncNow()

	// 池相关设置
	initPool()

	// 初始化 HTTP 客户端请求配置
	initReq()

	// 初始化定时任务
	initLogSender()

	return nil
}

// Runtime 重新加载配置时运行
func (m *M) Runtime() error {
	if err := loadLogger(); err != nil {
		return fmt.Errorf("unable to reinitialize logger")
	}
	loadReq()
	return nil
}

// Stop 程序退出时运行
func (m *M) Stop() error {
	close(LogChan.In)
	poolRelease()
	return nil
}

//go:norace
func initServerIP() {
	InternalIPv4 = myip.InternalIPv4()
	ExternalIPv4 = myip.ExternalIPAny(10)
}
