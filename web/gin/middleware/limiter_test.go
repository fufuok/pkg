package middleware

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// TestLimiterUnlimitedLifecycle 验证无限流请求也计入在途数量, 返回后恢复为零.
func TestLimiterUnlimitedLifecycle(t *testing.T) {
	limiter := NewRequestsLimiter(-1, nil)
	app := gin.New()
	app.Use(limiter.Handler())
	app.GET("/", func(c *gin.Context) {
		if n := limiter.Running(); n != 1 {
			t.Errorf("running during request = %d, want 1", n)
		}
		c.Status(http.StatusOK)
	})
	for range 5 {
		w := httptest.NewRecorder()
		app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
		if w.Code != http.StatusOK || limiter.Running() != 0 {
			t.Fatalf("status = %d, running after request = %d", w.Code, limiter.Running())
		}
	}
}

// TestLimiterToggleWithActiveRequests 验证开关限制时仍计入已准入的请求, 拒绝不会扣减计数.
func TestLimiterToggleWithActiveRequests(t *testing.T) {
	limiter := NewRequestsLimiter(1, nil)
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var requests sync.WaitGroup
	defer func() {
		close(release)
		requests.Wait()
		if n := limiter.Running(); n != 0 {
			t.Errorf("running after release = %d, want 0", n)
		}
	}()
	app := gin.New()
	app.Use(limiter.Handler())
	app.GET("/held", func(c *gin.Context) {
		started <- struct{}{}
		<-release
		c.Status(http.StatusOK)
	})
	app.GET("/probe", func(c *gin.Context) { c.Status(http.StatusOK) })
	for i := range 2 {
		if i == 1 {
			limiter.SetLimit(-1)
		}
		requests.Go(func() {
			w := httptest.NewRecorder()
			app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/held", nil))
			if w.Code != http.StatusOK {
				t.Errorf("held request status = %d", w.Code)
			}
		})
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("held request did not start")
		}
		if n := limiter.Running(); n != int32(i+1) {
			t.Fatalf("running = %d, want %d", n, i+1)
		}
	}
	for _, limit := range []int32{1, 0} {
		limiter.SetLimit(limit)
		w := httptest.NewRecorder()
		app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/probe", nil))
		if w.Code != http.StatusTooManyRequests || limiter.Running() != 2 {
			t.Fatalf("limit = %d, status = %d, running = %d", limit, w.Code, limiter.Running())
		}
	}
	if n := limiter.Limited(); n != 2 {
		t.Errorf("limited = %d, want 2", n)
	}
}
