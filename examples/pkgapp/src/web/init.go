// Package web 负责 HTTP 生命周期和路由装配.
package web

import (
	"github.com/fufuok/pkg/web/gin/engine"
	"github.com/fufuok/pkg/web/gin/middleware"
	"github.com/gin-gonic/gin"

	"github.com/fufuok/pkgapp/conf"
	"github.com/fufuok/pkgapp/web/router/api"
	"github.com/fufuok/pkgapp/web/router/sys"
)

// M 是 Web 生命周期模块.
type M struct{}

// Start 异步启动 Web.
func (m *M) Start() error {
	go initWeb()
	return nil
}

// Runtime 刷新可热更的 Web 配置.
func (m *M) Runtime() error {
	cfg := conf.Main().WebConf
	if err := middleware.UseWhitelistCache(cfg.WhitelistLRUCapacity, cfg.WhitelistLRULifetime); err != nil {
		return err
	}
	// server_addr、路由和 TLS 不在这里切换, 修改后需要重启.
	return engine.SetTrustedProxies()
}

// Stop 当前为空操作, engine.Run 不暴露关闭接口, 监听随进程退出释放.
// 待 pkg 提供关闭能力后再接入, 示例不另持有 HTTP Server.
func (m *M) Stop() error {
	return nil
}

// initWeb 组装路由并启动监听.
func initWeb() {
	setup := func(app *gin.Engine) *gin.Engine {
		// 按业务、系统、异常的既定顺序装配, 系统行为交给 pkg.
		api.SetupRouter(app)
		sys.SetupRouter(app)
		engine.SetupSYSRouter(app)
		engine.SetupExceptionRouter(app)
		return app
	}
	engine.Run(setup)
}
