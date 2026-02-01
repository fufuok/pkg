package engine

import (
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/sync/errgroup"

	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/logger"
	"github.com/fufuok/pkg/web/gin/middleware"
)

var app *gin.Engine

type App func(app *gin.Engine) *gin.Engine

// Run 启用 Web 服务
func Run(setup App) {
	if config.Debug {
		app = gin.Default()
	} else {
		gin.SetMode(gin.ReleaseMode)
		app = gin.New()
	}

	cfg := config.Config().WebConf
	app = setup(app)

	if err := SetTrustedProxies(); err != nil {
		log.Fatalln("Failed to SetTrustedProxies:", err, "\nbye.")
	}

	// 黑白名单中间件缓存初始化, 主配置无定义时可由应用方重新初始化
	if err := middleware.UseWhitelistCache(cfg.WhitelistLRUCapacity, cfg.WhitelistLRULifetime); err != nil {
		log.Fatalln("Failed to initialize whitelist config:", err, "\nbye.")
	}
	if err := middleware.UseBlacklistCache(cfg.BlacklistLRUCapacity, cfg.BlacklistLRULifetime); err != nil {
		log.Fatalln("Failed to initialize blacklist config:", err, "\nbye.")
	}

	app.Use(middleware.RecoveryWithLog(true))

	eg := errgroup.Group{}
	if cfg.ServerHttpsAddr != "" {
		for _, addr := range strings.Split(cfg.ServerHttpsAddr, ",") {
			eg.Go(func() error {
				logger.Warn().Str("addr", addr).Msg("HTTPS server started")
				server := &http.Server{
					Addr:     addr,
					Handler:  app,
					ErrorLog: log.New(io.Discard, "", 0), // 禁用底层服务器错误日志(TLS握手/连接重置等)
				}
				return server.ListenAndServeTLS(cfg.CertFile, cfg.KeyFile)
			})
		}
	}
	for _, addr := range strings.Split(cfg.ServerAddr, ",") {
		eg.Go(func() error {
			logger.Warn().Str("addr", addr).Msg("HTTP server started")
			server := &http.Server{
				Addr:     addr,
				Handler:  app,
				ErrorLog: log.New(io.Discard, "", 0), // 禁用底层服务器错误日志(TLS握手/连接重置等)
			}
			return server.ListenAndServe()
		})
	}
	if err := eg.Wait(); err != nil {
		log.Fatalln("Failed to start HTTP/HTTPS Server:", err, "\nbye.")
	}
}

// SetTrustedProxies 加载受信任的客户端 IP 代理配置
func SetTrustedProxies() error {
	app.TrustedPlatform = config.Config().WebConf.TrustedPlatform
	return app.SetTrustedProxies(config.Config().WebConf.TrustedProxies)
}
