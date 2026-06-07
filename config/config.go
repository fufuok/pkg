package config

import (
	"fmt"
	"net"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/fufuok/utils/conv"
	"github.com/fufuok/utils/xcrypto"
	"github.com/fufuok/utils/xfile"

	"github.com/fufuok/pkg/json"
)

var (
	// AlarmOn 全局报警开关 (区别是否被排除报警, 如测试节点)
	AlarmOn atomic.Bool

	// Whitelist 接口 IP 白名单配置
	Whitelist map[*net.IPNet]int64

	// Blacklist 接口 IP 黑名单配置
	Blacklist map[*net.IPNet]int64

	// 全局配置项
	mainConf atomic.Pointer[MainConf]
)

// MainConf 接口配置
type MainConf struct {
	SYSConf       SYSConf   `json:"sys_conf"`
	MainConf      FilesConf `json:"main_conf"`
	LogConf       LogConf   `json:"log_conf"`
	NodeConf      NodeConf  `json:"node_conf"`
	WebConf       WebConf   `json:"web_conf"`
	Whitelist     []string  `json:"whitelist"`
	Blacklist     []string  `json:"blacklist"`
	WhitelistConf FilesConf `json:"whitelist_conf"`
	BlacklistConf FilesConf `json:"blacklist_conf"`
}

// SYSConf 主配置, 变量意义见配置文件中的描述及 default.go 中的默认值
type SYSConf struct {
	RestartMain             bool     `json:"restart_main"`
	TimeSyncType            string   `json:"time_sync_type"`
	WatcherInterval         string   `json:"watcher_interval"`
	ReqDebug                bool     `json:"req_debug"`
	ReqTimeout              string   `json:"req_timeout"`
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
	NoPretty             bool   `json:"no_pretty"`
	Level                int    `json:"level"`
	File                 string `json:"file"`
	Period               uint32 `json:"period"`
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
	PeriodDuration       time.Duration
	PostIntervalDuration time.Duration
	PostBatchBytes       int
}

type WebConf struct {
	// Name 可选服务标识, 多实例时用于日志区分, 如 "api" / "admin"
	Name            string `json:"name"`
	PProfAddr       string `json:"pprof_addr"`
	ServerAddr      string `json:"server_addr"`
	ServerHttpsAddr string `json:"server_https_addr"`
	// Groups 是同一进程内多组 Web 服务监听配置. 每组会复用 WebConf 中的通用默认项,
	// 但可拥有独立 name/server_addr/server_https_addr 和路由注册函数.
	Groups         map[string]WebConf `json:"groups"`
	StatsPath      string             `json:"stats_path"`
	TrustedProxies []string           `json:"trusted_proxies"`

	// Gin
	TrustedPlatform string `json:"trusted_platform"`

	// Fiber
	EnableTrustedProxyCheck bool   `json:"enable_trusted_proxy_check"`
	ProxyHeader             string `json:"proxy_header"`

	// Fiber 默认不减少内存占用, 这里改为默认减少内存占用(可能增加 CPU 占用)
	DisableReduceMemoryUsage bool `json:"disable_reduce_memory_usage"`

	// Fiber 短连接模式
	DisableKeepalive bool `json:"disable_keepalive"`

	// Fiber 请求体大小限制, 0 为默认: 8 * 1024 * 1024, -1 表示不限制
	BodyLimit int `json:"body_limit"`

	// 黑白名单中间件缓存容量配置, 键生命周期秒数
	WhitelistLRUCapacity uint32 `json:"whitelist_lru_capacity"`
	WhitelistLRULifetime uint32 `json:"whitelist_lru_lifetime"`
	BlacklistLRUCapacity uint32 `json:"blacklist_lru_capacity"`
	BlacklistLRULifetime uint32 `json:"blacklist_lru_lifetime"`

	// 同时处理的请求数限制, 调用该中间件时有效 (以 处理中 的请求为依据, -1 表示不限制)
	RequestsLimit int32 `json:"requests_limit"`

	// 接口签名密钥生命周期和签名密钥值
	SignTTL int64  `json:"sign_ttl"`
	SignKey string `json:"-"`

	// CertFileEnv/KeyFileEnv 为分组级独立 TLS 证书的环境变量名(可选).
	// 分组同时指定二者时, 使用对应环境变量指向的证书覆盖继承自主配置的证书;
	// 为空时该分组继承主配置(WEB_CERT_FILE/WEB_KEY_FILE)的证书.
	CertFileEnv string `json:"cert_file_env"`
	KeyFileEnv  string `json:"key_file_env"`

	CertFile string `json:"-"`
	KeyFile  string `json:"-"`
}

// NormalizeWebConf 将传入的 WebConf 补齐为可启动配置.
// base 提供框架全局默认值和运行期派生字段; override 非零值覆盖 base.
// 该函数用于同一进程内多组 Web 服务, 既保持旧 web_conf 单实例兼容,
// 又允许应用按业务边界派生独立监听端口和服务名.
func NormalizeWebConf(base, override WebConf) WebConf {
	cfg := base
	if override.Name != "" {
		cfg.Name = override.Name
	}
	if override.PProfAddr != "" {
		cfg.PProfAddr = override.PProfAddr
	}
	if override.ServerAddr != "" {
		cfg.ServerAddr = override.ServerAddr
	}
	if override.ServerHttpsAddr != "" {
		cfg.ServerHttpsAddr = override.ServerHttpsAddr
	}
	if override.Groups != nil {
		cfg.Groups = override.Groups
	}
	if override.StatsPath != "" {
		cfg.StatsPath = override.StatsPath
	}
	if override.TrustedProxies != nil {
		cfg.TrustedProxies = override.TrustedProxies
	}
	if override.TrustedPlatform != "" {
		cfg.TrustedPlatform = override.TrustedPlatform
	}
	if override.EnableTrustedProxyCheck {
		cfg.EnableTrustedProxyCheck = true
	}
	if override.ProxyHeader != "" {
		cfg.ProxyHeader = override.ProxyHeader
	}
	if override.DisableReduceMemoryUsage {
		cfg.DisableReduceMemoryUsage = true
	}
	if override.DisableKeepalive {
		cfg.DisableKeepalive = true
	}
	if override.BodyLimit != 0 {
		cfg.BodyLimit = override.BodyLimit
	}
	if override.WhitelistLRUCapacity != 0 {
		cfg.WhitelistLRUCapacity = override.WhitelistLRUCapacity
	}
	if override.WhitelistLRULifetime != 0 {
		cfg.WhitelistLRULifetime = override.WhitelistLRULifetime
	}
	if override.BlacklistLRUCapacity != 0 {
		cfg.BlacklistLRUCapacity = override.BlacklistLRUCapacity
	}
	if override.BlacklistLRULifetime != 0 {
		cfg.BlacklistLRULifetime = override.BlacklistLRULifetime
	}
	if override.RequestsLimit != 0 {
		cfg.RequestsLimit = override.RequestsLimit
	}
	if override.SignTTL != 0 {
		cfg.SignTTL = override.SignTTL
	}
	if override.SignKey != "" {
		cfg.SignKey = override.SignKey
	}
	if override.CertFileEnv != "" {
		cfg.CertFileEnv = override.CertFileEnv
	}
	if override.KeyFileEnv != "" {
		cfg.KeyFileEnv = override.KeyFileEnv
	}
	if override.CertFile != "" {
		cfg.CertFile = override.CertFile
	}
	if override.KeyFile != "" {
		cfg.KeyFile = override.KeyFile
	}
	return cfg
}

// NormalizeWebGroupConf 将 web_conf.groups.<name> 归一为一组独立 Web 服务配置.
// 与 NormalizeWebConf 不同, 分组配置不会从 base 继承 server_addr/server_https_addr,
// 避免多个业务组在未显式配置端口时重复监听主服务端口; 其他通用项仍继承 base.
func NormalizeWebGroupConf(base WebConf, name string, group WebConf) WebConf {
	cfg := NormalizeWebConf(base, group)
	cfg.Groups = nil
	cfg.ServerAddr = group.ServerAddr
	cfg.ServerHttpsAddr = group.ServerHttpsAddr
	if cfg.Name == "" {
		cfg.Name = name
	}
	return cfg
}

type FilesConf struct {
	Path            string `json:"path"`
	Method          string `json:"method"`
	SecretName      string `json:"secret_name"`
	API             string `json:"api"`
	Interval        int    `json:"interval"`
	RandomWait      int    `json:"random_wait"`
	SecretValue     string `json:"-"`
	GetConfDuration time.Duration
}

// LoadConfig 加载配置
func LoadConfig() error {
	cfg, err := readConfig()
	if err != nil {
		return err
	}

	mainConf.Store(cfg)

	return nil
}

// 从主配置文件读取配置
func readConfig() (*MainConf, error) {
	body := AppConfigBody
	if len(body) == 0 {
		var err error
		body, err = os.ReadFile(ConfigFile)
		if err != nil {
			return nil, err
		}
	}

	cfg := new(MainConf)
	if err := json.Unmarshal(body, cfg); err != nil {
		return nil, err
	}

	loadEnvFiles(cfg.SYSConf.EnvFiles...)

	if err := parseSYSConfig(cfg); err != nil {
		return nil, err
	}

	parseLogConfig(cfg)
	parseAlarmOnConfig(cfg)

	if err := parseMainRemoteConfig(cfg); err != nil {
		return nil, err
	}

	parseNodeInfoConfig(cfg)
	parseWebConfig(cfg)

	if err := parseWhitelistConfig(cfg); err != nil {
		return nil, err
	}

	if err := parseBlacklistConfig(cfg); err != nil {
		return nil, err
	}

	if Debug {
		fmt.Printf("\n\n%s\n\n", json.MustJSONUnEscapeIndentString(cfg))
		fmt.Printf("\nWhitelist:\n%v\n\n", Whitelist)
		fmt.Printf("\nBlacklist:\n%v\n\n", Blacklist)
	}
	return cfg, nil
}

func parseSYSConfig(cfg *MainConf) error {
	if AppBaseSecretValue != "" {
		cfg.SYSConf.BaseSecretValue = AppBaseSecretValue
	} else {
		// 基础密钥: 由程序固化的密钥解密环境变量得到, 其他加密变量都使用基础密码加密
		cfg.SYSConf.BaseSecretValue = xcrypto.GetenvDecrypt(BaseSecretEnvName, BaseSecretKeyValue)
		if cfg.SYSConf.BaseSecretValue == "" {
			return fmt.Errorf("%s cannot be empty", BaseSecretEnvName)
		}
	}
	// 赋值到全局变量
	BaseSecretValue = cfg.SYSConf.BaseSecretValue
	WebTokenSalt = cfg.SYSConf.BaseSecretValue

	// 包版本格式清理
	cfg.SYSConf.DebVersion = regexp.MustCompile(`[^\w-.=]`).ReplaceAllString(cfg.SYSConf.DebVersion, "")

	// 配置文件变化监控时间间隔, 空为默认值
	dur, err := ParseDuration(cfg.SYSConf.WatcherInterval, WatcherIntervalDuration, 30*time.Second)
	if err != nil {
		return fmt.Errorf("parse watcher_interval err: %w", err)
	}
	cfg.SYSConf.WatcherIntervalDuration = dur
	cfg.SYSConf.WatcherInterval = dur.String()

	// 作为客户端发起请求默认超时时间, 空为默认值
	dur, err = ParseDuration(cfg.SYSConf.ReqTimeout, ReqTimeoutDuration, 1*time.Second)
	if err != nil {
		return fmt.Errorf("parse req_timeout err: %w", err)
	}
	cfg.SYSConf.ReqTimeoutDuration = dur
	cfg.SYSConf.ReqTimeout = dur.String()
	return nil
}

// TODO: 对外使用日志配置解析器
//
//nolint:cyclop
func parseLogConfig(cfg *MainConf) {
	// 日志级别: -1Trace 0Debug(0 或未指定该配置项) 1Info 2Warn(默认) 3Error 4Fatal 5Panic 6NoLevel 7Off
	if cfg.LogConf.Level > 7 || cfg.LogConf.Level < -1 {
		cfg.LogConf.Level = LogLevel
	}

	// 调试模式 Debug 日志
	if Debug {
		cfg.LogConf.Level = 0
	}

	// 抽样日志设置 (x 秒 n 条)
	if cfg.LogConf.Period == 0 && cfg.LogConf.Burst == 0 {
		cfg.LogConf.PeriodDuration = LogSamplePeriodDur
		cfg.LogConf.Burst = uint32(LogSampleBurst)
	} else {
		cfg.LogConf.PeriodDuration = time.Duration(cfg.LogConf.Period) * time.Second
	}

	// 日志推送到接口时间间隔 (秒)
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
}

func parseAlarmOnConfig(cfg *MainConf) {
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

	if cfg.LogConf.AlarmCode == AlarmDisabledValue {
		cfg.LogConf.AlarmCode = ""
	}

	AlarmOn.Store(cfg.LogConf.PostAlarmAPI != "" && cfg.LogConf.AlarmCode != "")
}

func parseMainRemoteConfig(cfg *MainConf) error {
	if err := ParseRemoteFileConfig(&cfg.MainConf, cfg.SYSConf.BaseSecretValue); err != nil {
		return err
	}
	// 忽略配置文件中指定的主配置文件路径, 由命令行参数或 BinName 确定, 或由应用端 init 指定
	cfg.MainConf.Path = ConfigFile
	// 优先使用环境变量中指定的主配置文件获取 API
	if api := os.Getenv(MainConfRemoteAPIEnvName); api != "" {
		_ = os.Setenv(MainConfRemoteAPIEnvName, "")
		cfg.MainConf.API = api
	}
	return nil
}

func parseWebConfig(cfg *MainConf) {
	// 优先使用配置中的绑定参数(HTTP), 英文逗号分隔多个端口
	if cfg.WebConf.ServerAddr == "" {
		cfg.WebConf.ServerAddr = WebServerAddr
	}

	// HTTP 请求体限制, -1 表示无限
	if cfg.WebConf.BodyLimit == 0 {
		cfg.WebConf.BodyLimit = WebBodyLimit
	}

	// 接口签名密钥和生命周期
	cfg.WebConf.SignKey = xcrypto.GetenvDecrypt(WebSignKeyEnvName, cfg.SYSConf.BaseSecretValue)
	if cfg.WebConf.SignTTL < int64(WebSignTTLMin) {
		cfg.WebConf.SignTTL = int64(WebSignTTLDefault)
	}

	// 证书文件存在时开启 HTTPS
	cfg.WebConf.CertFile = os.Getenv(WebCertFileEnvName)
	cfg.WebConf.KeyFile = os.Getenv(WebKeyFileEnvName)
	if xfile.IsFile(cfg.WebConf.CertFile) && xfile.IsFile(cfg.WebConf.KeyFile) {
		// 优先使用配置中的绑定参数(HTTPS), 英文逗号分隔多个端口
		if cfg.WebConf.ServerHttpsAddr == "" {
			cfg.WebConf.ServerHttpsAddr = WebServerHttpsAddr
		}
	} else {
		cfg.WebConf.ServerHttpsAddr = ""
	}

	// 多组 Web 服务复用 web_conf 通用项, 但监听地址必须在各 group 中显式配置,
	// 避免未配置端口的业务组继承主端口后导致重复监听或路由串线.
	if len(cfg.WebConf.Groups) > 0 {
		groups := make(map[string]WebConf, len(cfg.WebConf.Groups))
		for name, group := range cfg.WebConf.Groups {
			groupCfg := NormalizeWebGroupConf(cfg.WebConf, name, group)
			// 解析分组证书: 指定独立证书环境变量时用该组独立证书, 否则继承主配置证书
			resolveGroupCertFile(&groupCfg, group.CertFileEnv, group.KeyFileEnv)
			// 该组无可用证书时关闭其 HTTPS (按分组而非主配置判定, 以支持分组独立证书)
			if groupCfg.CertFile == "" || groupCfg.KeyFile == "" {
				groupCfg.ServerHttpsAddr = ""
			}
			groups[name] = groupCfg
		}
		cfg.WebConf.Groups = groups
	}
}

// resolveGroupCertFile 解析分组独立 TLS 证书.
// 分组同时指定 certFileEnv/keyFileEnv 时, 用对应环境变量指向的证书覆盖继承自主配置的证书;
// 指定了但文件无效时清空该组证书(不回退主证书, 避免证书与该组域名错配), 由调用方据此关闭该组 HTTPS.
// 未指定时不改动 groupCfg, 即继承主配置证书.
func resolveGroupCertFile(groupCfg *WebConf, certFileEnv, keyFileEnv string) {
	if certFileEnv == "" || keyFileEnv == "" {
		return
	}
	certFile := os.Getenv(certFileEnv)
	keyFile := os.Getenv(keyFileEnv)
	if xfile.IsFile(certFile) && xfile.IsFile(keyFile) {
		groupCfg.CertFile = certFile
		groupCfg.KeyFile = keyFile
		return
	}
	groupCfg.CertFile = ""
	groupCfg.KeyFile = ""
}

func parseWhitelistConfig(cfg *MainConf) error {
	if err := ParseRemoteFileConfig(&cfg.WhitelistConf, cfg.SYSConf.BaseSecretValue); err != nil {
		return err
	}
	if cfg.WhitelistConf.Path == "" {
		cfg.WhitelistConf.Path = DefaultWhitelistConfigFile
	}
	WhitelistConfigFile = cfg.WhitelistConf.Path

	// 读取白名单 IP 文件, 追加到 IP 列表
	if ips, e := xfile.ReadLines(WhitelistConfigFile); e == nil && len(ips) > 0 {
		cfg.Whitelist = append(cfg.Whitelist, ips...)
	}

	// 接口 IP 白名单
	whitelist, err := getIPNetList(cfg.Whitelist)
	if err != nil {
		return err
	}
	Whitelist = whitelist
	return nil
}

func parseBlacklistConfig(cfg *MainConf) error {
	if err := ParseRemoteFileConfig(&cfg.BlacklistConf, cfg.SYSConf.BaseSecretValue); err != nil {
		return err
	}
	if cfg.BlacklistConf.Path == "" {
		cfg.BlacklistConf.Path = DefaultBlacklistConfigFile
	}
	BlacklistConfigFile = cfg.BlacklistConf.Path

	// 读取黑名单 IP 文件, 追加到 IP 列表
	if ips, e := xfile.ReadLines(BlacklistConfigFile); e == nil && len(ips) > 0 {
		cfg.Blacklist = append(cfg.Blacklist, ips...)
	}

	// 接口访问 IP 黑名单
	blacklist, err := getIPNetList(cfg.Blacklist)
	if err != nil {
		return err
	}
	Blacklist = blacklist
	return nil
}

// IP 配置转换
func getIPNetList(ips []string) (map[*net.IPNet]int64, error) {
	ipNets := make(map[*net.IPNet]int64)
	for _, ip := range ips {
		// 排除空白行, __ 或 # 开头的注释行
		ip = strings.TrimSpace(ip)
		if ip == "" || strings.HasPrefix(ip, "__") || strings.HasPrefix(ip, "#") {
			continue
		}

		// IP段,数值(一般用于限制器),可选的行内注释 如: 192.168.0.0/16,200,注释(可选)
		ss := strings.SplitN(ip, ",", 3)
		ip = strings.TrimSpace(ss[0])
		val := int64(0)
		if len(ss) >= 2 {
			n := strings.TrimSpace(ss[1])
			val = conv.Atoi(n)
		}

		// 补全掩码
		if !strings.Contains(ip, "/") {
			if strings.Contains(ip, ":") {
				ip = ip + "/128"
			} else {
				ip = ip + "/32"
			}
		}
		// 转为网段
		_, ipNet, err := net.ParseCIDR(ip)
		if err != nil {
			return nil, err
		}
		ipNets[ipNet] = val
	}
	return ipNets, nil
}
