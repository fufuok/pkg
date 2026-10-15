package common

import (
	"fmt"

	"github.com/fufuok/pkg/myip"

	"github.com/fufuok/pkg/config"
)

var (
	// InternalIPv4 服务器 IP
	InternalIPv4 string
	ExternalIPv4 string

	// internalIPv4Lookup 和 externalIPv4Lookup 保留生产默认查询实现, 仅供包内测试隔离网络边界.
	internalIPv4Lookup = myip.InternalIPv4
	externalIPv4Lookup = myip.ExternalIPv4
)

type M struct{}

// Start 先应用启动时区, 再初始化日志和后台任务; 手工加载配置的安全前提见 InitTimeLocation.
func (m *M) Start() error {
	if err := InitTimeLocation(); err != nil {
		return fmt.Errorf("unable to initialize application time location: %w", err)
	}

	// 初始化日志环境
	initLogger()

	// 初始化本机 IP
	go initServerIP()

	// 池相关设置
	initPool()

	// 初始化 HTTP 客户端请求配置
	initReq()

	// 初始化定时任务
	initLogSender()

	return nil
}

// Runtime 更新日志和请求配置; 日志初始化失败时保留根因并停止本模块的后续更新.
func (m *M) Runtime() error {
	if err := loadLogger(); err != nil {
		return fmt.Errorf("unable to reinitialize logger: %w", err)
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

// initServerIP 查询内外网地址, 外网查询失败时回退到节点配置.
//
// 查询函数默认始终指向 myip 的生产实现. 包内测试只能在启动前替换它们, 不支持运行期并发改写.
//
//go:norace
func initServerIP() {
	InternalIPv4 = internalIPv4Lookup()
	ExternalIPv4 = externalIPv4Lookup()
	if ExternalIPv4 == "" {
		ExternalIPv4 = config.Config().NodeConf.NodeInfo.NodeIP
	}
}
