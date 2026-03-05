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

// Run 启用 Web 服务. 可选传入 webConf, 传入时优先使用, 便于同一应用按不同配置启动并监听不同端口.
func Run(setup App, webConf ...config.WebConf) {
	cfg := config.Config().WebConf
	if len(webConf) > 0 {
		cfg = webConf[0]
	}

	if cfg.Name == "" {
		cfg.Name = config.AppName
	}

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

	app = setup(app)

	// 黑白名单中间件缓存初始化, 主配置无定义时可由应用方重新初始化
	if err := middleware.UseWhitelistCache(cfg.WhitelistLRUCapacity, cfg.WhitelistLRULifetime); err != nil {
		log.Fatalln("Failed to initialize whitelist config:", err, "\nbye.")
	}
	if err := middleware.UseBlacklistCache(cfg.BlacklistLRUCapacity, cfg.BlacklistLRULifetime); err != nil {
		log.Fatalln("Failed to initialize blacklist config:", err, "\nbye.")
	}

	app.Use(
		middleware.RecoverLogger(),
		compress.New(),
	)

	eg := errgroup.Group{}
	if cfg.ServerHttpsAddr != "" {
		for addr := range strings.SplitSeq(cfg.ServerHttpsAddr, ",") {
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
	for addr := range strings.SplitSeq(cfg.ServerAddr, ",") {
		eg.Go(func() (err error) {
			logger.Warn().Str("addr", addr).Str("service", cfg.Name).Msg("HTTP server started")
			err = app.Listen(addr, fiber.ListenConfig{
				DisableStartupMessage: true,
			})
			return
		})
	}
	if err := eg.Wait(); err != nil {
		log.Fatalln("Failed to start HTTP/HTTPS Server ["+cfg.Name+"]:", err, "\nbye.")
	}
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
