package common

import (
	"github.com/fufuok/ants"

	"github.com/fufuok/pkg/config"
)

var commonTestState *commonTesterState

// commonTesterState 记录 common 测试助手拥有的资源和调用前状态.
// 助手只支持串行 Init-Stop 生命周期, 不支持嵌套或并发调用.
type commonTesterState struct {
	configOwned  bool
	internalIP   string
	externalIP   string
	previousPool ants.Pooler
}

// InitTester 建立 common 的最小离线测试环境.
//
// 助手确保配置可用, 发布确定性 IP 并安装可提交任务的默认 pool. 它不会启动公网 IP
// 探测、文件 logger、HTTP 客户端或永久日志 sender, 避免测试顺序和后台资源泄漏.
func InitTester() {
	if commonTestState != nil {
		panic("common test helper is already initialized")
	}

	state := &commonTesterState{
		internalIP: InternalIPv4,
		externalIP: ExternalIPv4,
	}
	if config.Config() == nil {
		config.InitTester()
		state.configOwned = true
	}

	pool, err := newDefaultPool()
	if err != nil {
		if state.configOwned {
			config.StopTester()
		}
		panic("failed to create common test pool: " + err.Error())
	}
	state.previousPool = ants.SwapDefaultAntsPool(pool)
	if state.previousPool == nil {
		pool.Release()
		if state.configOwned {
			config.StopTester()
		}
		panic("failed to preserve previous default pool")
	}

	InternalIPv4 = "127.0.0.1"
	ExternalIPv4 = "127.0.0.1"
	commonTestState = state
}

// StopTester 恢复调用前的默认 pool、IP 和助手拥有的配置状态.
//
// pool 必须先恢复再释放换出的助手池, 否则会把已关闭资源重新发布为默认池.
func StopTester() {
	state := commonTestState
	if state == nil {
		return
	}
	commonTestState = nil

	helperPool := ants.SwapDefaultAntsPool(state.previousPool)
	if helperPool != nil && helperPool != state.previousPool {
		helperPool.Release()
	}
	InternalIPv4 = state.internalIP
	ExternalIPv4 = state.externalIP
	if state.configOwned {
		config.StopTester()
	}
}
