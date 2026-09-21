package master

import (
	"net"
	"strings"
	"sync"
	"time"

	"github.com/fufuok/pkg/common"
	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/logger"
	"github.com/fufuok/pkg/sysenv"
	"github.com/fufuok/pkg/utils"
	"github.com/fufuok/pkg/xhash"
)

const (
	debRetryDelay       = time.Minute
	debUpdateRetryDelay = 10 * time.Second
)

var debInstall *debInstaller

// debTarget 只复制发布字段, 不保存可变配置指针或历史任务.
type debTarget struct {
	version    string
	threshold  uint64
	randomWait int
}

// debInstaller 的锁只保护目标和生命周期, 命令与等待始终由唯一worker串行执行.
// read读取已加载配置, run与eligible保留为私有测试接缝, 不构成扩展框架.
type debInstaller struct {
	mu                       sync.Mutex
	target                   debTarget
	round, finished          uint64
	started, stopped, paused bool
	wake                     chan struct{}
	done                     chan struct{}
	name                     string
	read                     func() debTarget
	run                      func(time.Duration, ...string) debCommandResult
	eligible                 func(string, uint64) bool
	ready                    func() error
}

// debAttempt 仅保留同轮唯一补试所需的信息, 新目标不会继承旧失败的修复授权.
type debAttempt struct {
	err          error
	retryInstall bool
	configure    bool
}

// newDebInstaller 创建未启动的安装器; 只有配置事件才启动worker, 初始化不安装.
func newDebInstaller(name string, read func() debTarget) *debInstaller {
	return &debInstaller{
		name: name, read: read, wake: make(chan struct{}, 1), done: make(chan struct{}),
		run: runDebCommand, eligible: canary, ready: debToolsReady,
	}
}

// prepareDebInstaller 在业务启动前冻结包名和公开版本快照; 不创建后台任务.
func prepareDebInstaller() {
	config.DebVersion = DebVersion(config.DebName)
	debInstall = newDebInstaller(config.DebName, func() debTarget {
		cfg := config.Config()
		return debTarget{cfg.SYSConf.DebVersion, cfg.SYSConf.CanaryDeployment, cfg.MainConf.RandomWait}
	})
}

// publish 接受一次真实配置事件; 同目标仍在途时不重置次数, 已结束时可开启新轮次.
func (u *debInstaller) publish() {
	if u == nil || u.name == "" {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.stopped {
		return
	}
	resume := u.paused
	u.paused = false
	u.refreshLocked(resume || u.round == u.finished)
	if !u.started {
		u.started = true
		go u.loop()
	}
	u.notifyLocked()
}

// refreshLocked 在同一小锁内读取最新快照, 防止较早读取的配置覆盖刚发布的新目标.
// force只用于真实配置事件, timer或重复wake不会恢复已结束轮次.
func (u *debInstaller) refreshLocked(force bool) {
	next := u.read()
	if force || next.version != u.target.version || next.threshold != u.target.threshold {
		u.target = next
		u.round++
		u.notifyLocked()
	}
}

// notifyLocked 合并通知, 调用方持有mu; 通知本身不是安装事件.
func (u *debInstaller) notifyLocked() {
	select {
	case u.wake <- struct{}{}:
	default:
	}
}

// pause 在配置加载失败时撤销未启动动作, 后续成功发布才能解除.
func (u *debInstaller) pause() {
	if u == nil {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.paused = true
	u.round++
	u.notifyLocked()
}

// stop 终止本地调度但不取消正在执行的包管理命令, 也不等待服务停止.
func (u *debInstaller) stop() {
	if u == nil {
		return
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.stopped {
		return
	}
	u.stopped = true
	if !u.started {
		close(u.done)
	}
	u.notifyLocked()
}

// current 是每条命令前的授权边界, 主动观察其他既有入口已成功加载的新配置.
func (u *debInstaller) current(round uint64) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.stopped || u.paused {
		return false
	}
	u.refreshLocked(false)
	return u.round == round
}

// wait 只持有一个临时timer, 同值通知不改变截止时间; 新目标或Stop立即结束等待.
func (u *debInstaller) wait(round uint64, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	for u.current(round) {
		select {
		case <-timer.C:
			return u.current(round)
		case <-u.wake:
		}
	}
	return false
}

// loop 独占尝试次数; 结束后无ticker, 只等待下一次配置事件.
func (u *debInstaller) loop() {
	defer close(u.done)
	for {
		u.mu.Lock()
		target, round := u.target, u.round
		stopped, idle := u.stopped, u.paused || u.round == u.finished
		u.mu.Unlock()
		if stopped {
			return
		}
		if idle {
			<-u.wake
			continue
		}
		u.installRound(target, round)
		u.mu.Lock()
		u.finished = round
		u.mu.Unlock()
	}
}

// installRound 每轮至多两次, 空目标或未命中灰度不会创建首次随机等待.
func (u *debInstaller) installRound(target debTarget, round uint64) {
	if target.version == "" || !u.eligible(target.version, target.threshold) {
		return
	}
	if err := u.ready(); err != nil {
		logger.Warn().Err(err).Msg("Debian installer unavailable")
		return
	}
	delay := time.Duration(0)
	if target.randomWait > 0 {
		delay = time.Duration(utils.FastIntn(target.randomWait)) * time.Second
	}
	if !u.wait(round, delay) {
		return
	}
	result := debAttempt{}
	for attempt := 1; attempt <= 2; attempt++ {
		result = u.attempt(target, round, result)
		if !u.current(round) || result.err == nil {
			return
		}
		logger.Warn().Err(result.err).Str("package", u.name).Str("version", target.version).
			Int("attempt", attempt).Bool("final", attempt == 2).Msg("Debian installation attempt failed")
		if attempt == 2 || !u.wait(round, debRetryDelay) {
			return
		}
	}
}

// gate 重新查询真实包版本; 低版本和非法目标直接结束, 查询错误才进入有限补试.
func (u *debInstaller) gate(target debTarget, round uint64, retryInstall bool) (bool, error) {
	if !u.current(round) || !u.eligible(target.version, target.threshold) {
		return false, nil
	}
	if !validDebName(u.name) {
		return false, nil
	}
	valid := u.run(debQueryTimeout, debDpkg, "--validate-version", target.version)
	if valid.err != nil {
		if valid.exit >= 0 {
			logger.Warn().Str("version", target.version).Msg("Invalid Debian target version")
			return false, nil
		}
		return false, valid.failure("validate version")
	}
	installed, err := queryDebVersion(u.run, u.name)
	if err != nil || installed == "" {
		return false, err
	}
	// 相等只允许补偿同轮失败的install, 不增加独立的同版修复任务.
	if installed == target.version {
		return retryInstall && u.current(round), nil
	}
	compared := u.run(debQueryTimeout, debDpkg, "--compare-versions", target.version, "gt", installed)
	if compared.exit == 1 {
		// Debian语义相等不要求字面相同, 例如1.0与1.0-0; 同轮失败仍可补试.
		equal := u.run(debQueryTimeout, debDpkg, "--compare-versions", target.version, "eq", installed)
		if equal.err == nil {
			return retryInstall && u.current(round), nil
		}
		if equal.exit != 1 {
			return false, equal.failure("compare equivalent versions")
		}
		logger.Warn().Str("installed", installed).Str("target", target.version).Msg("Debian downgrade rejected")
		return false, nil
	}
	if compared.err != nil {
		return false, compared.failure("compare versions")
	}
	return u.current(round), nil
}

// updateIndexes 对索引做一次有限补试, 返回false只表示授权撤销, 更新错误不阻断安装.
func (u *debInstaller) updateIndexes(round uint64) bool {
	for update := 0; update < 2; update++ {
		if !u.current(round) {
			return false
		}
		result := u.run(0, debAPTArgs("update", "")...)
		if result.err == nil {
			break
		}
		logger.Warn().Err(result.failure("update")).Msg("Debian index update failed")
		if update == 0 && !u.wait(round, debUpdateRetryDelay) {
			return false
		}
	}
	return true
}

// attempt 顺序执行索引更新、必要的配置恢复和精确安装, 仅查询或install失败消耗安装补试.
func (u *debInstaller) attempt(target debTarget, round uint64, previous debAttempt) debAttempt {
	allowed, err := u.gate(target, round, previous.retryInstall)
	if !allowed || err != nil {
		return debAttempt{err: err}
	}
	if !u.updateIndexes(round) {
		return debAttempt{}
	}
	allowed, err = u.gate(target, round, previous.retryInstall)
	if !allowed || err != nil {
		return debAttempt{err: err}
	}
	if previous.configure {
		result := u.run(0, debDpkg, "--force-confdef", "--force-confold", "--configure", "-a")
		if result.err != nil {
			logger.Warn().Err(result.failure("configure")).Msg("Debian configuration recovery failed")
		}
		allowed, err = u.gate(target, round, previous.retryInstall)
		if !allowed || err != nil {
			return debAttempt{err: err}
		}
	}
	if !u.current(round) {
		return debAttempt{}
	}
	result := u.run(0, debAPTArgs("install", u.name+"="+target.version)...)
	if result.err == nil {
		logger.Info().Str("package", u.name).Str("version", target.version).Msg("Debian package installed")
		return debAttempt{}
	}
	return debAttempt{
		err: result.failure("install"), retryInstall: true,
		configure: strings.Contains(result.output, "dpkg was interrupted") && strings.Contains(result.output, "dpkg --configure -a"),
	}
}

// canary 沿用内外IP及版本的确定性分桶; 100%无需IP, 非法比例或无可用IP不安装.
func canary(version string, threshold uint64) bool {
	if threshold == 0 || threshold > 100 {
		return false
	}
	if threshold == 100 {
		return true
	}
	validIP := func(s string) bool { ip := net.ParseIP(s); return ip != nil && !ip.IsUnspecified() }
	if !validIP(common.InternalIPv4) && !validIP(common.ExternalIPv4) {
		return false
	}
	return xhash.HashString64(common.InternalIPv4, common.ExternalIPv4, version)%100 < threshold
}

// DebVersion 查询完整Debian版本, 保留公开签名; 非Linux、缺包或查询失败返回空字符串.
func DebVersion(name string) string {
	if !sysenv.IsLinux() || !validDebName(name) {
		return ""
	}
	version, err := queryDebVersion(runDebCommand, name)
	if err != nil {
		logger.Error().Err(err).Str("package", name).Msg("Failed to query Debian version")
	}
	return version
}

// DebVersionByService 沿用既有服务名到包名的转换, 不安装或修复包.
func DebVersionByService(name string) string { return DebVersion(config.GetDevName(name)) }
