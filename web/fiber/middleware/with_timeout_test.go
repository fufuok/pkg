package middleware

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fufuok/utils/assert"
	"github.com/gofiber/fiber/v2"
)

// Ref: https://github.com/gofiber/fiber/tree/master/middleware/timeout
var ErrFooTimeOut = errors.New("foo context canceled")

func Test_TimeoutUseWithCustomError(t *testing.T) {
	app := fiber.New()
	app.Use(WithTimeout(200*time.Millisecond, ErrFooTimeOut))
	h := func(c *fiber.Ctx) error {
		sleepTime, _ := time.ParseDuration(c.Params("sleepTime") + "ms")
		if err := sleepWithContext(c.UserContext(), sleepTime, context.DeadlineExceeded); err != nil {
			return fmt.Errorf("%w: l2 wrap", fmt.Errorf("%w: l1 wrap ", err))
		}
		return nil
	}
	group := app.Group("/group", WithTimeout(100*time.Millisecond, ErrFooTimeOut))
	{
		group.Get("/:sleepTime", h)
	}
	app.Get("/test/:sleepTime", WithTimeout(100*time.Millisecond, ErrFooTimeOut), h)
	app.Get("/:sleepTime", h)
	testTimeout := func(traget string) {
		resp, err := app.Test(httptest.NewRequest("GET", traget, nil))
		assert.Equal(t, nil, err, "app.Test(req)")
		assert.Equal(t, fiber.StatusRequestTimeout, resp.StatusCode, "Status code")
		_ = resp.Body.Close()
	}
	testSucces := func(traget string) {
		resp, err := app.Test(httptest.NewRequest("GET", traget, nil))
		assert.Equal(t, nil, err, "app.Test(req)")
		assert.Equal(t, fiber.StatusOK, resp.StatusCode, "Status code")
		_ = resp.Body.Close()
	}
	testTimeout("/300")
	testTimeout("/group/150")
	testTimeout("/test/150")
	testSucces("/150")
	testSucces("/group/30")
	testSucces("/test/30")
}

func Test_WithTimeoutSkipTimeoutStatus(t *testing.T) {
	app := fiber.New()
	app.Use(WithTimeout(200*time.Millisecond, ErrFooTimeOut))
	h := func(c *fiber.Ctx) error {
		sleepTime, _ := time.ParseDuration(c.Params("sleepTime") + "ms")
		if err := sleepWithContext(c.UserContext(), sleepTime, context.DeadlineExceeded); err != nil {
			return c.SendString("Error: " + err.Error())
		}
		return c.SendString("OK")
	}
	group := app.Group("/group", WithTimeout(100*time.Millisecond, ErrFooTimeOut))
	{
		group.Get("/:sleepTime", h)
	}
	app.Get("/test/:sleepTime", WithTimeout(100*time.Millisecond, ErrFooTimeOut), h)
	app.Get("/:sleepTime", h)
	testTimeout := func(traget string) {
		resp, err := app.Test(httptest.NewRequest("GET", traget, nil))
		assert.Equal(t, nil, err, "app.Test(req)")
		assert.Equal(t, fiber.StatusOK, resp.StatusCode, "Status code")
		body, err := io.ReadAll(resp.Body)
		assert.Equal(t, nil, err)
		assert.Equal(t, "Error: "+context.DeadlineExceeded.Error(), string(body))
		_ = resp.Body.Close()
	}
	testSucces := func(traget string) {
		resp, err := app.Test(httptest.NewRequest("GET", traget, nil))
		assert.Equal(t, nil, err, "app.Test(req)")
		assert.Equal(t, fiber.StatusOK, resp.StatusCode, "Status code")
		body, err := io.ReadAll(resp.Body)
		assert.Equal(t, nil, err)
		assert.Equal(t, "OK", string(body))
		_ = resp.Body.Close()
	}
	testTimeout("/300")
	testTimeout("/group/150")
	testTimeout("/test/150")
	testSucces("/150")
	testSucces("/group/30")
	testSucces("/test/30")
}

func sleepWithContext(ctx context.Context, d time.Duration, te error) error {
	timer := time.NewTimer(d)
	select {
	case <-ctx.Done():
		if !timer.Stop() {
			<-timer.C
		}
		return te
	case <-timer.C:
	}
	return nil
}
