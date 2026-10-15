package engine

import (
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/compress"

	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/json"
	"github.com/fufuok/pkg/logger"
	"github.com/fufuok/pkg/logger/sampler"
	"github.com/fufuok/pkg/web/fiber/middleware"
	"github.com/fufuok/pkg/web/fiber/response"
	"github.com/fufuok/pkg/web/fiber/tproxy"
)

type App func(app *fiber.App) *fiber.App

// ServerGroup 描述同一进程内一组独立 fiber.App 的监听配置和路由注册函数.
// 适用于一个应用按业务域拆分端口, 避免多个端口共享同一套路由表.
type ServerGroup struct {
	Name    string
	WebConf config.WebConf
	Setup   App
}

// Run 启用 Web 服务. 可选传入 webConf, 传入时优先使用, 便于同一应用按不同配置启动并监听不同端口.
func Run(setup App, webConf ...config.WebConf) {
	cfg := config.Config().WebConf
	if len(webConf) > 0 {
		cfg = config.NormalizeWebConf(cfg, webConf[0])
	}

	if cfg.Name == "" {
		cfg.Name = config.AppName
	}
	if err := initMiddlewareCache(cfg); err != nil {
		log.Fatalln("Failed to initialize whitelist/blacklist config:", err, "\nbye.")
	}
	runOne(setup, cfg)
}

// RunGroups 启用多组 Web 服务. 每组使用独立 fiber.App 和独立路由表,
// 但复用 config.WebConf 的通用默认项, 适合 API/admin/callback 等业务端口隔离.
func RunGroups(groups ...ServerGroup) {
	base := config.Config().WebConf
	if len(groups) == 0 {
		log.Fatalln("Failed to start HTTP/HTTPS Server groups: groups cannot be empty\nbye.")
	}
	if err := initMiddlewareCache(base); err != nil {
		log.Fatalln("Failed to initialize whitelist/blacklist config:", err, "\nbye.")
	}

	var wg sync.WaitGroup
	for _, group := range groups {
		if group.Setup == nil {
			log.Fatalln("Failed to start HTTP/HTTPS Server: setup cannot be nil\nbye.")
		}
		groupConf := group.WebConf
		if configured, ok := base.Groups[group.Name]; ok {
			groupConf = config.NormalizeWebConf(configured, group.WebConf)
		}
		cfg := config.NormalizeWebGroupConf(base, group.Name, groupConf)
		if cfg.Name == "" {
			cfg.Name = config.AppName
		}
		wg.Go(func() {
			runOne(group.Setup, cfg)
		})
	}
	// 错误在对应任务中直接退出, 不等待其他长驻监听; 正常情况下持续等待服务.
	wg.Wait()
}

// newApp 按既定顺序构建并配置 fiber.App: 先注册恢复/压缩中间件, 再注册业务路由.
// cfg 由配置加载或归一化入口补齐默认值, 本函数只映射已处理的配置.
// 恢复/压缩中间件必须先于业务路由注册: Fiber 按注册顺序构建处理链, 路由处理器通常不调用 c.Next(),
// 之后再 Use 的中间件不会包裹已注册路由, 否则业务路由 panic 不会被恢复为 500.
func newApp(setup App, cfg config.WebConf) *fiber.App {
	app := fiber.New(fiber.Config{
		AppName:            cfg.Name,
		BodyLimit:          cfg.BodyLimit,
		DisableKeepalive:   cfg.DisableKeepalive,
		ReduceMemoryUsage:  !cfg.DisableReduceMemoryUsage,
		ProxyHeader:        cfg.ProxyHeader,
		TrustProxy:         cfg.EnableTrustedProxyCheck,
		EnableIPValidation: cfg.EnableIPValidation,
		TrustProxyConfig: fiber.TrustProxyConfig{
			Proxies: cfg.TrustedProxies,
		},
		JSONEncoder:   json.Marshal,
		JSONDecoder:   json.Unmarshal,
		StrictRouting: true,
		ErrorHandler:  errorHandler,
	})

	app.Use(
		middleware.RecoverLogger(),
		compress.New(),
	)

	return setup(app)
}

// runOne 构建服务并等待全部监听; 任一监听错误直接退出整个进程.
func runOne(setup App, cfg config.WebConf) {
	app := newApp(setup, cfg)

	var wg sync.WaitGroup
	var tlsConfig *tls.Config
	for rawAddr := range strings.SplitSeq(cfg.ServerHttpsAddr, ",") {
		// trim 并跳过空地址; 未配置 HTTPS 时也不加载证书或创建监听.
		addr := strings.TrimSpace(rawAddr)
		if addr == "" {
			continue
		}
		// 同一 App 的全部监听共享握手记录, 关闭连接时才能由同一 handler 清理.
		if tlsConfig == nil {
			var err error
			tlsConfig, err = initTLSConfig(app, cfg.CertFile, cfg.KeyFile)
			if err != nil {
				log.Fatalln("Failed to start HTTPS Server ["+cfg.Name+"] on "+addr+":", err, "\nbye.")
			}
		}
		wg.Go(func() {
			logger.Warn().Str("addr", addr).Str("service", cfg.Name).Msg("HTTPS server started")
			if err := app.Listen(addr, fiber.ListenConfig{
				DisableStartupMessage: true,
				TLSConfig:             tlsConfig,
			}); err != nil {
				log.Fatalln("Failed to start HTTPS Server ["+cfg.Name+"] on "+addr+":", err, "\nbye.")
			}
		})
	}
	for rawAddr := range strings.SplitSeq(cfg.ServerAddr, ",") {
		// trim 并跳过空地址, 避免空串或前后空格导致监听失败
		addr := strings.TrimSpace(rawAddr)
		if addr == "" {
			continue
		}
		wg.Go(func() {
			logger.Warn().Str("addr", addr).Str("service", cfg.Name).Msg("HTTP server started")
			if err := app.Listen(addr, fiber.ListenConfig{
				DisableStartupMessage: true,
			}); err != nil {
				log.Fatalln("Failed to start HTTP Server ["+cfg.Name+"] on "+addr+":", err, "\nbye.")
			}
		})
	}
	wg.Wait()
}

// initTLSConfig 加载启动证书并为同一 App 的 HTTPS 监听建立共享握手状态.
// Listen 会克隆 TLSConfig, 但保留同一 GetCertificate 接收者; TLS 最低版本和证书验证不变.
func initTLSConfig(app *fiber.App, certFile, keyFile string) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load HTTPS key pair (cert=%q, key=%q): %w", certFile, keyFile, err)
	}
	shared := new(fiber.TLSHandler)
	app.SetTLSHandler(shared)
	return &tls.Config{
		MinVersion:     tls.VersionTLS12,
		Certificates:   []tls.Certificate{cert},
		GetCertificate: shared.GetClientInfo,
	}, nil
}

// 请求错误处理
func errorHandler(c fiber.Ctx, err error) error {
	code := fiber.StatusInternalServerError
	if e, ok := errors.AsType[*fiber.Error](err); ok {
		code = e.Code
	}
	if code != fiber.StatusBadRequest {
		sampler.Info().Err(err).
			Str("client_ip", tproxy.GetClientIP(c)).Str("method", c.Method()).Int("status_code", code).
			Msg(c.OriginalURL())
	}
	return response.APIException(c, code, "", nil)
}

func initMiddlewareCache(cfg config.WebConf) error {
	// 黑白名单中间件缓存是包级共享状态, 多组 Web 服务只初始化一次;
	// 主配置无定义时可由应用方在 Start/Runtime 中重新初始化.
	if err := middleware.UseWhitelistCache(cfg.WhitelistLRUCapacity, cfg.WhitelistLRULifetime); err != nil {
		return err
	}
	return middleware.UseBlacklistCache(cfg.BlacklistLRUCapacity, cfg.BlacklistLRULifetime)
}
