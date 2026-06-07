package engine

import (
	"io"
	"log"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/sync/errgroup"

	"github.com/fufuok/pkg/config"
	"github.com/fufuok/pkg/logger"
	"github.com/fufuok/pkg/web/gin/middleware"
)

type App func(app *gin.Engine) *gin.Engine

var (
	appsMu sync.RWMutex
	apps   []registeredApp
)

type registeredApp struct {
	app       *gin.Engine
	groupName string
	// trustedProxies/trustedPlatform 记录最近一次应用到该 Engine 的信任代理配置,
	// 供 reload 时比对, 仅在实际变化时才修改运行中的 Engine, 避免无谓的并发写.
	trustedProxies  []string
	trustedPlatform string
}

// ServerGroup 描述同一进程内一组独立 gin.Engine 的监听配置和路由注册函数.
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

// RunGroups 启用多组 Web 服务. 每组使用独立 gin.Engine 和独立路由表,
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
			return runOne(group.Setup, cfg, group.Name)
		})
	}
	// fail-fast: 任一端口组监听失败都会使 eg.Wait 返回错误并终止整个进程,
	// 即一个端口冲突/占用会拖垮全部端口组. 这是有意的"启动期即暴露配置错误"取舍.
	if err := eg.Wait(); err != nil {
		log.Fatalln("Failed to start HTTP/HTTPS Server groups:", err, "\nbye.")
	}
}

// newEngine 按既定顺序构建并配置 gin.Engine: 先注册恢复中间件, 再注册业务路由, 最后应用代理信任配置.
// 恢复中间件必须先于业务路由注册: Gin 在路由注册时即快照当前中间件链, 之后再 Use 不会回溯包裹
// 已注册路由, 否则 release 模式下(gin.New() 无内建 Recovery)业务路由 panic 既无 500 也无日志.
func newEngine(setup App, cfg config.WebConf) (*gin.Engine, error) {
	var app *gin.Engine
	if config.Debug {
		app = gin.Default()
	} else {
		gin.SetMode(gin.ReleaseMode)
		app = gin.New()
	}

	app.Use(middleware.RecoveryWithLog(true))

	app = setup(app)

	app.TrustedPlatform = cfg.TrustedPlatform
	if err := app.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		return nil, err
	}
	return app, nil
}

func runOne(setup App, cfg config.WebConf, groupName ...string) error {
	app, err := newEngine(setup, cfg)
	if err != nil {
		return err
	}
	name := ""
	if len(groupName) > 0 {
		name = groupName[0]
	}
	registerApp(app, name, cfg.TrustedProxies, cfg.TrustedPlatform)

	eg := errgroup.Group{}
	if cfg.ServerHttpsAddr != "" {
		for rawAddr := range strings.SplitSeq(cfg.ServerHttpsAddr, ",") {
			// trim 并跳过空地址, 避免 http.Server{Addr: ""} 误绑 :443/:80 及前后空格导致监听失败
			addr := strings.TrimSpace(rawAddr)
			if addr == "" {
				continue
			}
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
	for rawAddr := range strings.SplitSeq(cfg.ServerAddr, ",") {
		// trim 并跳过空地址, 避免 http.Server{Addr: ""} 误绑 :80 及前后空格导致监听失败
		addr := strings.TrimSpace(rawAddr)
		if addr == "" {
			continue
		}
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
		return err
	}
	return nil
}

// SetTrustedProxies 使用当前全局 web_conf 更新所有已启动 gin.Engine 的代理信任配置.
// 多组监听时每组都有独立 Engine, Runtime reload 需要逐个刷新.
//
// 并发说明: Gin 未对 TrustedPlatform / trustedCIDRs 加锁, 在 Engine 服务请求期间写入会与
// 请求处理路径中的 c.ClientIP() 读取产生数据竞争. 绝大多数 reload 并不改动信任代理配置,
// 故此处先比对再决定是否修改, 仅在配置确有变化时才写入, 从而消除常规 reload 的竞争窗口.
// 残留情形(配置确实变更)仍是 best-effort 热更新, 完整无竞争需配合优雅重启(待后续实现).
func SetTrustedProxies() error {
	base := config.Config().WebConf
	appsMu.Lock()
	defer appsMu.Unlock()
	for i := range apps {
		item := &apps[i]
		cfg := base
		if item.groupName != "" {
			cfg = config.NormalizeWebGroupConf(base, item.groupName, base.Groups[item.groupName])
		}
		// 信任代理配置未变化时跳过, 避免无谓地修改正在服务请求的 Engine
		if item.trustedPlatform == cfg.TrustedPlatform && slices.Equal(item.trustedProxies, cfg.TrustedProxies) {
			continue
		}
		item.app.TrustedPlatform = cfg.TrustedPlatform
		if err := item.app.SetTrustedProxies(cfg.TrustedProxies); err != nil {
			return err
		}
		item.trustedProxies = cfg.TrustedProxies
		item.trustedPlatform = cfg.TrustedPlatform
	}
	return nil
}

func registerApp(app *gin.Engine, groupName string, trustedProxies []string, trustedPlatform string) {
	appsMu.Lock()
	defer appsMu.Unlock()
	apps = append(apps, registeredApp{
		app:             app,
		groupName:       groupName,
		trustedProxies:  trustedProxies,
		trustedPlatform: trustedPlatform,
	})
}

func initMiddlewareCache(cfg config.WebConf) error {
	// 黑白名单中间件缓存是包级共享状态, 多组 Web 服务只初始化一次;
	// 主配置无定义时可由应用方在 Start/Runtime 中重新初始化.
	if err := middleware.UseWhitelistCache(cfg.WhitelistLRUCapacity, cfg.WhitelistLRULifetime); err != nil {
		return err
	}
	return middleware.UseBlacklistCache(cfg.BlacklistLRUCapacity, cfg.BlacklistLRULifetime)
}
