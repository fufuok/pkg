# HTTP 响应缓存

Gin 和 Fiber 提供相同的两个入口, 共用进程内有界 LRU, 无需在业务层维护响应缓存:

| API | 用途 |
| --- | --- |
| `CacheByRequestURI(ttl, ...CacheConfig)` | 无请求体的公开 GET, 保留完整 URI 和查询参数顺序 |
| `CacheByRawData(ttl, ...CacheConfig)` | 应用明确声明为只读查询的 POST, 在完整请求键中加入原始正文 |

每次构造中间件创建独立缓存, 也可将同一实例注册到路由组. 缓存命中会跳过下游处理器, 因此 RawData 不得用于写操作、支付或其他有副作用的接口. HEAD 和其他方法直接透传.

## 选型结论

2026-10-22 对固定源码和实际行为进行核对后, 本项目采用原生 Gin/Fiber 适配层, 复用已有的 [freelru v0.17.0](https://github.com/fufuok/freelru/tree/v0.17.0) 作为存储. 选择依据是请求隔离、保守准入、明确内存预算和较小的维护范围, 不声称在所有负载下性能最优.

| 方案与固定来源 | 已核实能力和边界 | 本项目取舍 |
| --- | --- | --- |
| [victorspringer/http-cache f737c7a](https://github.com/victorspringer/http-cache/tree/f737c7a8ec8125e1b48fc59dd5fc942a1f25624b) | net/http、内存/Redis、可选 singleflight/SWR; 探针复现 Host/Method 缺失、凭据和 Set-Cookie 串缓存、未知 Vary、多行 Cache-Control 遗漏、max-age=0 变永久及查询重排 | 需要修补安全策略及框架桥接, 本次不引入 |
| [chenyahui/gin-cache v1.10.0](https://github.com/chenyahui/gin-cache/tree/v1.10.0) | Gin 原生、存储接口、singleflight; 默认 URI 缓存探针复现 Cookie、no-store、语言 Vary 未隔离 | 替换原有包装, 删除其依赖与外露选项 |
| [gin-contrib/cache c8bad2a](https://github.com/gin-contrib/cache/tree/c8bad2a66868ffd7918b1aeb8f71a8ec9c4bc66a) | Gin 原生、存储接口、页面装饰器; 默认 key 基于 URI, 写响应时更新 store, Atomic 版本以整实例互斥锁串行化 | 与当前少量路由中间件需求不匹配, 仍需补充隔离和准入 |
| [Fiber 原生 cache v3.6.1](https://github.com/gofiber/fiber/tree/v3.6.1/middleware/cache) | 已支持 Cache-Control、Vary、方法/Host 分区和外接 Storage; 默认 Cookie 不分区, 探针复现用户响应串用和 SSE 被缓冲缓存; 未合并同键 miss | 可用于按其配置契约设计的 Fiber 应用, pkg 采用与 Gin 一致的更小策略 |
| CDN/反向代理 | 适合跨实例复用、完整 HTTP 新鲜度与重验证, 部署和规则由应用负责 | 大规模公共内容优先在该层处理 |

候选库仍有近期维护, 本次结论针对上述固定提交. 其 README 的旧 adapter Get/Set 基准不包括完整 HTTP 中间件、gob 编解码或满容量淘汰. 源码中的满载 LRU 淘汰会扫描 metadata, 热命中仍需 gob decode; 仓外实验也观察到容量增大时淘汰成本上升. 没有对所有方案进行同机、同负载端到端基准, 因此不能把这些结果解释为性能排名.

选型阶段快照的同进程 HTTP 热命中微基准使用 Windows/amd64、Go 1.27、AMD Ryzen 7 5700G, 复用请求、每轮新建 httptest Recorder, Content-Type 一致, 200ms 三次中位数. 旧/新 Gin 包含 Gin engine, 候选库直接使用 net/http:

| 正文 | 原 Gin 包装 ns/op | 新 Gin 中间件 ns/op | victorspringer ns/op |
| --- | ---: | ---: | ---: |
| 128 B | 816 | 2998 | 25331 |
| 1 KiB | 2180 | 5559 | 26356 |
| 4 KiB | 6038 | 5894 | 31203 |

128 B 场景的分配分别为 1128/1857/11155 B 和 11/21/225 次. 原包装执行的安全检查更少, 新实现的小响应热路径有额外成本. 该实验不含真实网络、冷 miss、并发回源或业务开销, 且与验收并行存在噪声; 不能推导生产吞吐或绝对排名. 测量快照包含 Host/多行请求头等隔离修复; 后续 Gin 慢响应年龄修正主要影响冷写入, 未重新测量此表.

最终复审针对新实现优化前后使用另一组固定夹具, 同机三轮中位数, 默认 GOMAXPROCS=16, 128 B 响应. Gin 每次独立 Recorder, Fiber 每个 goroutine 复用自有 fasthttp.RequestCtx; 两框架只适合各自前后比较, 与上表也不能直接拼接:

| 场景 | 复审前 ns/op | 复审后 ns/op | 分配次数前后 | 字节前后 B/op |
| --- | ---: | ---: | --- | --- |
| Gin GET | 2663 | 2103 | 20 → 14 | 1761 → 1537 |
| Gin GET, 8 个头 | 3778 | 2941 | 29 → 14 | 2257 → 1537 |
| Fiber GET | 2995 | 2498 | 18 → 10 | 1506 → 945 |
| Fiber GET, 8 个头 | 4945 | 4130 | 46 → 38 | 2259 → 1698 |
| Gin 并行 GET | 829 | 824 | 20 → 14 | 1698 → 1474 |
| Fiber 并行 GET | 947 | 756 | 18 → 10 | 1506 → 945 |
| Fiber POST, 64 KiB 请求体 | 71855 | 33149 | 19 → 10 | 67147 → 945 |

收益主要来自初始准入不再构造条目、规范 Gin 请求头同步只读、Key 栈摘要和 Fiber 去掉哈希前正文复制. Key 单独测量为 0 B/op、0 allocs/op. 单调 TTL 检查使存储 Get 从约 30ns 增至 55ns, 用于防止墙钟回拨后回放过期内容. Gin POST 4 KiB/64 KiB 的耗时中位数为 8890 → 10193ns / 68312 → 86204ns, 分配次数分别从 37/44 降至 31/38; 含 8 个头的对应计时也未加速. 计时受到 CPU/GC 波动影响, 前后采样窗口为 300ms/500ms, 本轮不宣称所有场景加速. 实验只验证本机微基准; 生产收益仍取决于业务计算和实际命中率.

可独立检查的候选源码证据:

- [请求键未包含真实 Request.Host 和 Method](https://github.com/victorspringer/http-cache/blob/f737c7a8ec8125e1b48fc59dd5fc942a1f25624b/cache.go#L594).
- [Cache-Control 仅读首行及 TTL 处理](https://github.com/victorspringer/http-cache/blob/f737c7a8ec8125e1b48fc59dd5fc942a1f25624b/cache.go#L500).
- [查询参数排序会改写 origin 输入](https://github.com/victorspringer/http-cache/blob/f737c7a8ec8125e1b48fc59dd5fc942a1f25624b/cache.go#L758).
- [内存淘汰扫描](https://github.com/victorspringer/http-cache/blob/f737c7a8ec8125e1b48fc59dd5fc942a1f25624b/adapter/memory/memory.go#L174) 和 [命中反序列化](https://github.com/victorspringer/http-cache/blob/f737c7a8ec8125e1b48fc59dd5fc942a1f25624b/cache.go#L732).

## 使用方式

两个框架的缓存 API 和配置字段一致, 仅导入路径及框架处理器类型不同. 以下示例分别保存为 `main.go` 即可编译.

Gin:

```go
package main

import (
	"log"
	"time"

	"github.com/gin-gonic/gin"

	cache "github.com/fufuok/pkg/web/gin/middleware"
)

// main 为公开目录和无副作用的查询分别注册缓存, 实际项目应替换示例数据源.
func main() {
	r := gin.New()
	r.GET("/catalog", cache.CacheByRequestURI(30*time.Second), func(c *gin.Context) {
		c.JSON(200, gin.H{"items": []string{"book", "pen"}})
	})
	r.POST("/search", cache.CacheByRawData(15*time.Second), func(c *gin.Context) {
		var query struct {
			Q string `json:"q"`
		}
		// 无效请求仍由业务返回错误, 非 200 响应不会缓存.
		if err := c.ShouldBindJSON(&query); err != nil {
			c.JSON(400, gin.H{"error": "invalid query"})
			return
		}
		c.JSON(200, gin.H{"query": query.Q, "items": []string{}})
	})
	if err := r.Run(":8080"); err != nil {
		log.Fatal(err)
	}
}
```

Fiber:

```go
package main

import (
	"log"
	"time"

	"github.com/gofiber/fiber/v3"

	cache "github.com/fufuok/pkg/web/fiber/middleware"
)

// main 展示与 Gin 相同的缓存入口, 原始请求正文仍保留给 Fiber 绑定器使用.
func main() {
	app := fiber.New()
	app.Get("/catalog", cache.CacheByRequestURI(30*time.Second), func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"items": []string{"book", "pen"}})
	})
	app.Post("/search", cache.CacheByRawData(15*time.Second), func(c fiber.Ctx) error {
		var query struct {
			Q string `json:"q"`
		}
		// 绑定失败返回框架错误, 缓存不会消费错误处理器生成的响应.
		if err := c.Bind().Body(&query); err != nil {
			return fiber.ErrBadRequest
		}
		return c.JSON(fiber.Map{"query": query.Q, "items": []string{}})
	})
	if err := app.Listen(":8080"); err != nil {
		log.Fatal(err)
	}
}
```

可选配置示例, Gin/Fiber 写法相同:

```go
cache.CacheByRequestURI(30*time.Second, cache.CacheConfig{
	Capacity:            128,
	MaxBodyBytes:        128 << 10,
	MaxRequestBodyBytes: 64 << 10,
	Vary:                []string{"X-Tenant"},
})
```

业务按额外请求头生成不同内容时, 应将该头加入 `Vary` 配置, 并由响应声明相应的 `Vary`. 以请求头区分租户的接口仍须先完成鉴权; 缓存键本身不验证租户身份.

## 默认值与内存预算

| 配置 | 默认值 | 行为 |
| --- | --- | --- |
| `ttl` | 10 分钟 | 非正值使用默认值, 命中不续期 |
| `Capacity` | 256 项 | 固定容量 LRU, 零值使用默认值 |
| `MaxBodyBytes` | 256 KiB | 超限响应继续发送, 不缓存 |
| `MaxRequestBodyBytes` | 64 KiB | 超限 POST 保持原始正文并透传 |
| 响应头预算 | 64 KiB/项 | 固定上限, 对整个响应头快照检查 |
| `Vary` | Accept、Accept-Encoding、Accept-Language、Origin | 用户配置追加并去重, 不删除默认维度 |

默认单实例缓存正文载荷最多约 64 MiB, 头字段内容预算最多约 16 MiB. 这些是条目载荷预算, 还需计算 LRU 表、Header map、字符串、在途请求和捕获缓冲区等开销, 不是进程 RSS 上限. 每次构造都会增加一个实例, 应按路由数量和响应大小调整配置. 负数正文限制及非法 `Vary` 名称会在构造时 panic.

不启动缓存清理 goroutine; 过期条目在访问或容量淘汰时回收. 内核独立用单调时钟复核有效期, 系统墙钟回拨不能延长响应 TTL. 同键 miss 先获得执行锁再复查缓存, 只有准入成功的结果可复用, 失败或私人响应各自执行业务. 等待锁不监听请求取消, 不提供单次回源超时或后台刷新; 应用应为处理器设置合理执行边界. 锁登记随最后一个持有者/等待者退出删除. Gin 请求在等待期间已被取消时直接返回 503 且不执行业务, 不会写出空 200.

领头请求的响应未准入 (no-store/private, Set-Cookie, 超限, 非 200, 业务错误或 panic 等) 时, 该键进入 hit-for-pass 直通窗口, 时长为 `min(ttl, 5s)`: 已在键锁上排队的请求立即放行并发执行业务, 窗口内新到达的请求也不再排队, 避免不可缓存接口被逐个串行执行. 5 秒与 nginx `proxy_cache_lock_timeout` 默认值同量级, 足以覆盖常见处理耗时, 又能让恢复可缓存的接口在数秒内重新合并回源. 窗口内若业务返回可准入响应, 照常写入缓存并立即结束窗口; 窗口到期后恢复单个领头请求回源. 直通标记存放在固定容量 `min(Capacity, 1024)` 的 LRU 中并惰性过期, 不随请求键增长, 被淘汰的键仅退化为等待领头请求结束后再并发执行. 可缓存响应仍保持同键单次回源.

## 请求隔离与准入

请求键包含方法, 协议, Host, 精确 URI, Content-Type, Content-Encoding 和已配置的表示请求头, POST 另加原始正文. 使用有长度边界的 SHA-256 摘要, 不保留原始大键, 不排序查询参数或规范化 JSON. 两个框架的 Host 与协议来源不同:

- Fiber: 同时包含原始 Host 和 Fiber 按可信代理配置解析的有效 Host (`c.Host()`), 协议取 `c.Scheme()`, 可信代理下会采用转发协议. 转发头的可信性由 Fiber 配置和部署保证.
- Gin: 只包含原始 `Request.Host`, 协议仅由 `Request.TLS` 判断, 不读取 `X-Forwarded-Proto`/`X-Forwarded-Host` 等转发头. 部署在终止 TLS 的反向代理之后时, 同一路由的 http/https 请求在缓存层都表现为 http; 业务若根据转发头生成绝对 URL, 重定向地址或其他内容, 必须把这些头 (如 `X-Forwarded-Proto`, `X-Forwarded-Host`) 加入 `Vary` 配置, 否则不同协议或主机的响应会互相复用.

下列请求直接透传: Authorization、Proxy-Authorization、Cookie、Upgrade、Range、标准 If-* 条件请求、请求 Cache-Control/Pragma、请求 Trailer、SSE Accept、GET 请求体以及不支持的方法. GET 只在 `ContentLength == 0` 且没有 Transfer-Encoding 时视为无正文, 声明长度, 未知长度或 chunked 的 GET 正文均透传; HTTP/2 无正文 GET 的 `Body` 虽非 `http.NoBody` 也可正常缓存. Fiber 请求体流直接透传; Gin 对 POST 正文有界预读并恢复已读前缀, 预读到 EOF 后复查 net/http 此时才合并的未声明请求 trailer, 存在即透传且不缓存. 响应流保持流式处理, Gin 的 Flush/Hijack 等连接操作也会禁用该次缓存; Fiber 接管连接 (`RequestCtx().Hijack`) 的响应同样不缓存.

只保存完整 200 响应. 响应含 Set-Cookie、认证挑战、Content-Range、连接级头、Trailer、SSE、`Vary: *` 或未配置的 Vary 维度时不保存. Cache-Control 的 private/no-store/no-cache、无效或非正 max-age/s-maxage 同样不保存; 正 max-age/s-maxage 只会缩短配置 TTL. 显式 Date、Age、Expires 或 Pragma 采用保守跳过策略.

保留一般端到端响应头, 包括 CSP、nosniff、Content-Location、分页 Link 和自定义业务元数据, 并补充向上取整的 Age, 避免下游将完整 max-age 重新计时. Content-Type、Content-Encoding、缓存控制和摘要等表示头始终与缓存正文一致; 其他头在外层当前请求已设置同名字段时保留当前值, 包括显式空值. Content-Length 由框架根据实际正文生成; 外层中间件预设的长度头在命中回放时会被清除 (含非规范大小写), 并按缓存正文重写.

不回放 CORS、请求 ID、追踪字段、Server-Timing、限流额度等已知逐请求动态头, 这些应在缓存之前生成. 其他自定义逐请求头也应放外层, 命中跳过的业务不能负责生成它们. 带动态 nonce 的 CSP 与页面正文需要成对生成, 应由应用选用适合共享的响应或跳过该路由缓存. Gin 从首次提交响应头计入年龄, 完整正文生成期间已耗尽 TTL 时不再保存; 首次提交头之后生成的 Trailer 同样不保存. 未主动增加 `public`、`max-age` 或 `X-Cache`.

## 中间件顺序

鉴权、日志、统计、限流、请求 ID 和 CORS 应位于缓存外层, 这样命中请求也会经过这些逻辑. 缓存只适用于显式选择的路由, 不建议直接覆盖整个应用.

- Gin: 按 `鉴权/日志等 -> 缓存 -> gzip -> 业务` 注册, 让缓存捕获实际压缩后的字节并按 Accept-Encoding 分区. 已在外层开启响应编码时会透传, 避免重复压缩. 注意 gin-contrib/gzip 等压缩中间件若注册在缓存外层 (例如全局 `r.Use(gzip.Gzip(...))` 后再给路由挂缓存), 进入缓存前已设置 Content-Encoding, 每个接受压缩的请求都会绕过缓存, 实际只有不压缩的请求能命中.
- Fiber: 推荐 `鉴权/日志等 -> compress -> 缓存 -> 业务`, 与 pkg engine 已注册 compress 的顺序一致. 此时缓存保存未压缩正文, 每次发送由外层 compress 处理. 原生适配也覆盖缓存位于 compress 外层的完整响应场景.
- 流式/SSE/文件响应使用原处理器. Fiber 超时工作协程未结束时不读取活动响应, 不把超时结果缓存; 接管连接 (Hijack) 的响应不缓存, 避免回放源站未实际发送的内容.

本实现不提供 Redis、跨进程失效、CDN 控制面、SWR、条件重验证或完整 RFC 9111 状态机. 需要这些能力时应选择专门的 HTTP 缓存层, 或在业务数据缓存层定义更新与失效规则.

## 从旧 Gin 接口迁移

这是允许破坏性的接口调整:

- `CacheByRequestURI(ttl, forgetTimeout, ...cache.Option)` 改为 `CacheByRequestURI(ttl, ...CacheConfig)`.
- `CacheByRawData(ttl, forgetTimeout, ...cache.Option)` 改为 `CacheByRawData(ttl, ...CacheConfig)`, 仅缓存只读 POST, 且键包含 URI 和表示维度.
- 删除 `CacheByRequestPath`, 不再提供忽略查询参数的快捷缓存. 旧 Path 包装实测可将不同查询值混用, 应迁移到完整 URI 缓存.
- 删除 `forgetTimeout` 和三方 `cache.Option`, 同键回源合并默认启用, 不按超时强制放开第二次并发回源. 领头响应未准入时按上文的 `min(ttl, 5s)` 直通窗口放开并发, 不可缓存接口不会被串行化.
- 旧代码常把 gin-contrib/gzip 注册为全局中间件. 迁移后若压缩仍位于缓存外层, Content-Encoding 检查会让所有压缩请求绕过缓存. 须把缓存注册在压缩外层 (例如去掉全局 gzip, 在路由上按 `缓存 -> gzip -> 业务` 注册), 或把压缩移到反向代理.
- 原先依赖缓存库提供 Redis、自定义状态码或任意键策略的调用方需要自行选择对应方案.

旧包装的隔离探针还复现了同实例 RawData 缓存跨 URI 复用正文、URI 缓存跨 Host 复用的行为. 新接口有意收紧这些边界, 不保证旧缓存命中率保持不变.
