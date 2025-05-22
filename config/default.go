package config

import (
	"os"
	"path/filepath"
	"time"

	"github.com/fufuok/utils"
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

	if ConfigFile == "" {
		ConfigFile = filepath.Join(ConfigPath, BinName+".json")
	}

	if EnvFilePath == "" {
		EnvFilePath = filepath.Join(RootPath, "..", "env")
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
