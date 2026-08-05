package xfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fufuok/pkg/assert"
)

// TestNewRollerValidation 验证空文件名和无法创建文件的路径返回稳定错误,
// 成功构造时则创建目标文件并保持可关闭状态.
func TestNewRollerValidation(t *testing.T) {
	roller, err := NewRoller("", &Options{})
	assert.Nil(t, roller)
	assert.True(t, errors.Is(err, ErrFilename))

	invalidFile := filepath.Join(t.TempDir(), "missing-parent", "app.log")
	roller, err = NewRoller(invalidFile, &Options{})
	assert.Nil(t, roller)
	assert.NotNil(t, err)

	filename := filepath.Join(t.TempDir(), "app.log")
	roller, closeRoller := newXFileTestRoller(t, filename, &Options{FlushInterval: time.Hour})
	assert.NotNil(t, roller)
	assert.True(t, IsFile(filename))
	closeRoller()
}

// TestRollerOptionNormalization 通过真实 Roller 验证默认值、最小值和显式选项.
// nil Options 的既有异常状态不属于公开契约, 不在一期测试中固化.
func TestRollerOptionNormalization(t *testing.T) {
	root := t.TempDir()
	defaults, closeDefaults := newXFileTestRoller(t, filepath.Join(root, "defaults.log"), &Options{})
	_, defaultMaker := defaults.maker.(*DefaultFilename)
	_, defaultLogger := defaults.logger.(*stdLogger)
	assert.True(t, defaultMaker)
	assert.True(t, defaultLogger)
	assert.Equal(t, DefaultFlushSizeLimit, defaults.flushSizeLimit)
	assert.Equal(t, DefaultFlushInterval, defaults.flushInterval)
	closeDefaults()

	minimums, closeMinimums := newXFileTestRoller(t, filepath.Join(root, "minimums.log"), &Options{
		FlushSizeLimit: -1,
		FlushInterval:  -1,
	})
	assert.Equal(t, MinFlushSizeLimit, minimums.flushSizeLimit)
	assert.Equal(t, MinFlushInterval, minimums.flushInterval)
	closeMinimums()

	maker := &scriptedFilenameMaker{}
	logger := new(recordingXFileLogger)
	explicit, closeExplicit := newXFileTestRoller(t, filepath.Join(root, "explicit.log"), &Options{
		FilenameMaker:  maker,
		Logger:         logger,
		Rebuild:        true,
		FlushSizeLimit: MinFlushSizeLimit + 512,
		FlushInterval:  2 * MinFlushInterval,
	})
	assert.Equal(t, FilenameMaker(maker), explicit.maker)
	assert.Equal(t, Logger(logger), explicit.logger)
	assert.True(t, explicit.rebuild)
	assert.Equal(t, MinFlushSizeLimit+512, explicit.flushSizeLimit)
	assert.Equal(t, 2*MinFlushInterval, explicit.flushInterval)
	closeExplicit()
}

// TestRollerWriteAndClose 验证 Write 和 WriteString 追加到已有文件,
// Close 会把剩余缓冲同步到真实文件.
func TestRollerWriteAndClose(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "append.log")
	writeXFileTestFile(t, filename, "before\n")
	roller, closeRoller := newXFileTestRoller(t, filename, &Options{FlushInterval: time.Hour})

	n, err := roller.Write([]byte("bytes\n"))
	assert.Nil(t, err)
	assert.Equal(t, len("bytes\n"), n)
	n, err = roller.WriteString("string\n")
	assert.Nil(t, err)
	assert.Equal(t, len("string\n"), n)
	closeRoller()

	assert.Equal(t, "before\nbytes\nstring\n", readXFileTestFile(t, filename))
}

// TestRollerConcurrentWrites 使用真实文件验证多协程写入不会丢失或拼接记录.
// 记录顺序不属于公开契约, 因此只校验每个唯一记录恰好出现一次.
func TestRollerConcurrentWrites(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "concurrent.log")
	roller, closeRoller := newXFileTestRoller(t, filename, &Options{FlushInterval: time.Hour})

	const (
		writers = 24
		lines   = 20
	)
	var wg sync.WaitGroup
	writeErrors := make(chan error, writers)
	for writerID := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for lineID := range lines {
				_, err := roller.WriteString(fmt.Sprintf("%02d-%02d\n", writerID, lineID))
				if err != nil {
					writeErrors <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(writeErrors)
	for err := range writeErrors {
		assert.Nil(t, err)
	}
	closeRoller()

	actualLines := strings.Split(strings.TrimSuffix(readXFileTestFile(t, filename), "\n"), "\n")
	assert.Equal(t, writers*lines, len(actualLines))
	seen := make(map[string]int, len(actualLines))
	for _, line := range actualLines {
		seen[line]++
	}
	for writerID := range writers {
		for lineID := range lines {
			assert.Equal(t, 1, seen[fmt.Sprintf("%02d-%02d", writerID, lineID)])
		}
	}
}

// TestRollerTimerFlush 验证最小定时间隔会把未满缓冲的数据写入磁盘.
// 最长等待显著大于最小间隔, 只校验最终可见性而不依赖精确调度时刻.
func TestRollerTimerFlush(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "timer.log")
	roller, closeRoller := newXFileTestRoller(t, filename, &Options{FlushInterval: MinFlushInterval})
	defer closeRoller()
	_, err := roller.WriteString("timer flush")
	assert.Nil(t, err)

	deadline := time.Now().Add(2 * time.Second)
	for {
		content, readErr := os.ReadFile(filename)
		assert.Nil(t, readErr)
		if string(content) == "timer flush" {
			break
		}
		if time.Now().After(deadline) {
			assert.Equal(t, "timer flush", string(content))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestRollerRollover 验证 flush 先写完旧文件再切换到 maker 返回的新文件,
// 空缓冲时不会无意义调用文件名生成器.
func TestRollerRollover(t *testing.T) {
	root := t.TempDir()
	firstFile := filepath.Join(root, "first.log")
	secondFile := filepath.Join(root, "second.log")
	maker := &scriptedFilenameMaker{next: secondFile}
	roller, closeRoller := newXFileTestRoller(t, firstFile, &Options{
		FilenameMaker: maker,
		FlushInterval: time.Hour,
	})
	roller.flush()
	assert.Equal(t, 0, maker.callCount())

	_, err := roller.WriteString("first")
	assert.Nil(t, err)
	roller.flush()
	assert.Equal(t, 1, maker.callCount())
	assert.Equal(t, "first", readXFileTestFile(t, firstFile))

	_, err = roller.WriteString("second")
	assert.Nil(t, err)
	closeRoller()
	assert.Equal(t, "second", readXFileTestFile(t, secondFile))
}

// TestRollerReportsIOErrors 验证刷新失败和滚动目标无法创建时调用配置的 Logger,
// 同时确认已成功刷新的旧文件内容仍然保留.
func TestRollerReportsIOErrors(t *testing.T) {
	t.Run("flush closed file", func(t *testing.T) {
		filename := filepath.Join(t.TempDir(), "closed.log")
		logger := new(recordingXFileLogger)
		roller, closeRoller := newXFileTestRoller(t, filename, &Options{
			Logger:        logger,
			FlushInterval: time.Hour,
		})
		defer closeRoller()
		_, err := roller.WriteString("buffered")
		assert.Nil(t, err)
		assert.Nil(t, roller.file.Close())
		roller.flush()
		assert.True(t, logger.contains("Failed to write file:"))
	})

	t.Run("rollover target is unavailable", func(t *testing.T) {
		root := t.TempDir()
		filename := filepath.Join(root, "current.log")
		logger := new(recordingXFileLogger)
		maker := &scriptedFilenameMaker{next: filepath.Join(root, "missing", "next.log")}
		roller, closeRoller := newXFileTestRoller(t, filename, &Options{
			FilenameMaker: maker,
			Logger:        logger,
			FlushInterval: time.Hour,
		})
		defer closeRoller()
		_, err := roller.WriteString("preserved")
		assert.Nil(t, err)
		roller.flush()
		assert.True(t, logger.contains("Unable to create new file:"))
		assert.Equal(t, "preserved", readXFileTestFile(t, filename))
	})
}

// TestFilenameMakers 验证默认文件名保持不变, 时间模板首次生成新名称后
// 在时间标签未变化时复用调用方传入的现有名称.
func TestFilenameMakers(t *testing.T) {
	defaultMaker := new(DefaultFilename)
	assert.Equal(t, "existing.log", defaultMaker.MakeFilename("existing.log"))

	root := t.TempDir()
	timeMaker := &TimeBasedFilename{
		FilePath:    root,
		FilenameTpl: "run-%s.log",
		TimeTpl:     "fixed",
	}
	generated := timeMaker.MakeFilename("old.log")
	assert.Equal(t, filepath.Join(root, "run-fixed.log"), generated)
	assert.Equal(t, "fixed", timeMaker.TimeTag)
	assert.Equal(t, generated, timeMaker.MakeFilename(generated))

	withoutPath := &TimeBasedFilename{FilenameTpl: "%s.log", TimeTpl: "fixed"}
	assert.Equal(t, "fixed.log", withoutPath.MakeFilename(""))
}

// newXFileTestRoller 创建真实 Roller, 并返回可重复调用的关闭函数.
// cleanup 使用 sync.Once 保证断言失败和显式关闭不会触发重复关闭通道 panic.
func newXFileTestRoller(t *testing.T, filename string, opt *Options) (*Roller, func()) {
	t.Helper()
	roller, err := NewRoller(filename, opt)
	assert.Nil(t, err)
	assert.NotNil(t, roller)
	var once sync.Once
	closeRoller := func() {
		once.Do(roller.Close)
	}
	t.Cleanup(closeRoller)
	return roller, closeRoller
}

// scriptedFilenameMaker 首次返回预设滚动目标, 后续保持当前名称.
type scriptedFilenameMaker struct {
	mu    sync.Mutex
	next  string
	calls int
}

// MakeFilename 实现 FilenameMaker, 并记录真实调用次数供空缓冲断言使用.
func (m *scriptedFilenameMaker) MakeFilename(current string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	if m.next == "" {
		return current
	}
	next := m.next
	m.next = ""
	return next
}

// callCount 返回线程安全的生成器调用次数.
func (m *scriptedFilenameMaker) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// recordingXFileLogger 保存 Roller I/O 错误, 便于验证错误路径没有吞错.
type recordingXFileLogger struct {
	mu       sync.Mutex
	messages []string
}

// Errorf 实现 Logger, 将格式化后的错误消息安全追加到内存.
func (l *recordingXFileLogger) Errorf(format string, values ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = append(l.messages, fmt.Sprintf(format, values...))
}

// contains 判断是否至少有一条日志包含指定稳定前缀.
func (l *recordingXFileLogger) contains(value string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, message := range l.messages {
		if strings.Contains(message, value) {
			return true
		}
	}
	return false
}
