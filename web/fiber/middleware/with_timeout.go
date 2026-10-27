package middleware

import (
	"slices"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/timeout"

	"github.com/fufuok/pkg/config"
)

// asyncTimeoutKey 标记本请求已进入 pkg 异步超时处理, 此后进入的缓存保守透传.
// 标记保留至请求回收, 不表示实时协程身份; 只识别本包 WithTimeout, 不保护外置的第三方异步 timeout.
type asyncTimeoutKey struct{}

// WithTimeoutDefault 使用 config.WebTimeout 构建中间件, 其他行为与 WithTimeout 一致.
func WithTimeoutDefault(timeoutErrors ...error) fiber.Handler {
	return WithTimeout(config.WebTimeout, timeoutErrors...)
}

// WithTimeout 为后续 Handler 设置处理期限, 默认沿用 Fiber 的 503 超时响应.
// Handler 应监听 c.Context().Done() 并及时结束; 超时回包不会强制终止业务逻辑.
// timeoutErrors 在构造时复制, 按错误链匹配自定义超时错误; context.Canceled 需要显式注册,
// 其他业务错误继续交由应用 ErrorHandler 处理. 每请求只支持一层异步 timeout.
// 后续处理链运行在独立工作协程, 截止后与请求协程并发访问 Ctx; 响应缓存须注册在其外层,
// 注册在内层的缓存识别 asyncTimeoutKey 后整段透传. dur 非正时与 Fiber timeout 一致完全禁用期限:
// 同步执行, 不设置标记, 也不改写业务返回值.
func WithTimeout(dur time.Duration, timeoutErrors ...error) fiber.Handler {
	if dur <= 0 {
		return func(c fiber.Ctx) error { return c.Next() }
	}
	h := timeout.New(timeoutWorker, timeout.Config{
		Timeout: dur,
		// 保存快照, 构造后调用方修改自己的切片不影响匹配.
		Errors: slices.Clone(timeoutErrors),
	})
	return func(c fiber.Ctx) error {
		// 在请求协程上于工作协程启动前设置, 启动建立先后关系, 工作协程读取安全; 标记保留至请求回收,
		// 截止后工作协程仍可能读取, 返回时不清除.
		c.SetLocal(asyncTimeoutKey{}, struct{}{})
		return h(c)
	}
}

// timeoutWorker 在超时工作协程中执行剩余处理链. 入口 context 即带期限的超时上下文, 在业务之前保存,
// 内层替换并收尾 cancel 的派生 context 不影响判断. 业务返回的非 nil 错误原样返回, 由 timeout 按错误链
// 与 timeoutErrors 处理 (显式返回 context.Canceled 时须自行注册才映射为超时响应); 返回 nil 但入口 context
// 已截止或被上游取消时改为 fiber.ErrServiceUnavailable, 防止 Fiber 在截止与返回同时就绪时选择完成分支
// 并把残缺结果当作成功. 完成分支仍经过应用 ErrorHandler, 不保证与独立截止响应具有相同正文.
func timeoutWorker(c fiber.Ctx) error {
	ctx := c.Context()
	if err := c.Next(); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return fiber.ErrServiceUnavailable
	}
	return nil
}
