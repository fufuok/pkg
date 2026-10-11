package conf

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"

	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/crontab"
	"github.com/fufuok/pkg/json"
	"github.com/fufuok/pkg/pubsub"
	"github.com/fufuok/pkg/xcrypto"
)

var (
	// 应用配置项. 热更时整体替换, 使用时调用 App().
	appMainConf atomic.Pointer[AppMainConf]

	// Notifier 广播业务配置已替换. TryPub 不阻塞, 慢订阅者可能漏掉通知.
	Notifier *pubsub.PubSub[string, struct{}]
)

// AppMainConf 是应用侧解析结果.
//
// pkg 已解析公共配置. 应用只在这里解析自己拥有的 app_conf, 不重复定义 sys_conf、web_conf.
// 调用方通过 App() 读取, 不得修改返回对象.
type AppMainConf struct {
	AppConf AppConf `json:"app_conf"`
}

// AppConf 是本项目拥有的业务配置.
type AppConf struct {
	// Message 是状态接口返回的业务消息, 不能为空.
	Message string `json:"message"`

	// Note 是解密后的可选备注. 环境变量缺失时保持空串, 不视为配置错误.
	Note string `json:"-"`

	// PayloadKey 是对外 AES-GCM 的独立随机密钥. 仅从内部配置密文加载, 不输出到 JSON.
	PayloadKey []byte `json:"-"`

	// HeartbeatSpec 支持 pkg 的可选秒表达式. 本例使用 6 段, 空值使用 DefaultHeartbeatSpec.
	HeartbeatSpec string `json:"heartbeat_spec"`
}

// LoadConfig 加载应用配置.
func LoadConfig() error {
	cfg, err := readConfig()
	if err != nil {
		// 失败时保留上一份快照, 不回滚 pkg 已发布的公共配置.
		return fmt.Errorf("load app config: %w", err)
	}
	appMainConf.Store(cfg)
	if Notifier != nil {
		Notifier.TryPub(struct{}{}, DataConfTopic)
	}
	return nil
}

// readConfig 读取并解析应用配置.
func readConfig() (*AppMainConf, error) {
	// 1. 优先使用程序变量或内嵌配置文档.
	body := config.AppConfigBody
	if len(body) == 0 {
		// 2. 未设置时读取 pkg 已选定的主配置文件.
		var err error
		body, err = os.ReadFile(config.ConfigFile)
		if err != nil {
			return nil, fmt.Errorf("read app config %s: %w", config.ConfigFile, err)
		}
	}

	cfg := new(AppMainConf)
	if err := json.Unmarshal(body, cfg); err != nil {
		return nil, fmt.Errorf("decode app config: %w", err)
	}
	if err := parseAppConfig(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// parseAppConfig 合并环境变量并校验必要字段, 失败时不发布应用快照.
func parseAppConfig(cfg *AppMainConf) error {
	// 使用 loadEnvConfig 已热加载的值, 后加载 env 的同名变量优先于 JSON.
	if message := strings.TrimSpace(Message); message != "" {
		cfg.AppConf.Message = message
	}
	if strings.TrimSpace(cfg.AppConf.Message) == "" {
		return errors.New("app_conf.message cannot be empty")
	}
	// GetenvDecrypt 是程序内部配置密钥方案.
	cfg.AppConf.Note = xcrypto.GetenvDecrypt(NoteEnvName, config.BaseSecretValue)
	// 通用 AES 密钥与配置包装密钥分开. env 中先包装 Raw URL Base64, 再还原随机字节.
	key, err := base64.RawURLEncoding.DecodeString(xcrypto.GetenvDecrypt(PayloadKeyEnvName, config.BaseSecretValue))
	if err != nil {
		return fmt.Errorf("decode %s: %w", PayloadKeyEnvName, err)
	}
	if len(key) != xcrypto.AES256KeySize {
		return fmt.Errorf("%s must decode to %d bytes", PayloadKeyEnvName, xcrypto.AES256KeySize)
	}
	cfg.AppConf.PayloadKey = key
	if Main().WebConf.SignKey == "" {
		return fmt.Errorf("%s cannot be empty", config.WebSignKeyEnvName)
	}
	if cfg.AppConf.HeartbeatSpec == "" {
		cfg.AppConf.HeartbeatSpec = DefaultHeartbeatSpec
	}
	if _, err := crontab.DefaultParser.Parse(cfg.AppConf.HeartbeatSpec); err != nil {
		return fmt.Errorf("parse app_conf.heartbeat_spec: %w", err)
	}
	return nil
}

// HeartbeatSpecOrDefault 返回心跳 cron 表达式.
func (c *AppMainConf) HeartbeatSpecOrDefault() string {
	if c == nil || c.AppConf.HeartbeatSpec == "" {
		return DefaultHeartbeatSpec
	}
	return c.AppConf.HeartbeatSpec
}
