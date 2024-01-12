package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/fufuok/utils/xcrypto"
	"github.com/fufuok/utils/xhash"

	"github.com/fufuok/pkg/json"
)

var (
	// AlarmOn 全局报警开关 (区别是否被排除报警, 如测试节点)
	AlarmOn = true

	// 全局配置项
	mainConf atomic.Pointer[MainConf]
)

// MainConf 接口配置
type MainConf struct {
	SYSConf  SYSConf   `json:"sys_conf"`
	MainConf FilesConf `json:"main_conf"`
	LogConf  LogConf   `json:"log_conf"`
	NodeConf NodeConf  `json:"node_conf"`
}

// SYSConf 主配置, 变量意义见配置文件中的描述及 default.go 中的默认值
type SYSConf struct {
	RestartMain             bool     `json:"restart_main"`
	WatcherInterval         int      `json:"watcher_interval"`
	ReqTimeout              int      `json:"req_timeout"`
	ReqMaxRetries           int      `json:"req_max_retries"`
	DebVersion              string   `json:"deb_version"`
	CanaryDeployment        uint64   `json:"canary_deployment"`
	SkipRemoteConfig        string   `json:"skip_remote_config"`
	EnvFiles                []string `json:"env_files"`
	BaseSecretValue         string   `json:"-"`
	WatcherIntervalDuration time.Duration
	ReqTimeoutDuration      time.Duration
}

type LogConf struct {
	NoColor              bool   `json:"no_color"`
	Level                int    `json:"level"`
	File                 string `json:"file"`
	Period               int    `json:"period"`
	Burst                uint32 `json:"burst"`
	MaxSize              int64  `json:"max_size"`
	MaxBackups           int    `json:"max_backups"`
	MaxAge               int    `json:"max_age"`
	PostAPI              string `json:"post_api"`
	PostAPIEnv           string `json:"post_api_env"`
	PostAlarmAPI         string `json:"post_alarm_api"`
	PostAlarmAPIEnv      string `json:"post_alarm_api_env"`
	AlarmCode            string `json:"alarm_code"`
	AlarmCodeEnv         string `json:"alarm_code_env"`
	PostInterval         int    `json:"post_interval"`
	PostBatchNum         int    `json:"post_batch_num"`
	PostBatchMB          int    `json:"post_batch_mb"`
	PeriodDur            time.Duration
	PostIntervalDuration time.Duration
	PostBatchBytes       int
}

type FilesConf struct {
	Path            string `json:"-"`
	Method          string `json:"method"`
	SecretName      string `json:"secret_name"`
	API             string `json:"api"`
	Interval        int    `json:"interval"`
	RandomWait      int    `json:"random_wait"`
	SecretValue     string `json:"-"`
	GetConfDuration time.Duration
}

// LoadConf 加载配置
func LoadConf() error {
	cfg, err := readConf()
	if err != nil {
		return err
	}

	mainConf.Store(cfg)

	return nil
}

// 读取配置
func readConf() (*MainConf, error) {
	body, err := os.ReadFile(ConfigFile)
	if err != nil {
		return nil, err
	}

	cfg := new(MainConf)
	if err := json.Unmarshal(body, cfg); err != nil {
		return nil, err
	}

	_ = loadEnvFiles(cfg.SYSConf.EnvFiles...)

	// 基础密钥: 由程序固化的密钥解密环境变量得到, 其他加密变量都使用基础密码加密
	cfg.SYSConf.BaseSecretValue = xcrypto.GetenvDecrypt(BaseSecretKeyName, BaseSecretSalt+AppName)
	if cfg.SYSConf.BaseSecretValue == "" {
		return nil, fmt.Errorf("%s cannot be empty", BaseSecretKeyName)
	}

	// 包版本格式清理
	cfg.SYSConf.DebVersion = regexp.MustCompile(`[^\w-.=]`).ReplaceAllString(cfg.SYSConf.DebVersion, "")

	// 日志级别: -1Trace 0Debug 1Info 2Warn(默认) 3Error 4Fatal 5Panic 6NoLevel 7Off
	if cfg.LogConf.Level > 7 || cfg.LogConf.Level < -1 {
		cfg.LogConf.Level = LogLevel
	}
	// 调试模式 Debug 日志
	if Debug {
		cfg.LogConf.Level = 0
	}

	// 抽样日志设置 (x 秒 n 条)
	if cfg.LogConf.Period < 0 {
		cfg.LogConf.PeriodDur = LogSamplePeriodDur
		cfg.LogConf.Burst = uint32(LogSampleBurst)
	} else {
		cfg.LogConf.PeriodDur = time.Duration(cfg.LogConf.Period) * time.Second
	}

	// 日志推送到接口时间间隔
	if cfg.LogConf.PostInterval > 0 {
		cfg.LogConf.PostIntervalDuration = time.Duration(cfg.LogConf.PostInterval) * time.Second
	} else {
		cfg.LogConf.PostIntervalDuration = LogPostIntervalDuration
	}
	if cfg.LogConf.PostBatchNum < 1 {
		cfg.LogConf.PostBatchNum = LogPostBatchNum
	}
	if cfg.LogConf.PostBatchMB < 1 {
		cfg.LogConf.PostBatchBytes = LogPostBatchBytes
	} else {
		// 配置文件单位: MB
		cfg.LogConf.PostBatchBytes = cfg.LogConf.PostBatchMB << 20
	}

	// 日志文件
	if cfg.LogConf.File == "" {
		cfg.LogConf.File = LogFile
	}

	// 日志大小和保存设置
	if cfg.LogConf.MaxSize < 1 {
		cfg.LogConf.MaxSize = int64(LogFileMaxSize)
	}
	if cfg.LogConf.MaxBackups < 1 {
		cfg.LogConf.MaxBackups = LogFileMaxBackups
	}
	if cfg.LogConf.MaxAge < 1 {
		cfg.LogConf.MaxAge = LogFileMaxAge
	}

	// 优先使用环境变量中设置的报警 API 和 Code
	cfg.LogConf.PostAPI = strings.TrimSpace(cfg.LogConf.PostAPI)
	cfg.LogConf.PostAlarmAPI = strings.TrimSpace(cfg.LogConf.PostAlarmAPI)
	cfg.LogConf.AlarmCode = strings.TrimSpace(cfg.LogConf.AlarmCode)
	if key := strings.TrimSpace(cfg.LogConf.PostAPIEnv); key != "" {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			_ = os.Setenv(key, "")
			cfg.LogConf.PostAPI = v
		}
	}
	if key := strings.TrimSpace(cfg.LogConf.PostAlarmAPIEnv); key != "" {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			_ = os.Setenv(key, "")
			cfg.LogConf.PostAlarmAPI = v
		}
	}
	if key := strings.TrimSpace(cfg.LogConf.AlarmCodeEnv); key != "" {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			_ = os.Setenv(key, "")
			cfg.LogConf.AlarmCode = v
		}
	}

	AlarmOn = cfg.LogConf.PostAlarmAPI != "" && cfg.LogConf.AlarmCode != ""

	// 每次获取远程主配置的时间间隔, < 30 秒则禁用该功能
	if cfg.MainConf.Interval >= 30 {
		// 远程获取主配置 API, 解密 SecretName
		if cfg.MainConf.SecretName != "" {
			cfg.MainConf.SecretValue = xcrypto.GetenvDecrypt(cfg.MainConf.SecretName,
				cfg.SYSConf.BaseSecretValue)
			if cfg.MainConf.SecretValue == "" {
				return nil, fmt.Errorf("%s cannot be empty", cfg.MainConf.SecretName)
			}
		}
		cfg.MainConf.GetConfDuration = time.Duration(cfg.MainConf.Interval) * time.Second
	}
	// 配置拉取执行前最大随机秒数
	if cfg.MainConf.RandomWait <= 0 {
		cfg.MainConf.RandomWait = DefaultRandomWait
	}
	cfg.MainConf.Path = ConfigFile

	// 配置文件变化监控时间间隔
	if cfg.SYSConf.WatcherInterval < 1 {
		cfg.SYSConf.WatcherIntervalDuration = WatcherIntervalDuration
	} else {
		cfg.SYSConf.WatcherIntervalDuration = time.Duration(cfg.SYSConf.WatcherInterval) * time.Minute
	}

	// 作为客户端发起请求默认超时时间
	if cfg.SYSConf.ReqTimeout < 1 {
		cfg.SYSConf.ReqTimeoutDuration = ReqTimeoutDuration
	} else {
		cfg.SYSConf.ReqTimeoutDuration = time.Duration(cfg.SYSConf.ReqTimeout) * time.Second
	}

	if err = setNodeInfo(&cfg.NodeConf); err != nil {
		return nil, err
	}

	ver := GetFileVer(ConfigFile)
	ver.MD5 = xhash.MD5BytesHex(body)
	ver.LastUpdate = time.Now()
	if Debug {
		fmt.Printf("\n\n%s\n\n", json.MustJSONIndent(cfg))
	}
	return cfg, nil
}
