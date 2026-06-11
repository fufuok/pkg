package config

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fufuok/utils"
	"github.com/joho/godotenv"
)

var (
	// BinName !!! 这个变量决定启动时加载的默认配置名, 日志名等
	// 可以由 LDFLAGS 编译时注入
	// 可以在应用程序 func init() { BinName = "yourapp" ... }
	// 也可启动程序时命令行参数指定主配置文件
	BinName = "ffapp"

	AppName = "FF.App"

	// DebName !!! 这个变量决定自升级时安装命令, 以及日志中显示的包名称
	// 可以在应用程序 func init() { DebName = "your-app" ... }
	DebName = "ff-app"

	// BinNameEnvName 上面 3 个变量可以在环境变量中设置(优先使用): LOCAL_DEB_NAME=your-web-app
	BinNameEnvName = "LOCAL_BIN_NAME"
	AppNameEnvName = "LOCAL_APP_NAME"
	DebNameEnvName = "LOCAL_DEB_NAME"

	// AppBaseSecretValue APP 设置的基础密钥值(可选, 优先使用)
	AppBaseSecretValue string
	// AppConfigBody APP 设置的固定配置 JSON 字符串, 替代配置文件 (可选, 优先使用)
	AppConfigBody []byte

	// MainConfRemoteAPIEnvName 获取主配置文件的 API URL (可选, 优先使用)
	MainConfRemoteAPIEnvName = "MAIN_CONF_REMOTE_API"
	// MainConfigFileEnvName 启动级主配置文件路径环境变量名, 绝对路径直接使用, 相对路径基于 ConfigPath
	MainConfigFileEnvName = "MAIN_CONFIG_FILE"
	// MainConfigNameEnvName 启动级主配置名称环境变量名, 仅作为 ConfigPath 下的 JSON 文件名使用
	MainConfigNameEnvName = "MAIN_CONFIG_NAME"
	// BootstrapEnvSuffix 启动级环境文件后缀, 完整文件名为 env/{BinName}.default.env
	BootstrapEnvSuffix = ".default.env"

	// ConfigInitialized 全局配置是否已初始化
	ConfigInitialized bool
)

var (
	// RootPath 程序运行绝对路径 !!! 决定加载的默认配置目录, 环境文件目录, 日志目录(必须存在)
	RootPath        string
	DefaultRootPath = utils.ExecutableDir(true)
	DefaultLogPath  = filepath.Join(DefaultRootPath, "..", "log")

	// LogDaemon 守护进程日志, 路径为空时不记录日志
	LogDaemon = filepath.Join(DefaultLogPath, "daemon.log")

	// LogPath 日志路径
	LogPath string
	LogFile string

	// ConfigPath 主配置文件绝对路径, .env 配置文件路径
	ConfigPath  string
	ConfigFile  string
	EnvFilePath string
	EnvMainFile string

	// WhitelistConfigFile IP 白名单配置文件路径
	WhitelistConfigFile        string
	DefaultWhitelistConfigFile string
	// BlacklistConfigFile IP 黑名单配置文件路径
	BlacklistConfigFile        string
	DefaultBlacklistConfigFile string

	// ReqUserAgent Request 请求名称
	ReqUserAgent string
)

var (
	Debug bool

	// LogLevel 日志级别: -1Trace 0Debug 1Info 2Warn(默认) 3Error 4Fatal 5Panic 6NoLevel 7Off
	LogLevel = 2
	// LogSamplePeriodDur 抽样日志设置 (每秒最多 10 个日志)
	LogSamplePeriodDur = time.Second
	LogSampleBurst     = 10
	// LogFileMaxSize 每 100M 自动切割, 保留 30 天内最近 10 个日志文件
	LogFileMaxSize    = 100
	LogFileMaxBackups = 10
	LogFileMaxAge     = 30
	// LogPostIntervalDuration 日志推送时间间隔(秒)
	LogPostIntervalDuration = 2 * time.Second
	// LogPostBatchNum 单次批量提交数据最大条数或最大字节数
	LogPostBatchNum   = 2000
	LogPostBatchBytes = 2 << 20

	// BaseSecretValue 项目基础密钥值(从环境变量解码), 与 config.Config().SYSConf.BaseSecretValue 相同
	BaseSecretValue string
	// BaseSecretEnvName 项目基础密钥 (环境变量名)
	BaseSecretEnvName = "BASE_SECRET_KEY"
	// BaseSecretKeyNameEnvName 用于在环境变量中指定上一行设置的值的键名, 而不是使用默认的: BASE_SECRET_KEY
	BaseSecretKeyNameEnvName = "BASE_SECRET_KEY_NAME"

	// BaseSecretSalt 用于解密基础密钥值的密钥的前半部分, 盐 (编译在程序中), 后半部分为 AppName 值
	BaseSecretSalt = "Fufu↑777發彡 " // #nosec G101
	// BaseSecretSaltEnvName 环境变量中读取上一行设置的值的键名, 而不是使用上一行中设置的固定值
	BaseSecretSaltEnvName = "BASE_SECRET_SALT"

	// BaseSecretKeyValue 用于解密基础密钥的完整密钥, 默认为: 上面的盐 + AppName 值
	// 可由应用自行指定该值以跳过上面的规则, 如在 main.go 中: config.BaseSecretKeyValue = "我的基础密钥解密KEY值"
	BaseSecretKeyValue string

	// WatcherIntervalDuration 文件变化监控时间间隔
	WatcherIntervalDuration = 2 * time.Minute

	// DefaultLoadConfigInterval 通用配置定时加载时间, 各类 Sender 运行中加载最新配置
	DefaultLoadConfigInterval = 1 * time.Minute

	// DefaultRandomWait 执行前随机等待最大秒数
	DefaultRandomWait = 120

	// ReqTimeoutDuration 作为客户端发起请求默认超时时间
	ReqTimeoutDuration      = 30 * time.Second
	ReqTimeoutShortDuration = 3 * time.Second

	// ChanxInitCap 无限缓冲信道默认初始化缓冲大小
	ChanxInitCap = 50
	// ChanxMaxBufCap 无限缓冲信道最大缓冲数量, 0 为无限, 超过限制(ChanxInitCap + ChanxMaxBufCap)丢弃数据
	ChanxMaxBufCap = 500000

	// ServiceNameSuffix systemctl 服务名后缀 (Ubuntu)
	ServiceNameSuffix = ".service"

	// AlarmDisabledValue 禁用报警标识
	AlarmDisabledValue = "--disabled"
)

var (
	// WebServerAddr 缺省的 HTTP 接口端口
	WebServerAddr = ":12366"
	// WebServerHttpsAddr 缺省的 HTTPS 接口端口
	WebServerHttpsAddr = ":12377"

	// WebCertFileEnvName 默认证书路径环境变量名
	WebCertFileEnvName = "WEB_CERT_FILE"
	WebKeyFileEnvName  = "WEB_KEY_FILE"

	// WebSignKeyEnvName 接口签名密钥加密串环境变量名
	WebSignKeyEnvName = "WEB_SIGN_KEY"
	// WebSignTTLDefault 接口签名有效生命周期(秒数, 默认: 60, 最小 5)
	WebSignTTLDefault = 60
	WebSignTTLMin     = 5

	// WebLogInfoKey 接口日志附带请求信息, 上下文键名
	WebLogInfoKey = "_WLIK_"

	// WebLogSlowResponse 慢日志条件
	WebLogSlowResponse = 5 * time.Second

	// WebLogMinStatusCode Web 日志响应码条件值
	WebLogMinStatusCode = 500

	// WebTimeout Web 请求超时
	WebTimeout = 30 * time.Second

	// WebBodyLimit POST 最大 8M, 超过该值影响: 413 Request Entity Too Large
	WebBodyLimit = 8 << 20

	// WebTokenSalt 用于加密代理客户端请求 IP 的盐, 默认为 BaseSecretValue 项目基础密钥值
	WebTokenSalt string
)

// 初始化缺省变量
// .
// ├── bin
// │   └── ffapp
// ├── env
// │   └── ffapp.env
// ├── etc
// │   └── ffapp.json
// ├── log
// │   ├── daemon.log
// │   └── ffapp.log
//
//nolint:cyclop
func initDefaultConfig() {
	if RootPath == "" {
		RootPath = DefaultRootPath
	} else {
		// 指定程序根路径时, 修正缺省日志路径
		DefaultLogPath = filepath.Join(RootPath, "..", "log")
	}

	if LogPath == "" {
		LogPath = DefaultLogPath
	}

	if LogFile == "" {
		LogFile = filepath.Join(LogPath, BinName+".log")
	}

	if ConfigPath == "" {
		ConfigPath = filepath.Join(RootPath, "..", "etc")
	}

	if EnvFilePath == "" {
		EnvFilePath = filepath.Join(RootPath, "..", "env")
	}

	loadBootstrapEnv()

	if ConfigFile == "" {
		ConfigFile = resolveMainConfigFile(ConfigPath, BinName)
	}

	if EnvMainFile == "" {
		EnvMainFile = filepath.Join(EnvFilePath, BinName+".env")
	}

	if NodeInfoBackupFile == "" {
		NodeInfoBackupFile = filepath.Join(ConfigPath, "node_info.backup")
	}

	if ReqUserAgent == "" {
		ReqUserAgent = AppName + "/" + Version
	}

	if DefaultWhitelistConfigFile == "" {
		DefaultWhitelistConfigFile = filepath.Join(ConfigPath, BinName+".whitelist.conf")
	}

	if DefaultBlacklistConfigFile == "" {
		DefaultBlacklistConfigFile = filepath.Join(ConfigPath, BinName+".blacklist.conf")
	}

	makePaths()
}

func makePaths() {
	_ = os.MkdirAll(LogPath, 0o755)
	_ = os.MkdirAll(ConfigPath, 0o755)
}

// loadBootstrapEnv 加载启动级环境文件 env/{BinName}.default.env.
//
// 该文件只用于主配置读取前的路径派生, 不是运行期热加载配置. 这里使用
// godotenv.Load 而不是 loadEnvFiles 中的 godotenv.Overload, 是为了把该文件
// 定义成“可被机器环境变量覆盖的默认值”. 这样 systemd Environment, /etc/default,
// 容器 env 或 shell env 中的同名变量能保持最高机器级优先级.
func loadBootstrapEnv() {
	_ = godotenv.Load(filepath.Join(EnvFilePath, BinName+BootstrapEnvSuffix))
}

// ResolveDefaultConfigFile 解析可由环境变量显式指定的默认配置文件路径.
//
// envName 指向的环境变量表示完整文件路径: 绝对路径直接使用, 相对路径基于
// ConfigPath. 当环境变量未设置或只包含空白时, 回退到 {ConfigPath}/{BinName}{suffix}.
// 该函数只做路径解析, 不检查文件是否存在, 让调用方在真正读取配置时暴露部署错误.
func ResolveDefaultConfigFile(envName, suffix string) string {
	return resolveDefaultConfigFile(ConfigPath, BinName, envName, suffix)
}

// resolveDefaultConfigFile 是 ResolveDefaultConfigFile 的可测实现.
//
// 通过显式传入 configPath 和 binName, 主配置初始化和单元测试都能复用同一套路径规则,
// 同时避免为了测试或主配置解析临时改写包级全局变量.
func resolveDefaultConfigFile(configPath, binName, envName, suffix string) string {
	if v := strings.TrimSpace(os.Getenv(envName)); v != "" {
		if filepath.IsAbs(v) {
			return v
		}
		return filepath.Join(configPath, v)
	}
	return filepath.Join(configPath, binName+suffix)
}

// ResolveDefaultConfigName 解析可由环境变量显式指定的默认配置名称.
//
// envName 指向的环境变量只表示配置名, 不表示路径. 解析时会裁剪空白, 取
// filepath.Base 并自动补齐 suffix, 避免通过目录分隔符逃逸 ConfigPath. 当环境变量
// 未设置或只包含空白时, 回退到 {ConfigPath}/{BinName}{suffix}.
func ResolveDefaultConfigName(envName, suffix string) string {
	return resolveDefaultConfigName(ConfigPath, BinName, envName, suffix)
}

// resolveDefaultConfigName 是 ResolveDefaultConfigName 的可测实现.
//
// 配置名与配置路径的语义不同: 名称只能落在 configPath 下, 因此这里会取 base 名并
// 补齐 suffix; 路径型环境变量请使用 resolveDefaultConfigFile.
func resolveDefaultConfigName(configPath, binName, envName, suffix string) string {
	if v := strings.TrimSpace(os.Getenv(envName)); v != "" {
		v = filepath.Base(v)
		if !strings.HasSuffix(strings.ToLower(v), strings.ToLower(suffix)) {
			v += suffix
		}
		return filepath.Join(configPath, v)
	}
	return filepath.Join(configPath, binName+suffix)
}

// resolveMainConfigFile 按启动级优先级解析主配置文件路径.
//
// 优先级固定为 MAIN_CONFIG_FILE > MAIN_CONFIG_NAME > {ConfigPath}/{BinName}.json.
// 显式 ConfigFile 的最高优先级由调用方通过“仅在 ConfigFile 为空时调用本函数”保证.
func resolveMainConfigFile(configPath, binName string) string {
	if v := strings.TrimSpace(os.Getenv(MainConfigFileEnvName)); v != "" {
		return resolveDefaultConfigFile(configPath, binName, MainConfigFileEnvName, ".json")
	}

	if v := strings.TrimSpace(os.Getenv(MainConfigNameEnvName)); v != "" {
		return resolveDefaultConfigName(configPath, binName, MainConfigNameEnvName, ".json")
	}

	return resolveDefaultConfigFile(configPath, binName, "", ".json")
}
