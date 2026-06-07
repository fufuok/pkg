package engine

import (
	"errors"
	"log"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/compress"
	"golang.org/x/sync/errgroup"

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
	if err := runOne(setup, cfg); err != nil {
		log.Fatalln("Failed to start HTTP/HTTPS Server ["+cfg.Name+"]:", err, "\nbye.")
	}
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

	eg := errgroup.Group{}
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
		eg.Go(func() error {
			return runOne(group.Setup, cfg)
		})
	}
	// fail-fast: 任一端口组监听失败都会使 eg.Wait 返回错误并终止整个进程,
	// 即一个端口冲突/占用会拖垮全部端口组. 这是有意的"启动期即暴露配置错误"取舍.
	if err := eg.Wait(); err != nil {
		log.Fatalln("Failed to start HTTP/HTTPS Server groups:", err, "\nbye.")
	}
}

// newApp 按既定顺序构建并配置 fiber.App: 先注册恢复/压缩中间件, 再注册业务路由.
// 恢复/压缩中间件必须先于业务路由注册: Fiber 按注册顺序构建处理链, 路由处理器通常不调用 c.Next(),
// 之后再 Use 的中间件不会包裹已注册路由, 否则业务路由 panic 不会被恢复为 500.
func newApp(setup App, cfg config.WebConf) *fiber.App {
	app := fiber.New(fiber.Config{
		AppName:           cfg.Name,
		BodyLimit:         cfg.BodyLimit,
		DisableKeepalive:  cfg.DisableKeepalive,
		ReduceMemoryUsage: !cfg.DisableReduceMemoryUsage,
		ProxyHeader:       cfg.ProxyHeader,
		TrustProxy:        cfg.EnableTrustedProxyCheck,
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

func runOne(setup App, cfg config.WebConf) error {
	app := newApp(setup, cfg)

	eg := errgroup.Group{}
	if cfg.ServerHttpsAddr != "" {
		for rawAddr := range strings.SplitSeq(cfg.ServerHttpsAddr, ",") {
			// trim 并跳过空地址, 避免空串或前后空格导致监听失败
			addr := strings.TrimSpace(rawAddr)
			if addr == "" {
				continue
			}
			eg.Go(func() (err error) {
				logger.Warn().Str("addr", addr).Str("service", cfg.Name).Msg("HTTPS server started")
				err = app.Listen(addr, fiber.ListenConfig{
					DisableStartupMessage: true,
					CertFile:              cfg.CertFile,
					CertKeyFile:           cfg.KeyFile,
				})
				return
			})
		}
	}
	for rawAddr := range strings.SplitSeq(cfg.ServerAddr, ",") {
		// trim 并跳过空地址, 避免空串或前后空格导致监听失败
		addr := strings.TrimSpace(rawAddr)
		if addr == "" {
			continue
		}
		eg.Go(func() (err error) {
			logger.Warn().Str("addr", addr).Str("service", cfg.Name).Msg("HTTP server started")
			err = app.Listen(addr, fiber.ListenConfig{
				DisableStartupMessage: true,
			})
			return
		})
	}
	if err := eg.Wait(); err != nil {
		return err
	}
	return nil
}

// 请求错误处理
func errorHandler(c fiber.Ctx, err error) error {
	code := fiber.StatusInternalServerError
	var e *fiber.Error
	if errors.As(err, &e) {
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
