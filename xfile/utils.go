package xfile

import (
	"archive/zip"
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fufuok/pkg/utils"
)

// IsExist 文件或目录是否存在
func IsExist(s string) bool {
	_, err := os.Stat(s)
	return err == nil || os.IsExist(err)
}

// IsFile 文件是否存在
func IsFile(s string) bool {
	info, err := os.Stat(s)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// IsDir 目录是否存在
func IsDir(s string) bool {
	info, err := os.Stat(s)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// ResetDir 清除并重建空目录
func ResetDir(dirPath string) error {
	if _, err := os.Stat(dirPath); !os.IsNotExist(err) {
		if err := os.RemoveAll(dirPath); err != nil {
			return err
		}
	}
	return os.MkdirAll(dirPath, 0o755)
}

// ReadFile reads contents from a file
func ReadFile(filename string) (string, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return "", err
	}
	return utils.B2S(content), nil
}

// ReadLines reads contents from a file and splits them by new lines.
// A convenience wrapper to ReadLinesOffsetN(filename, 0, -1).
func ReadLines(filename string) ([]string, error) {
	return ReadLinesOffsetN(filename, 0, -1)
}

// HeadLines 读取文件头部 N 行
func HeadLines(filename string, num int) ([]string, error) {
	return ReadLinesOffsetN(filename, 0, num)
}

// ReadLinesOffsetN reads contents from file and splits them by new line.
// The offset tells at which line number to start.
// The count determines the number of lines to read (starting from offset):
// n >= 0: at most n lines
// n < 0: whole file
// Ref: gopsutil
//
// 无末尾换行的最后一行与普通行一样受 offset/n 约束 (offset 超过总行数时返回空).
// 只有 EOF 视为正常结束; 其他读取错误 (如目标是目录) 连同已读到的行一起返回, 不伪装成成功.
func ReadLinesOffsetN(filename string, offset uint, n int) ([]string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = f.Close()
	}()

	var ret []string
	r := bufio.NewReader(f)
	for i := 0; i < n+int(offset) || n < 0; i++ {
		line, err := r.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return ret, fmt.Errorf("failed to read lines: %w", err)
		}
		// EOF 且没有剩余文本: 文件已读完.
		if err != nil && len(line) == 0 {
			break
		}
		// 完整行和 EOF 前的末行统一按 offset 跳过, 末行不能绕过 offset 检查.
		if i >= int(offset) {
			ret = append(ret, strings.Trim(line, "\n"))
		}
		if err != nil {
			// EOF 前的末行已处理完毕.
			break
		}
	}
	return ret, nil
}

// TailLines 返回文件最后 N 行
func TailLines(filename string, num int, cleanLine ...bool) ([]string, error) {
	if num == 0 {
		return nil, nil
	}

	if num < 0 {
		return ReadLines(filename)
	}

	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = f.Close()
	}()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	size := info.Size()
	if size == 0 {
		return nil, nil
	}

	clean := false
	if len(cleanLine) > 0 {
		clean = cleanLine[0]
	}

	chunkSize := int64(2048)
	if num > 5 {
		chunkSize = 4096
	}
	if size < chunkSize {
		chunkSize = size
	}

	ret := make([]string, 0, num)
	buf := make([]byte, chunkSize)
	chip := ""
	for offset := -chunkSize; ; offset -= chunkSize {
		if offset < -size {
			buf = buf[:chunkSize-(-size-offset)]
			offset = -size
		}
		if _, err := f.Seek(offset, io.SeekEnd); err != nil {
			break
		}
		n, err := f.Read(buf)
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		lines := strings.Split(utils.B2S(buf[:n])+chip, "\n")
		chip = lines[0]
		if len(lines) > 1 {
			for i := len(lines) - 1; i >= 1; i-- {
				line := lines[i]
				if clean {
					line = strings.TrimRight(line, " \t\r\n")
					if line == "" {
						continue
					}
				}
				ret = append([]string{line}, ret...)
				if len(ret) == num {
					return ret, nil
				}
			}
		}
		if offset <= -size {
			break
		}
	}

	if clean {
		chip = strings.TrimRight(chip, " \t\r\n")
		if chip == "" {
			return ret, nil
		}
	}
	ret = append([]string{chip}, ret...)
	return ret, nil
}

// ModTime 文件最后修改时间
func ModTime(filename string) time.Time {
	info, err := os.Stat(filename)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// CopyDir 目录拷贝
func CopyDir(srcDir, dstDir string) error {
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(dstDir, relPath)
		if info.IsDir() {
			return os.MkdirAll(dstPath, os.ModePerm)
		}
		return CopyFile(path, dstPath)
	})
}

// CopyFile 文件拷贝
func CopyFile(srcFile, dstFile string) error {
	src, err := os.Open(srcFile)
	if err != nil {
		return err
	}
	defer func() {
		_ = src.Close()
	}()

	dst, err := os.Create(dstFile)
	if err != nil {
		return err
	}
	defer func() {
		_ = dst.Close()
	}()

	_, err = io.Copy(dst, src)
	return err
}

// ZipDir 将目录打包为 zip 文件, 条目统一使用正斜杠.
// 遍历、ZIP 收尾和目标文件关闭错误均返回, 失败时可能保留未完成的输出文件.
func ZipDir(srcDir, zipFilePath string) (err error) {
	zipFile, err := os.Create(zipFilePath)
	if err != nil {
		return fmt.Errorf("failed to create zip file: %w", err)
	}
	defer func() {
		if closeErr := zipFile.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("failed to close zip file: %w", closeErr))
		}
	}()

	zipWriter := zip.NewWriter(zipFile)
	defer func() {
		// 收尾写入目录索引可能是首次真正写磁盘, 必须保留错误及此前的失败.
		if closeErr := zipWriter.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("failed to finalize zip file: %w", closeErr))
		}
	}()

	err = filepath.Walk(srcDir, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return fmt.Errorf("failed to access file: %w", err)
		}

		// 获取文件在 zip 中的相对路径
		relPath, err := filepath.Rel(srcDir, filePath)
		if err != nil {
			return fmt.Errorf("failed to get relative path for file: %w", err)
		}

		if relPath == "." {
			return nil
		}
		relPath = filepath.ToSlash(relPath)

		// 创建 zip 文件中的文件或目录
		if info.IsDir() {
			// 添加目录条目
			if _, err := zipWriter.Create(relPath + "/"); err != nil {
				return fmt.Errorf("failed to create directory entry for %s: %w", relPath, err)
			}
			return nil
		}

		// 添加文件条目
		file, err := os.Open(filePath)
		if err != nil {
			return fmt.Errorf("failed to open file: %w", err)
		}
		defer func() {
			_ = file.Close()
		}()

		w, err := zipWriter.Create(relPath)
		if err != nil {
			return fmt.Errorf("failed to create file entry for %s: %w", relPath, err)
		}
		if _, err := io.Copy(w, file); err != nil {
			return fmt.Errorf("failed to copy file: %w", err)
		}
		return nil
	})
	return err
}

// UnzipDir 解压 zip 到目录.
// 目标目录不存在时自动创建. 所有写入经 os.Root 约束在 dstDir 内: 条目名含 ".." 直接拒绝,
// 目标树中已有的符号链接可以指向树内, 但指向树外 (或为绝对路径链接) 时返回错误, 不会写出目标目录.
// 文件条目的父目录按需创建, 合法 ZIP 缺少显式目录条目或文件条目先于目录条目时同样可以解压.
// 遇到错误立即返回, 此前已解压的条目保留.
func UnzipDir(zipFile, dstDir string) error {
	reader, err := zip.OpenReader(zipFile)
	if err != nil {
		return fmt.Errorf("failed to open the zip file: %w", err)
	}
	defer func() {
		_ = reader.Close()
	}()

	// 原实现遇到目录条目时 MkdirAll 会顺带建出 dstDir, 这里统一预先创建以保持兼容.
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	root, err := os.OpenRoot(dstDir)
	if err != nil {
		return fmt.Errorf("failed to open destination directory: %w", err)
	}
	defer func() {
		_ = root.Close()
	}()

	for _, file := range reader.File {
		if strings.Contains(file.Name, "..") {
			return fmt.Errorf("illegal file path in zip: %v", file.Name)
		}

		// 与原 filepath.Join(dstDir, name) 的相对部分一致: 清理多余分隔符, 去掉前导 "/" 并转为本地分隔符.
		name := filepath.Join(".", file.Name)

		if file.FileInfo().IsDir() {
			directoryMode := file.Mode().Perm()
			// zip.Writer.Create 生成的目录条目不携带可遍历的 Unix 权限. Linux
			// 下若使用零权限或无执行位的权限, 后续嵌套文件将无法写入该目录.
			if directoryMode == 0 || directoryMode&0o111 == 0 {
				directoryMode = 0o755
			}
			if err := root.MkdirAll(name, directoryMode); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}
			continue
		}

		if err := unzipFileInRoot(root, file, name); err != nil {
			return err
		}
	}
	return nil
}

// unzipFileInRoot 把单个文件条目写到 root 内的相对路径 name, 缺失的父目录按 0o755 创建.
// 父目录和文件均经 root 解析, 路径中任何指向 root 之外的符号链接都会使操作失败.
func unzipFileInRoot(root *os.Root, zipFile *zip.File, name string) error {
	if dir := filepath.Dir(name); dir != "." {
		if err := root.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}
	}

	src, err := zipFile.Open()
	if err != nil {
		return fmt.Errorf("failed to open zip file: %w", err)
	}
	defer func() {
		_ = src.Close()
	}()

	// os.Root.OpenFile 只接受权限位, 带 setuid/setgid/sticky 或类型位 (如 zip -y 的符号链接条目) 的模式会被拒绝;
	// 旧实现经 os.OpenFile 时类型位被忽略, 符号链接条目写成内容为链接目标的普通文件. 这里只取权限位保持可解压,
	// 不再保留特殊权限位 (解压不可信归档时也不应写出 setuid 文件).
	dst, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, zipFile.Mode().Perm())
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer func() {
		_ = dst.Close()
	}()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("failed to copy file: %w", err)
	}
	return nil
}

// UnzipFile 解压单个文件
func UnzipFile(zipFile *zip.File, dstFile string) error {
	src, err := zipFile.Open()
	if err != nil {
		return fmt.Errorf("failed to open zip file: %w", err)
	}
	defer func() {
		_ = src.Close()
	}()

	dst, err := os.OpenFile(dstFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, zipFile.Mode())
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer func() {
		_ = dst.Close()
	}()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("failed to copy file: %w", err)
	}

	return nil
}
