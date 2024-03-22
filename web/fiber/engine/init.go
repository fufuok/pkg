package engine

import (
	"errors"
	"log"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"golang.org/x/sync/errgroup"

	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/json"
	"github.com/fufuok/pkg/logger"
	"github.com/fufuok/pkg/logger/sampler"
	"github.com/fufuok/pkg/web/fiber/middleware"
	"github.com/fufuok/pkg/web/fiber/proxy"
	"github.com/fufuok/pkg/web/fiber/response"
)

var app *fiber.App

type App func(app *fiber.App) *fiber.App

// Run 启用 Web 服务
func Run(setup App) {
	cfg := config.Config().WebConf
	app = fiber.New(fiber.Config{
		BodyLimit:               cfg.BodyLimit,
		DisableKeepalive:        cfg.DisableKeepalive,
		ReduceMemoryUsage:       !cfg.DisableReduceMemoryUsage,
		ProxyHeader:             cfg.ProxyHeader,
		EnableTrustedProxyCheck: cfg.EnableTrustedProxyCheck,
		TrustedProxies:          cfg.TrustedProxies,
		JSONEncoder:             json.Marshal,
		JSONDecoder:             json.Unmarshal,
		DisableStartupMessage:   true,
		StrictRouting:           true,
		ErrorHandler:            errorHandler,
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
		for _, addr := range strings.Split(cfg.ServerHttpsAddr, ",") {
			addr := addr
			eg.Go(func() (err error) {
				logger.Info().Str("addr", addr).Msg("Listening and serving HTTPS")
				err = app.ListenTLS(addr, cfg.CertFile, cfg.KeyFile)
				return
			})
		}
	}
	for _, addr := range strings.Split(cfg.ServerAddr, ",") {
		addr := addr
		eg.Go(func() (err error) {
			logger.Info().Str("addr", addr).Msg("Listening and serving HTTP")
			err = app.Listen(addr)
			return
		})
	}
	if err := eg.Wait(); err != nil {
		log.Fatalln("Failed to start HTTP/HTTPS Server:", err, "\nbye.")
	}
}

// 请求错误处理
func errorHandler(c *fiber.Ctx, err error) error {
	code := fiber.StatusInternalServerError
	var e *fiber.Error
	if errors.As(err, &e) {
		code = e.Code
	}
	if code != fiber.StatusBadRequest {
		sampler.Info().Err(err).
			Str("client_ip", proxy.GetClientIP(c)).Str("method", c.Method()).Int("status_code", code).
			Msg(c.OriginalURL())
	}
	return response.APIException(c, code, "", nil)
}
