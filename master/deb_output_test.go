package master

import (
	"bytes"
	"strings"
	"testing"
	"testing/synctest"
	"time"
	"unicode/utf8"

	"github.com/rs/zerolog"

	"github.com/fufuok/pkg/common"
)

// TestDebInstallerUpdateStartLog 保证每条实际执行的update恰好对应一条开始日志, 撤销等待不虚报命令.
func TestDebInstallerUpdateStartLog(t *testing.T) {
	for _, kind := range []string{"success", "retry", "cancel_wait"} {
		t.Run(kind, func(t *testing.T) {
			var output bytes.Buffer
			log := common.Log()
			previous := *log
			*log = zerolog.New(&output)
			defer func() { *log = previous }()
			synctest.Test(t, func(t *testing.T) {
				u, f := newDebFixture(t)
				if kind != "success" {
					f.updateFailures = 1
				}
				u.publish()
				synctest.Wait()
				if kind == "cancel_wait" {
					u.stop()
				}
				time.Sleep(time.Minute)
				synctest.Wait()
				updates := 0
				for _, call := range f.recorded() {
					if call == "update" {
						updates++
					}
				}
				started := strings.Count(output.String(), "Debian index update started")
				if started != updates {
					t.Fatalf("update commands=%d, start logs=%d", updates, started)
				}
			})
		})
	}
}

// TestDebOutputUnicodeTail 验证分片写入和尾窗截断不破坏UTF-8字符.
func TestDebOutputUnicodeTail(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
	}{
		{name: "full_width_limit", input: strings.Repeat("😀", debLogTailRunes), want: strings.Repeat("😀", debLogTailRunes)},
		{name: "full_width_truncated", input: "x" + strings.Repeat("😀", debLogTailRunes), want: debLogTailMarker + strings.Repeat("😀", debLogTailRunes)},
		{name: "partial_first_rune", input: strings.Repeat("界", 2800) + "终", want: debLogTailMarker + strings.Repeat("界", debLogTailRunes-1) + "终"},
		{name: "trimmed_short", input: "  完成  \n", want: "完成"},
		{name: "trimmed_truncated", input: strings.Repeat(" ", debLogTailLimit+1) + "完成\n", want: debLogTailMarker + "完成"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var captured debOutput
			data := []byte(tc.input)
			// 用7字节分片覆盖跨字符写入.
			for offset := 0; offset < len(data); offset += 7 {
				chunk := data[offset:min(offset+7, len(data))]
				if n, err := captured.Write(chunk); n != len(chunk) || err != nil {
					t.Fatalf("write consumed %d/%d bytes: %v", n, len(chunk), err)
				}
			}
			_, _ = captured.Write(nil)
			if len(captured.body) > debOutputLimit || len(captured.tail) > debLogTailLimit {
				t.Fatal("output buffers exceeded their byte limits")
			}
			got := captured.result(0, nil).logTail()
			if !utf8.ValidString(got) || got != tc.want {
				t.Fatalf("unexpected UTF-8 tail: valid=%t, runes=%d, want=%d", utf8.ValidString(got), utf8.RuneCountInString(got), utf8.RuneCountInString(tc.want))
			}
		})
	}
}

// TestDebInstallerRecoveryUsesPrefix 验证仅前64KiB中的失败诊断可触发configure.
func TestDebInstallerRecoveryUsesPrefix(t *testing.T) {
	const diagnostic = "dpkg was interrupted, run 'dpkg --configure -a'"
	large := strings.Repeat("p", debOutputLimit+1)
	for _, tc := range []struct {
		name, output       string
		success, configure bool
	}{
		{name: "short_prefix", output: diagnostic, configure: true},
		{name: "prefix_before_large_output", output: diagnostic + large, configure: true},
		{name: "tail_only", output: large + diagnostic},
		{name: "split_prefix_and_tail", output: "dpkg was interrupted" + large + "dpkg --configure -a"},
		{name: "successful_command", output: diagnostic, success: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				u, f := newDebFixture(t)
				f.installFailures = 2
				u.run = func(timeout time.Duration, args ...string) debCommandResult {
					result := f.run(timeout, args...)
					if args[0] != debAPTGet || args[len(args)-2] != "install" {
						return result
					}
					var captured debOutput
					_, _ = captured.Write([]byte(tc.output))
					if tc.success {
						return captured.result(0, nil)
					}
					return captured.result(result.exit, result.err)
				}
				u.publish()
				time.Sleep(time.Hour)
				synctest.Wait()
				switch {
				case tc.configure:
					requireDebActions(t, f, "update", "install:2", "update", "configure", "install:2")
				case tc.success:
					requireDebActions(t, f, "update", "install:2")
				default:
					requireDebActions(t, f, "update", "install:2", "update", "install:2")
				}
			})
		})
	}
}
