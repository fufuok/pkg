package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/fufuok/pkg/base58"
	"github.com/joho/godotenv"
)

var (
	extraEnvFiles []string

	// envFileKeys 记录上轮文件管理的 key, 用于将本轮移除的变量置空.
	envFileKeys map[string]struct{}

	// readEnvFile 固定单次加载的内容读取边界, 测试替换时不得并行运行.
	readEnvFile = os.ReadFile
)

// GetEnvFiles 获取已加载的 .env 文件列表
func GetEnvFiles() []string {
	return slices.Clone(extraEnvFiles)
}

// loadEnvFiles 读取并验证全部 .env 快照, 成功后依次覆盖环境并发布跟踪集合.
// 后加载的优先, 已存在的环境变量值会被覆盖.
// 忽略不存在的文件.
//
// 热加载语义: 当变量从 env 文件中被删除或注释掉时, 进程环境中对应的旧值会被置空,
// 使 os.Getenv 返回空字符串. 这解决了 godotenv.Overload 只设置文件中存在的 key,
// 注释/删除的 key 仍保留旧值的问题. 仅影响由 env 文件管理过的 key, 不影响系统级环境变量.
// 文件读取、语法和无效键值错误在修改环境前返回. 环境写入不具备多变量原子性,
// 若操作系统写入失败则返回错误, 不发布新的跟踪集合; 生命周期必须串行调用.
func loadEnvFiles(envFiles ...string) error {
	envFiles = append([]string{EnvMainFile}, envFiles...)
	for i, f := range envFiles {
		if !filepath.IsAbs(f) {
			f = filepath.Join(EnvFilePath, f)
			envFiles[i] = f
		}
	}

	// 每个文件只读取和解析一次, 防止验证后文件变化使应用值与 key 集合不一致.
	// godotenv 的展开仅依赖同一文件已解析的键, 因而可在写环境前保存完整结果.
	values := make([]map[string]string, len(envFiles))
	currentKeys := make(map[string]struct{})
	for i, f := range envFiles {
		body, err := readEnvFile(f)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return fmt.Errorf("read env file %q: %w", f, err)
		}
		m, err := parseEnvSnapshot(f, body)
		if err != nil {
			return err
		}
		for k := range m {
			currentKeys[k] = struct{}{}
		}
		values[i] = m
	}

	// 对比上次记录的 key: 上次有但本次没有的, 说明已从 env 文件中移除,
	// 将进程环境中的旧值置空. envFileKeys 为 nil (首次加载) 时跳过.
	for k := range envFileKeys {
		if _, ok := currentKeys[k]; !ok {
			if err := os.Setenv(k, ""); err != nil {
				return fmt.Errorf("clear removed env key %q: %w", k, err)
			}
		}
	}

	// 只应用已验证的快照, 不再次访问文件; 后文件仍覆盖前文件中的同名键.
	for i, m := range values {
		for k, v := range m {
			if err := os.Setenv(k, v); err != nil {
				return fmt.Errorf("apply env file %q key %q: %w", envFiles[i], k, err)
			}
		}
	}

	envFileKeys = currentKeys
	extraEnvFiles = envFiles
	loadEnvConfig()
	return nil
}

// parseEnvSnapshot 解析单份内容并预检操作系统环境变量约束, 不修改进程状态.
// 语法错误不回显源文本, 避免把未闭合的密钥等值写入诊断日志.
func parseEnvSnapshot(path string, body []byte) (map[string]string, error) {
	m, err := godotenv.UnmarshalBytes(body)
	if err != nil {
		return nil, fmt.Errorf("parse env file %q: invalid environment syntax", path)
	}
	for k, v := range m {
		if k == "" || strings.ContainsAny(k, "=\x00") || strings.IndexByte(v, 0) >= 0 {
			return nil, fmt.Errorf("parse env file %q: invalid environment entry for key %q", path, k)
		}
	}
	return m, nil
}

// 加载环境变量中设定的应用配置
func loadEnvConfig() {
	if s := os.Getenv(BaseSecretKeyNameEnvName); s != "" {
		_ = os.Setenv(BaseSecretKeyNameEnvName, "")
		BaseSecretEnvName = s
	}
	// 盐是 Base58 编码后存入 env, 缺省为固定值 (BaseSecretSalt 变量)
	if s := string(base58.Decode(os.Getenv(BaseSecretSaltEnvName))); s != "" {
		_ = os.Setenv(BaseSecretSaltEnvName, "")
		BaseSecretSalt = s
	}

	if s := os.Getenv(BinNameEnvName); s != "" {
		_ = os.Setenv(BinNameEnvName, "")
		BinName = s
	}
	if s := os.Getenv(AppNameEnvName); s != "" {
		_ = os.Setenv(AppNameEnvName, "")
		AppName = s
	}
	if s := os.Getenv(DebNameEnvName); s != "" {
		_ = os.Setenv(DebNameEnvName, "")
		DebName = s
	}

	// 应用指定的基础密钥解密 KEY 优先, 默认使用: 盐+AppName
	if BaseSecretKeyValue == "" {
		BaseSecretKeyValue = BaseSecretSalt + AppName
	}
}
