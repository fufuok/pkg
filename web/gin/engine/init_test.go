package engine

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/fufuok/utils/assert"

	"github.com/fufuok/pkg/config"
)

// TestNewEngineRecoversPanic 回归测试 (#1): release 模式下 gin.New() 不含内建 Recovery,
// 恢复中间件必须先于业务路由注册才能包裹它们, 使路由 panic 被捕获并返回 500.
//
// 守护点: 若恢复中间件被误移回 setup 之后注册, 业务路由将不被包裹, 此时直接 ServeHTTP
// 触发的 panic 会逸出 (无内建恢复/无 net/http 连接级兜底), 导致本测试直接 panic 而失败.
func TestNewEngineRecoversPanic(t *testing.T) {
	// 显式走 release 分支 (gin.New(), 不含内建 Recovery), 避免受其他测试或默认值影响
	old := config.Debug
	config.Debug = false
	defer func() { config.Debug = old }()

	setup := func(app *gin.Engine) *gin.Engine {
		app.GET("/panic", func(*gin.Context) {
			panic("boom: simulated handler panic")
		})
		app.GET("/ok", func(c *gin.Context) {
			c.String(http.StatusOK, "OK")
		})
		return app
	}

	app, err := newEngine(setup, config.WebConf{})
	assert.Nil(t, err)

	// panic 路由应被恢复中间件捕获并返回 500
	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/panic", nil))
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	// 正常路由不受影响, 仍返回 200, 说明恢复后进程存活可继续服务
	w = httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ok", nil))
	assert.Equal(t, http.StatusOK, w.Code)
}
