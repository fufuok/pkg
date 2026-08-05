package xfile

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fufuok/pkg/assert"
)

// TestPathPredicatesAndResetDir 验证文件、目录和缺失路径的判断语义,
// 并确认 ResetDir 会清空已有目录或创建新的嵌套目录.
func TestPathPredicatesAndResetDir(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "file.txt")
	dir := filepath.Join(root, "dir")
	missing := filepath.Join(root, "missing")
	writeXFileTestFile(t, file, "content")
	assert.Nil(t, os.Mkdir(dir, 0o755))

	assert.True(t, IsExist(file))
	assert.True(t, IsExist(dir))
	assert.False(t, IsExist(missing))
	assert.True(t, IsFile(file))
	assert.False(t, IsFile(dir))
	assert.False(t, IsFile(missing))
	assert.True(t, IsDir(dir))
	assert.False(t, IsDir(file))
	assert.False(t, IsDir(missing))

	resetDir := filepath.Join(root, "reset")
	writeXFileTestFile(t, filepath.Join(resetDir, "nested", "old.txt"), "old")
	assert.Nil(t, ResetDir(resetDir))
	entries, err := os.ReadDir(resetDir)
	assert.Nil(t, err)
	assert.Equal(t, 0, len(entries))

	newDir := filepath.Join(root, "new", "nested")
	assert.Nil(t, ResetDir(newDir))
	assert.True(t, IsDir(newDir))

	blocker := filepath.Join(root, "blocker")
	writeXFileTestFile(t, blocker, "file blocks child directory")
	assert.NotNil(t, ResetDir(filepath.Join(blocker, "child")))
}

// TestReadFileAndLineRanges 使用固定临时内容验证全文读取、头部读取、偏移读取、
// 零行和越界行为, 避免测试结果依赖测试源文件自身的格式.
func TestReadFileAndLineRanges(t *testing.T) {
	root := t.TempDir()
	filename := filepath.Join(root, "lines.txt")
	content := "alpha\nbeta\ngamma\nomega"
	writeXFileTestFile(t, filename, content)

	text, err := ReadFile(filename)
	assert.Nil(t, err)
	assert.Equal(t, content, text)

	lines, err := ReadLines(filename)
	assert.Nil(t, err)
	assert.Equal(t, []string{"alpha", "beta", "gamma", "omega"}, lines)

	head, err := HeadLines(filename, 2)
	assert.Nil(t, err)
	assert.Equal(t, []string{"alpha", "beta"}, head)

	all, err := HeadLines(filename, -1)
	assert.Nil(t, err)
	assert.Equal(t, lines, all)

	rangeLines, err := ReadLinesOffsetN(filename, 1, 2)
	assert.Nil(t, err)
	assert.Equal(t, []string{"beta", "gamma"}, rangeLines)

	remaining, err := ReadLinesOffsetN(filename, 2, -1)
	assert.Nil(t, err)
	assert.Equal(t, []string{"gamma", "omega"}, remaining)

	empty, err := ReadLinesOffsetN(filename, 2, 0)
	assert.Nil(t, err)
	assert.Equal(t, 0, len(empty))

	missing := filepath.Join(root, "missing.txt")
	text, err = ReadFile(missing)
	assert.NotNil(t, err)
	assert.Equal(t, "", text)
	lines, err = ReadLines(missing)
	assert.NotNil(t, err)
	assert.Equal(t, 0, len(lines))
}

// TestTailLines 验证尾部换行、空白行清理和空文件行为.
// 大文件用例确认文件超过读取块时仍能稳定返回位于末块中的目标行.
func TestTailLines(t *testing.T) {
	root := t.TempDir()
	filename := filepath.Join(root, "tail.txt")
	writeXFileTestFile(t, filename, "one\n \nthree\nfour\n")

	lines, err := TailLines(filename, 2)
	assert.Nil(t, err)
	assert.Equal(t, []string{"four", ""}, lines)

	cleanLines, err := TailLines(filename, 2, true)
	assert.Nil(t, err)
	assert.Equal(t, []string{"three", "four"}, cleanLines)

	all, err := TailLines(filename, -1)
	assert.Nil(t, err)
	assert.Equal(t, []string{"one", " ", "three", "four"}, all)

	noLines, err := TailLines(filepath.Join(root, "missing.txt"), 0)
	assert.Nil(t, err)
	assert.Equal(t, 0, len(noLines))
	noLines, err = TailLines(filepath.Join(root, "missing.txt"), 1)
	assert.NotNil(t, err)
	assert.Equal(t, 0, len(noLines))

	emptyFile := filepath.Join(root, "empty.txt")
	writeXFileTestFile(t, emptyFile, "")
	noLines, err = TailLines(emptyFile, 5)
	assert.Nil(t, err)
	assert.Equal(t, 0, len(noLines))

	largeLines := make([]string, 400)
	for i := range largeLines {
		largeLines[i] = strings.Repeat("x", 32) + "-" + time.Unix(int64(i), 0).UTC().Format("150405")
	}
	largeFile := filepath.Join(root, "large.txt")
	writeXFileTestFile(t, largeFile, strings.Join(largeLines, "\n"))
	tail, err := TailLines(largeFile, 50)
	assert.Nil(t, err)
	assert.Equal(t, largeLines[len(largeLines)-50:], tail)
}

// TestModTime 验证存在文件返回非零修改时间, 缺失路径返回时间零值.
func TestModTime(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "mtime.txt")
	writeXFileTestFile(t, filename, "mtime")

	mtime := ModTime(filename)
	assert.NotEqual(t, time.Time{}, mtime)
	assert.Equal(t, time.Time{}, ModTime(filepath.Join(t.TempDir(), "missing.txt")))
}

// TestCopyFileAndCopyDir 验证覆盖复制、嵌套目录和空目录均保持内容,
// 同时覆盖源文件缺失和目标父目录缺失的稳定错误路径.
func TestCopyFileAndCopyDir(t *testing.T) {
	root := t.TempDir()
	sourceFile := filepath.Join(root, "source.txt")
	destinationFile := filepath.Join(root, "destination.txt")
	writeXFileTestFile(t, sourceFile, "source content")
	writeXFileTestFile(t, destinationFile, "stale content")

	assert.Nil(t, CopyFile(sourceFile, destinationFile))
	assert.Equal(t, "source content", readXFileTestFile(t, destinationFile))
	assert.NotNil(t, CopyFile(filepath.Join(root, "missing.txt"), destinationFile))
	assert.NotNil(t, CopyFile(sourceFile, filepath.Join(root, "missing-parent", "file.txt")))

	sourceDir := filepath.Join(root, "source-dir")
	assert.Nil(t, os.MkdirAll(filepath.Join(sourceDir, "empty"), 0o755))
	writeXFileTestFile(t, filepath.Join(sourceDir, "root.txt"), "root")
	writeXFileTestFile(t, filepath.Join(sourceDir, "nested", "child.txt"), "child")
	destinationDir := filepath.Join(root, "destination-dir")
	assert.Nil(t, CopyDir(sourceDir, destinationDir))
	assert.Equal(t, "root", readXFileTestFile(t, filepath.Join(destinationDir, "root.txt")))
	assert.Equal(t, "child", readXFileTestFile(t, filepath.Join(destinationDir, "nested", "child.txt")))
	assert.True(t, IsDir(filepath.Join(destinationDir, "empty")))
	assert.NotNil(t, CopyDir(filepath.Join(root, "missing-dir"), filepath.Join(root, "unused")))
}

// TestZipDirAndUnzipDir 使用真实 zip 文件验证目录、空目录和文件内容往返,
// 并直接验证 UnzipFile 的成功与目标父目录缺失错误路径.
func TestZipDirAndUnzipDir(t *testing.T) {
	root := t.TempDir()
	sourceDir := filepath.Join(root, "source")
	assert.Nil(t, os.MkdirAll(filepath.Join(sourceDir, "empty"), 0o755))
	writeXFileTestFile(t, filepath.Join(sourceDir, "root.txt"), "root data")
	writeXFileTestFile(t, filepath.Join(sourceDir, "nested", "child.txt"), "child data")

	archive := filepath.Join(root, "archive.zip")
	assert.Nil(t, ZipDir(sourceDir, archive))

	reader, err := zip.OpenReader(archive)
	assert.Nil(t, err)
	t.Cleanup(func() {
		_ = reader.Close()
	})
	entries := make(map[string]*zip.File, len(reader.File))
	for _, file := range reader.File {
		entries[strings.ReplaceAll(file.Name, "\\", "/")] = file
	}
	assert.NotNil(t, entries["empty/"])
	assert.NotNil(t, entries["nested/"])
	assert.NotNil(t, entries["nested/child.txt"])
	assert.NotNil(t, entries["root.txt"])

	destinationDir := filepath.Join(root, "destination")
	assert.Nil(t, os.Mkdir(destinationDir, 0o755))
	assert.Nil(t, UnzipDir(archive, destinationDir))
	assert.True(t, IsDir(filepath.Join(destinationDir, "empty")))
	assert.Equal(t, "root data", readXFileTestFile(t, filepath.Join(destinationDir, "root.txt")))
	assert.Equal(t, "child data", readXFileTestFile(t, filepath.Join(destinationDir, "nested", "child.txt")))

	directFile := filepath.Join(root, "direct.txt")
	assert.Nil(t, UnzipFile(entries["root.txt"], directFile))
	assert.Equal(t, "root data", readXFileTestFile(t, directFile))
	assert.NotNil(t, UnzipFile(entries["root.txt"], filepath.Join(root, "missing-parent", "direct.txt")))
}

// TestZipErrorPaths 验证缺失源目录、非法输出位置、损坏压缩包和目录穿越均返回错误.
// 穿越用例同时确认目标目录外不会产生文件.
func TestZipErrorPaths(t *testing.T) {
	root := t.TempDir()
	assert.NotNil(t, ZipDir(filepath.Join(root, "missing-source"), filepath.Join(root, "missing-source.zip")))
	assert.NotNil(t, ZipDir(root, filepath.Join(root, "missing-parent", "archive.zip")))

	badArchive := filepath.Join(root, "bad.zip")
	writeXFileTestFile(t, badArchive, "not a zip archive")
	assert.NotNil(t, UnzipDir(badArchive, filepath.Join(root, "bad-output")))

	escapeArchive := filepath.Join(root, "escape.zip")
	writeXFileTestZipEntry(t, escapeArchive, "../escape.txt", "blocked")
	destinationDir := filepath.Join(root, "destination")
	assert.NotNil(t, UnzipDir(escapeArchive, destinationDir))
	assert.False(t, IsExist(filepath.Join(root, "escape.txt")))
}

// writeXFileTestFile 创建父目录并写入固定测试内容.
func writeXFileTestFile(t *testing.T, filename, content string) {
	t.Helper()
	assert.Nil(t, os.MkdirAll(filepath.Dir(filename), 0o755))
	assert.Nil(t, os.WriteFile(filename, []byte(content), 0o644))
}

// readXFileTestFile 读取测试文件并在失败时立即终止当前用例.
func readXFileTestFile(t *testing.T, filename string) string {
	t.Helper()
	content, err := os.ReadFile(filename)
	assert.Nil(t, err)
	return string(content)
}

// writeXFileTestZipEntry 创建只包含一个受控条目的 zip 文件.
func writeXFileTestZipEntry(t *testing.T, filename, entryName, content string) {
	t.Helper()
	file, err := os.Create(filename)
	assert.Nil(t, err)
	writer := zip.NewWriter(file)
	writerClosed := false
	fileClosed := false
	t.Cleanup(func() {
		if !writerClosed {
			_ = writer.Close()
		}
		if !fileClosed {
			_ = file.Close()
		}
	})
	entry, err := writer.Create(entryName)
	assert.Nil(t, err)
	_, err = entry.Write([]byte(content))
	assert.Nil(t, err)
	err = writer.Close()
	writerClosed = true
	assert.Nil(t, err)
	err = file.Close()
	fileClosed = true
	assert.Nil(t, err)
}
