package common

import (
	"fmt"
	"time"

	"github.com/fufuok/pkg/myip"

	"github.com/fufuok/pkg/config"
)

var (
	// stopDrainTimeout 是 Stop 等待日志 sender 排空并提交末批的时长上限, 避免 sender 异常时拖住停机.
	// sender 只做内存拼批和非阻塞提交, 正常毫秒级完成; 取 2 秒以适配常见 5 秒的 systemd 停止期限.
	// 包内测试可缩短, 不支持并发改写.
	stopDrainTimeout = 2 * time.Second

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

// Stop 程序退出时运行: 关闭日志队列后先等待 sender 排空并把末批提交给协程池, 再释放协程池.
// 默认池 (MultiPool) 的 Release 会等待在途任务, 末批 HTTP 发送因此在 Stop 返回前完成.
// sender 等待以 stopDrainTimeout 为上限, 超时只记录告警并继续停机, 不返回错误
// (返回错误会被 master 视为致命并直接退出进程).
// 边界: 只有 EOF 末批限时 logTailPostTimeout; 此前已提交的满批/定时批次和 PostLog 按客户端超时与重试
// 由 Release 等待 (与 v1.4.2 相同), 日志接口挂起不响应时本函数可能超过部署的停止期限.
// 本模块在 stopPipeline 中倒数第二个停止, 业务 Pipeline 此时已完成清理.
func (m *M) Stop() error {
	close(LogChan.In)
	if done := logSenderDone; done != nil {
		// 必须先于释放协程池: 否则 sender 的末批提交会被已关闭的池拒绝而丢弃.
		timer := time.NewTimer(stopDrainTimeout)
		select {
		case <-done:
		case <-timer.C:
			Log().Warn().Dur("timeout", stopDrainTimeout).Msg("Log sender drain timed out")
		}
		timer.Stop()
	}
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
