package master

import (
	"os"
	"strings"

	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/logger"
)

const binaryChangeActionEnv = "PKG_BINARY_CHANGE_ACTION"

type binaryChangeAction string

const (
	binaryChangeRestart binaryChangeAction = "restart"
	binaryChangeIgnore  binaryChangeAction = "ignore"
	binaryChangeSigterm binaryChangeAction = "sigterm"
)

var (
	// binaryChangeAction 在启动时读取一次, watcher运行期间不再受环境热加载影响.
	binaryChangeActionValue = binaryChangeRestart
	// binaryChangeSignal 供Linux实现和测试替身注入SIGTERM发送行为.
	binaryChangeSignal = sendBinaryChangeSIGTERM
)

// parseBinaryChangeAction 解析主程序二进制变化策略.
// 空值使用兼容历史行为的restart; 未知值也回退restart, 避免新二进制安装后长期运行旧进程.
func parseBinaryChangeAction(raw string) (binaryChangeAction, bool) {
	switch value := strings.ToLower(strings.TrimSpace(raw)); value {
	case "":
		return binaryChangeRestart, true
	case string(binaryChangeRestart):
		return binaryChangeRestart, true
	case string(binaryChangeIgnore):
		return binaryChangeIgnore, true
	case string(binaryChangeSigterm):
		return binaryChangeSigterm, true
	default:
		return binaryChangeRestart, false
	}
}

// loadBinaryChangeAction 在配置加载完成后固定本进程的二进制变化策略.
// 该策略是部署级环境变量, 不随运行期env文件重载改变, 防止远端配置意外改变退出行为.
func loadBinaryChangeAction() {
	raw := os.Getenv(binaryChangeActionEnv)
	action, valid := parseBinaryChangeAction(raw)
	binaryChangeActionValue = action
	if !valid {
		logger.Warn().Str("value", strings.TrimSpace(raw)).Str("fallback", string(binaryChangeRestart)).
			Msg("Invalid binary change action, fallback to restart")
	}
}

// handleBinaryChange 执行已固定的二进制变化策略.
// 返回true表示本轮watcher不应继续执行配置和业务Runtime; ignore例外, 它只记录并继续当前轮次.
func handleBinaryChange(action binaryChangeAction) (needContinue bool) {
	switch action {
	case binaryChangeIgnore:
		logger.Warn().Str("deb_version", config.DebVersion).
			Msg("Binary changed, restart ignored by policy")
		return false
	case binaryChangeSigterm:
		logger.Warn().Str("deb_version", config.DebVersion).
			Msg("Binary changed, sending SIGTERM")
		if err := binaryChangeSignal(); err != nil {
			logger.Error().Err(err).Msg("Failed to send SIGTERM, fallback to restart")
			restartChan <- true
		}
		return true
	default:
		logger.Warn().Str("deb_version", config.DebVersion).Msg(">>>>>>> Restart main <<<<<<<")
		restartChan <- true
		return true
	}
}
