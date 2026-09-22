package master

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/fufuok/pkg/sysenv"
)

const (
	// 包管理器路径统一来自 sysenv, 本包只保留超时和输出上限.
	debAPTGet       = sysenv.BinAptGet
	debDpkg         = sysenv.BinDpkg
	debDpkgQuery    = sysenv.BinDpkgQuery
	debQueryTimeout = 5 * time.Second
	debOutputLimit  = 64 * 1024
)

var debNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]+$`)

// debCommandResult 保留退出结果与有界合并输出, 不把命令启动失败误报为成功.
type debCommandResult struct {
	output string
	exit   int
	err    error
}

// debOutput 持续消费但仅保留前64KiB; 相同Writer让os/exec串行写入stdout和stderr.
type debOutput struct {
	body      []byte
	truncated bool
}

// Write 超限时仍返回完整消费长度, 防止日志截断阻塞包管理器.
func (w *debOutput) Write(p []byte) (int, error) {
	keep := min(len(p), debOutputLimit-len(w.body))
	w.body = append(w.body, p[:keep]...)
	w.truncated = w.truncated || keep < len(p)
	return len(p), nil
}

// runDebCommand 仅只读查询使用超时; 修改命令不绑定配置或Stop取消, Run保证Wait收尾.
func runDebCommand(timeout time.Duration, args ...string) debCommandResult {
	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	command := exec.CommandContext(ctx, args[0], args[1:]...)
	command.Env = append(os.Environ(), "LC_ALL=C", "DEBIAN_FRONTEND=noninteractive")
	// 命令已退出而后代仍持有输出管道时, 不让日志复制永久阻塞收尾.
	command.WaitDelay = debQueryTimeout
	output := &debOutput{}
	command.Stdout, command.Stderr = output, output
	err := command.Run()
	exit := -1
	if command.ProcessState != nil {
		exit = command.ProcessState.ExitCode()
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	text := string(output.body)
	if output.truncated {
		text += "\n[output truncated]"
	}
	return debCommandResult{text, exit, err}
}

// validDebName 限定单个Debian包名, 禁止APT模式、架构选择符或参数混入.
func validDebName(name string) bool { return debNamePattern.MatchString(name) }

// debToolsReady 按Linux、root权限和工具能力检查, 不依赖发行版标签或systemd.
func debToolsReady() error {
	if !sysenv.IsLinux() {
		return fmt.Errorf("Debian installation requires Linux")
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("Debian installation requires root")
	}
	for _, tool := range []string{debAPTGet, debDpkg, debDpkgQuery} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("required tool %s: %w", tool, err)
		}
	}
	return nil
}

// queryDebVersion 只把明确缺包或仅残留配置视作空版本, 其他错误不能伪装成低版本.
// 使用原始Status字段兼容Ubuntu 14.04的dpkg 1.17.5, 不依赖1.17.11才加入的状态虚拟字段.
func queryDebVersion(run func(time.Duration, ...string) debCommandResult, name string) (string, error) {
	result := run(debQueryTimeout, debDpkgQuery, "-W", "-f=${Status}\t${Version}\n", "--", name)
	if result.exit == 1 && strings.Contains(result.output, "no packages found matching") {
		return "", nil
	}
	if result.err != nil {
		return "", result.failure("query version")
	}
	status, version, ok := strings.Cut(strings.TrimSpace(result.output), "\t")
	state := strings.Fields(status)
	if !ok || len(state) != 3 || version == "" {
		return "", fmt.Errorf("unexpected dpkg-query output: %q", result.output)
	}
	if state[2] == "not-installed" || state[2] == "config-files" {
		return "", nil
	}
	return version, nil
}

// debAPTArgs 固定非交互和原生锁参数; update严格模式不构成安装门禁.
// 精确安装显式覆盖主机放宽策略, 查询后外部升级也不能引发APT降级或删除包.
func debAPTArgs(action, target string) []string {
	args := []string{
		debAPTGet,
		"-o", "Acquire::Retries=0", "-o", "Acquire::http::Timeout=30", "-o", "Acquire::https::Timeout=30",
		"-o", "DPkg::Lock::Timeout=60",
	}
	if action == "update" {
		return append(args, "-o", "APT::Update::Error-Mode=any", "update")
	}
	return append(
		args,
		"-o", "APT::Get::allow-downgrades=false", "-o", "APT::Get::force-yes=false",
		"-o", "APT::Get::allow-change-held-packages=false",
		"-o", "Dpkg::Options::=--force-confdef", "-o", "Dpkg::Options::=--force-confold",
		"-y", "--no-remove", "--only-upgrade", "install", target,
	)
}

// failure 为有界命令结果补充阶段, 不隐藏退出码及执行失败原因.
func (r debCommandResult) failure(stage string) error {
	return fmt.Errorf("%s exited %d: %w; output: %s", stage, r.exit, r.err, r.output)
}
