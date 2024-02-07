package master

import (
	"fmt"
	"regexp"
	"sync/atomic"
	"time"

	"github.com/fufuok/utils"
	"github.com/fufuok/utils/xhash"

	"github.com/fufuok/pkg/cmder"
	"github.com/fufuok/pkg/common"
	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/logger"
)

const (
	// ubuntu apt 命令
	aptBin = "/usr/bin/apt"

	// apt install 执行超时时间
	aptTimeout = 30 * time.Minute
)

var (
	// 包是否已在安装中
	debInstalling atomic.Bool

	// 修正软件包配置命令
	cmdDpkgConfigure = []string{"/usr/bin/dpkg", "--configure", "-a"}

	// 获取系统安装的软件包版本 (Ubuntu 20.04)
	cmdDebVersion = []string{"/usr/bin/dpkg", "-l"}

	// 版本号数据获取正则
	// ii  ff-app 0.1.5.221117173434 amd64        ff-app
	verRegexpTpl = `\s%s\s+([\w.-]+)\s`
)

func installDeb(ver string) {
	if !debInstalling.CompareAndSwap(false, true) {
		logger.Warn().Str("ver", ver).Msg("skip installing deb")
		return
	}
	defer debInstalling.Store(false)

	// 随机一定的时间执行, 减少仓库压力
	wait := utils.FastIntn(config.Config().MainConf.RandomWait)
	time.Sleep(time.Duration(wait) * time.Second)

	deb := fmt.Sprintf("%s=%s", config.DebName, ver)
	updateCmd := []string{aptBin, "update"}
	installCmd := []string{aptBin, "install", deb}

	status := cmder.RunCmd(updateCmd, aptTimeout)
	logger.Warn().Str("deb", deb).Float64("took_s", status.Runtime).
		Strs("stdout", status.Stdout).Strs("stderr", status.Stderr).
		Strs("cmd", updateCmd).
		Msg("install deb")

	status = cmder.RunCmd(cmdDpkgConfigure)
	logger.Warn().Str("deb", deb).Float64("took_s", status.Runtime).
		Strs("stdout", status.Stdout).Strs("stderr", status.Stderr).
		Strs("cmd", cmdDpkgConfigure).
		Msg("install deb")

	status = cmder.RunCmd(installCmd, aptTimeout)
	if status.Exit != 0 {
		logger.Error().Err(status.Error).Int("exit_code", status.Exit).Float64("took_s", status.Runtime).
			Str("deb", deb).Strs("stdout", status.Stdout).Strs("stderr", status.Stderr).
			Strs("cmd", installCmd).
			Msg("install deb")
	} else {
		config.DebVersion = ver
		logger.Warn().Str("deb", deb).Float64("took_s", status.Runtime).
			Strs("stdout", status.Stdout).Strs("stderr", status.Stderr).
			Strs("cmd", installCmd).
			Msg("install deb")
	}
}

// 判断是否满足安装条件
// 配置: [0-100]
// 算法: Hash(内网 IP + 外网 IP + dev_version) % 100 < canary_deployment
func canary(ver string, threshold uint64) bool {
	h := xhash.HashString64(common.InternalIPv4, common.ExternalIPv4, ver)
	v := h % 100
	return v < threshold
}

// 获取当前安装的包版本
func getCurrentDebVersion() string {
	return DebVersion(config.DebName)
}

// DebVersion 获取当前安装的包版本
func DebVersion(debName string) string {
	if debName == "" {
		return ""
	}
	// dpkg -l ff-app
	status := cmder.RunCmd(append(cmdDebVersion, debName))
	n := len(status.Stdout)
	if n == 0 || status.Exit != 0 {
		logger.Error().Err(status.Error).Int("exit_code", status.Exit).Float64("took_s", status.Runtime).
			Strs("stdout", status.Stdout).Strs("stderr", status.Stderr).
			Msg("dpkg -l")
		return ""
	}

	verRegexp, err := regexp.Compile(fmt.Sprintf(verRegexpTpl, debName))
	if err != nil {
		return ""
	}
	res := verRegexp.FindStringSubmatch(status.Stdout[n-1])
	if len(res) == 2 {
		return res[1]
	}
	return ""
}

// DebVersionByService ff-app.service
func DebVersionByService(name string) string {
	return DebVersion(config.GetDevName(name))
}
