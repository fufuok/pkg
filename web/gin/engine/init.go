package engine

import (
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/sync/errgroup"

	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/logger"
	"github.com/fufuok/pkg/web/gin/middleware"
)

type App func(app *gin.Engine) *gin.Engine

// Run 启用 Web 服务. 可选传入 webConf, 传入时优先使用, 便于同一应用按不同配置启动并监听不同端口.
func Run(setup App, webConf ...config.WebConf) {
	var app *gin.Engine
	if config.Debug {
		app = gin.Default()
	} else {
		gin.SetMode(gin.ReleaseMode)
		app = gin.New()
	}

	cfg := config.Config().WebConf
	if len(webConf) > 0 {
		cfg = webConf[0]
	}

	if cfg.Name == "" {
		cfg.Name = config.AppName
	}

	app = setup(app)

	app.TrustedPlatform = cfg.TrustedPlatform
	if err := app.SetTrustedProxies(cfg.TrustedProxies); err != nil {
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
		for addr := range strings.SplitSeq(cfg.ServerHttpsAddr, ",") {
			eg.Go(func() error {
				logger.Warn().Str("addr", addr).Str("service", cfg.Name).Msg("HTTPS server started")
				server := &http.Server{
					Addr:              addr,
					Handler:           app,
					ErrorLog:          log.New(io.Discard, "", 0), // 禁用底层服务器错误日志 (TLS 握手/连接重置等)
					ReadHeaderTimeout: 5 * time.Second,            // 防止 Slowloris 攻击
				}
				return server.ListenAndServeTLS(cfg.CertFile, cfg.KeyFile)
			})
		}
	}
	for addr := range strings.SplitSeq(cfg.ServerAddr, ",") {
		eg.Go(func() error {
			logger.Warn().Str("addr", addr).Str("service", cfg.Name).Msg("HTTP server started")
			server := &http.Server{
				Addr:              addr,
				Handler:           app,
				ErrorLog:          log.New(io.Discard, "", 0), // 禁用底层服务器错误日志 (TLS 握手/连接重置等)
				ReadHeaderTimeout: 5 * time.Second,            // 防止 Slowloris 攻击
			}
			return server.ListenAndServe()
		})
	}
	if err := eg.Wait(); err != nil {
		log.Fatalln("Failed to start HTTP/HTTPS Server ["+cfg.Name+"]:", err, "\nbye.")
	}
}
