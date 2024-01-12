package config

import (
	"os"
	"path/filepath"
	"sync"
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
	DebName = "ff-app"
)

var (
	// RootPath 程序运行绝对路径 !!! 决定加载的默认配置目录, 环境文件目录, 日志目录
	RootPath        string
	DefaultRootPath = utils.ExecutableDir(true)

	// LogPath 日志路径
	LogPath   string
	LogFile   string
	LogDaemon string

	// ConfigPath 主配置文件绝对路径, .env 配置文件路径
	ConfigPath  string
	ConfigFile  string
	EnvFilePath string
	EnvMainFile string

	// ReqUserAgent Request 请求名称
	ReqUserAgent string
)

var (
	Debug bool

	// FilesVer 配置文件版本信息
	FilesVer sync.Map

	// LogLevel 日志级别: -1Trace 0Debug 1Info 2Warn(默认) 3Error 4Fatal 5Panic 6NoLevel 7Off
	LogLevel = 2
	// LogSamplePeriodDur 抽样日志设置 (每秒最多 3 个日志)
	LogSamplePeriodDur = time.Second
	LogSampleBurst     = 10
	// LogFileMaxSize 每 100M 自动切割, 保留 30 天内最近 10 个日志文件
	LogFileMaxSize    = 100
	LogFileMaxBackups = 10
	LogFileMaxAge     = 30
	// LogPostIntervalDuration 日志推送时间间隔(秒)
	LogPostIntervalDuration = 1 * time.Second
	// LogPostBatchNum 单次批量提交数据最大条数或最大字节数
	LogPostBatchNum   = 2000
	LogPostBatchBytes = 2 << 20

	// BaseSecretKeyName 项目基础密钥 (环境变量名)
	BaseSecretKeyName = "BASE_SECRET_KEY"
	// BaseSecretKeyNameKeyName 用于在环境变量中指定上一行设置的值的键名, 而不是使用默认的: BASE_SECRET_KEY
	BaseSecretKeyNameKeyName = "BASE_SECRET_KEY_NAME"

	// BaseSecretSalt 用于解密基础密钥值的密钥的前半部分, 盐 (编译在程序中), 后半部分为 AppName 值
	BaseSecretSalt = "Fufu↑777發彡 "
	// BaseSecretSaltKeyName 环境变量中读取上一行设置的值的键名, 而不是使用上一行中设置的固定值
	BaseSecretSaltKeyName = "BASE_SECRET_SALT"

	// WatcherIntervalDuration 文件变化监控时间间隔(分)
	WatcherIntervalDuration = 2 * time.Minute

	// DefaultLoadConfigInterval 通用配置定时加载时间, 各类 Sender 运行中加载最新配置
	DefaultLoadConfigInterval = 1 * time.Minute

	// DefaultRandomWait 执行前随机等待最大秒数
	DefaultRandomWait = 120

	// ReqTimeoutDuration 作为客户端发起请求默认超时时间(秒)
	ReqTimeoutDuration      = 30 * time.Second
	ReqTimeoutShortDuration = 3 * time.Second

	// ChanxInitCap 无限缓冲信道默认初始化缓冲大小
	ChanxInitCap = 50
	// ChanxMaxBufCap 无限缓冲信道最大缓冲数量, 0 为无限, 超过限制(ChanxInitCap + ChanxMaxBufCap)丢弃数据
	ChanxMaxBufCap = 100000

	// ServiceNameSuffix systemctl 服务名后缀 (Ubuntu)
	ServiceNameSuffix = ".service"
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
func initDefaultConfig() {
	if RootPath == "" {
		RootPath = DefaultRootPath
	}

	if LogPath == "" {
		LogPath = filepath.Join(RootPath, "..", "log")
	}

	if LogFile == "" {
		LogFile = filepath.Join(LogPath, BinName+".log")
	}

	if LogDaemon == "" {
		LogDaemon = filepath.Join(LogPath, "daemon.log")
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

	if ReqUserAgent == "" {
		ReqUserAgent = AppName + "/" + Version
	}

	makePaths()
}

func makePaths() {
	_ = os.MkdirAll(LogPath, 0755)
	_ = os.MkdirAll(ConfigPath, 0755)
}
