# HTTP 响应缓存

Gin 和 Fiber 提供相同的两个入口, 共用进程内有界 LRU, 无需在业务层维护响应缓存:

| API | 用途 |
| --- | --- |
| `CacheByRequestURI(ttl, ...CacheConfig)` | 无请求体的公开 GET, 保留完整 URI 和查询参数顺序 |
| `CacheByRawData(ttl, ...CacheConfig)` | 应用明确声明为只读查询的 POST, 在完整请求键中加入原始正文 |

每次构造中间件创建独立缓存, 也可将同一实例注册到路由组. 缓存命中会跳过下游处理器, 因此 RawData 不得用于写操作、支付或其他有副作用的接口. HEAD 和其他方法直接透传.

头名约定: Gin 的响应头直接写入 `http.Header` 时必须使用规范名; `unsafeResponse` 遇到非规范响应头会拒绝该响应, 因此该响应不存储也不会作为命中返回. Fiber 的响应头会转换为 `http.Header` 后按同一规则检查; 设置 `DisableHeaderNormalizing: true` 禁用头名规范化时, 请求侧凭据与隐私字段仍按名称大小写无关方式识别, 但应用设置的非规范表示头不保证被清理或替换, 在进入缓存前设置也可能造成冷热差异. 应用应始终使用规范头名. 同名请求头的全部字段行参与隐私检查及缓存分区.

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

2026-10-27 留存的热命中同机交替基准 (Windows/amd64, Go 1.27.0, AMD Ryzen 7 5700G, `-test.cpu 1`, 每轮 300ms, 正反顺序交替 10 轮取中位数, 1 KiB 正文, 12 个浏览器请求头). 同一份夹具分别编译为测试二进制; 对照版本只回退当时的键编码、策略检查和 Fiber 头收集三项优化, 其余修复与依赖相同. 每轮原始输出、夹具、overlay 和 SHA-256 均已留存; 以下为该夹具的历史数据, 不推导后续超时修复或 Set 头共享的性能收益.

| 场景 | 对照 ns/op | 当时优化后 ns/op | B/op 对照 → 优化后 | 分配次数对照 → 优化后 |
| --- | ---: | ---: | ---: | ---: |
| Gin 热命中 | 1143 | 848.6 | 32 → 32 | 1 → 1 |
| Gin 热命中, 代理改写 `URL.Scheme` | 1141.5 | 862.95 | 32 → 32 | 1 → 1 |
| Fiber 热命中 | 3157 | 2640 | 960 → 1008 | 13 → 7 |

Gin 夹具复用请求与只计数、不保存正文的 writer, Fiber 复用自有 fasthttp.RequestCtx; 不含请求构造、真实网络、冷 miss 或业务耗时. 因此数字仅支持各框架同夹具前后比较, 不能作为全请求成本或候选库排名. Fiber 共享头值数组减少小分配, 本夹具的总分配字节反而增加 48 B, 不宣称所有内存指标下降. 旧包装缺少本次隔离规则, 不作为等价性能对照.

Key 使用栈缓冲和零拷贝摘要写入; SkipRequest 用 Cut 解析列表; CanServe 以 EqualFold 匹配指令; Fiber 只收集相关请求头. `TestHitPathDoesNotAllocate` 锁定所覆盖输入的零分配, 包含大写指令、媒体类型和超长头值. 它不承诺任意响应 Vary 值的规范化都不分配: 非规范大小写的维度名可能分配; 配置超过 64 字节的 Vary 头名时, Fiber 识别长字段名也可能分配. Gin 的有效协议使用独立 `EffectiveScheme` 字段, `TestGinCacheRequestDoesNotAllocate` 覆盖非空 URL.Scheme.

仓库内可直接复跑的正式夹具与上述仓外夹具不同:

```shell
go test -mod=readonly ./web/gin/middleware -run '^$' -bench '^BenchmarkGinResponseCacheHit$' -benchmem -count=5
```

同日留存的正式 Gin 夹具五轮中位数 4582 ns/op、7109 B/op、18 allocs/op, 耗时范围 3507–4827 ns/op, GOMAXPROCS=16. 它包含 ServeHTTP、Recorder 与请求对象构造, 没有等价旧版对照, 不据此推导优化幅度. Ubuntu 真机基准未执行; WSL race 验证与性能基准是不同证据.

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
| `Capacity` | 256 项 | 固定容量 LRU, 零值使用默认值; 单实例上限 65536, 超出时构造 panic |
| `MaxBodyBytes` | 256 KiB | 超限响应继续发送, 不缓存 |
| `MaxRequestBodyBytes` | 64 KiB | 超限 POST 保持原始正文并透传 |
| 响应头预算 | 64 KiB/项 | 固定上限, 对整个响应头快照检查 |
| `Vary` | Accept、Accept-Encoding、Accept-Language、Origin、Forwarded、X-Forwarded-Host、X-Forwarded-Proto | 用户配置追加并去重, 不删除默认维度; 转发头默认分区, 防止应用按其生成的绝对地址被单个伪造请求写入共享缓存 |

默认单实例缓存正文载荷最多约 64 MiB, 头字段内容预算最多约 16 MiB. freelru 会按 `Capacity` 立即预分配主表, 64 位平台上限 65536 项的空主表约占 4.75 MiB; 构造前拒绝更大的值, 不静默截断. 这些预算还未计入 Header map、字符串、在途请求和捕获缓冲区, 不是进程 RSS 上限. `Prepare` 在复制待存条目前检查头预算, Fiber 适配层构造独立头快照时仍可能先复制, 因此它也不是临时快照的内存上限. 每次构造都会增加一个实例, 应按路由数量和响应大小调整配置. 负数正文限制及非法 `Vary` 名称会在构造时 panic.

共享内核的所有权约定: `Prepare` 为响应头创建独立 map 和值切片, `OmitInherited` 只能在首次 `Set` 前执行. 首次发布后 Header 冻结, 不得修改 map 或值切片; 同一冻结 Header 可发布到多个键, `Set` 不再重复克隆. `Set` 为每个键创建独立存储条目并紧凑复制 Body, 不接管 Gin 捕获缓冲区及其多余容量. `Get` 返回的头与正文只读, 框架回放时复制到当前响应, 不让池化缓冲区污染缓存.

不启动缓存清理 goroutine; 过期条目在访问或容量淘汰时回收. 内核独立用单调时钟复核有效期, 系统墙钟回拨不能延长响应 TTL. 同键 miss 先获得执行锁再复查缓存, 只有准入成功的结果可复用, 失败或私人响应各自执行业务. 等待锁不监听请求取消, 不提供单次回源超时或后台刷新; 应用应为处理器设置合理执行边界. 锁登记随最后一个持有者/等待者退出删除. Gin 请求在等待期间已被取消时直接返回 503 且不执行业务, 不会写出空 200. 键锁记录持有请求的上下文: Gin `HandleContext`, Fiber `RestartRouting` 或同一实例在路由组与路由上重复注册, 使同一请求再次进入同键缓存时直接透传, 不在自己持有的锁上等待.

领头请求的响应未准入 (no-store/private, Set-Cookie, 超限, 非 200, 业务错误或 panic 等) 时, 该键进入 hit-for-pass 直通窗口, 时长为 `min(ttl, 5s)`: 已在键锁上排队的请求立即放行并发执行业务, 窗口内新到达的请求也不再排队, 避免不可缓存接口被逐个串行执行. 5 秒与 nginx `proxy_cache_lock_timeout` 默认值同量级, 用于避免频繁回到等待领头的状态; 窗口结束后恢复合并, 已放行的请求仍可能并发完成. 窗口内若业务返回可准入响应, 照常写入缓存并立即结束窗口; 窗口到期后恢复单个领头请求回源. 直通标记存放在固定容量 `min(Capacity, 1024)` 的 LRU 中并惰性过期, 不随请求键增长, 被淘汰的键仅退化为等待领头请求结束后再并发执行. 正常可缓存冷请求合并为同键单次回源; 直通窗口内已经放行的请求, 即使随后恢复可缓存也可能同时完成. Gin 领头请求因客户端断开 (`context.Canceled`) 而没有结果时不登记直通, 由下一位等待者接任领头, 单个断开的客户端不会让全部排队请求同时回源; 超时等其他结果仍按未准入处理. 取消状态以进入缓存时的请求上下文为准, 内层中间件替换并在返回时 cancel 的派生上下文不影响缓存与直通.

## 请求隔离与准入

请求键包含方法, 协议, Host, 精确 URI, 框架路由实际使用的路径与查询 (Gin `URL.Path`/`URL.RawQuery`, Fiber `c.Path()`/URI 查询串), Content-Type, Content-Encoding 和已配置的表示请求头, POST 另加原始正文. 外层按未进入键的输入 (如 User-Agent) 改写路径或查询时, 不同改写结果分别缓存; 未改写时路径与查询由 URI 唯一确定, 不额外拆分缓存. 使用有长度边界的 SHA-256 摘要, 不保留原始大键, 不排序查询参数或规范化 JSON. 两个框架的 Host 与协议来源不同:

- Fiber: 同时包含原始 Host 和 Fiber 按可信代理配置解析的有效 Host (`c.Host()`), 协议取 `c.Scheme()`, 可信代理下会采用转发协议. 转发头的可信性由 Fiber 配置和部署保证.
- Gin: 键包含原始 `Request.Host`, `Request.TLS` 判定的协议, 以及代理中间件改写后的 `URL.Scheme`; `Forwarded`, `X-Forwarded-Host`, `X-Forwarded-Proto` 默认进入键. 其他业务依赖的转发头仍需加入 `Vary` 配置, 转发头是否可信由代理部署保证.

转发头进入键只能隔离不同的代理表示, 不验证其可信性. 客户端直连时, 变化这些取值会增加缓存键基数并降低命中率, 也可能增加回源负载; 应由入口代理覆盖或移除非可信转发头. 同一公开表示使用稳定的转发头值时不会额外分区. 键计算为此多查 3 个请求头, 不据此承诺固定纳秒开销.

下列请求直接透传: Authorization、Proxy-Authorization、Cookie、Upgrade、Range、标准 If-* 条件请求、请求 Cache-Control/Pragma、请求 Trailer、SSE Accept、GET 请求体以及不支持的方法. GET 只在 `ContentLength == 0` 且没有 Transfer-Encoding 时视为无正文, 声明长度, 未知长度或 chunked 的 GET 正文均透传; HTTP/2 无正文 GET 的 `Body` 虽非 `http.NoBody` 也可正常缓存. Fiber 请求体流直接透传; Gin 对 POST 正文有界预读并恢复已读前缀, 预读到 EOF 后复查 net/http 此时才合并的未声明请求 trailer, 存在即透传且不缓存. 响应流保持流式处理, Gin 的 Flush/Hijack 等连接操作也会禁用该次缓存; Fiber 接管连接 (`RequestCtx().Hijack`) 的响应同样不缓存.

只保存完整 200 响应. 响应含 Set-Cookie、认证挑战、Content-Range、连接级头、Trailer、SSE、`Vary: *` 或未配置的 Vary 维度时不保存. Cache-Control 的 private/no-store/no-cache、无效或非正 max-age/s-maxage 同样不保存; 正 max-age/s-maxage 只会缩短配置 TTL. 显式 Date、Age、Expires 或 Pragma 采用保守跳过策略.

保留一般端到端响应头, 包括 CSP、nosniff、Content-Location、分页 Link 和自定义业务元数据, 并补充向上取整的 Age, 避免下游将完整 max-age 重新计时. 命中时先清除外层预设的表示头, 再回放缓存中的 Content-Type、Content-Encoding、缓存控制和摘要等字段; 缓存中没有的表示头也保持缺失. 其他头在外层当前请求已设置同名字段时保留当前值, 包括显式空值. 外层在缓存之前已设置且业务未改动的非表示头不进入条目, 命中时由外层为当前请求重新设置, 不会把按访客条件设置的值 (如仅对内部 UA 添加的调试头) 回放给其他访客. Gin 命中时清除预设 Content-Length, 不按缓存正文强写长度, 由最终 writer 根据实际写出的正文定界.

不回放 CORS、请求 ID、追踪字段、Server-Timing、限流额度等已知逐请求动态头, 这些应在缓存之前生成. 其他自定义逐请求头也应放外层, 命中跳过的业务不能负责生成它们. 带动态 nonce 的 CSP 与页面正文需要成对生成, 应由应用选用适合共享的响应或跳过该路由缓存. Gin 从首次提交响应头计入年龄, 完整正文生成期间已耗尽 TTL 时不再保存; 首次提交头之后生成的 Trailer 同样不保存. 未主动增加 `public`、`max-age` 或 `X-Cache`.

## 中间件顺序

鉴权、日志、统计、限流、请求 ID 和 CORS 应位于缓存外层, 这样命中请求也会经过这些逻辑. 缓存只适用于显式选择的路由, 不建议直接覆盖整个应用.

会话类中间件是例外: 业务依赖会话渲染的正文 (如 CSRF token) 只对该访客有效. Gin 的头快照取自业务首次写出之前 (与记录的正文一致, 不含外层压缩在提交时加入的 Content-Encoding 或逐访客头), 存储前再以实时头复核, 对于已通过捕获 writer 写出或显式提交的响应, 外层 writer 在该次提交时加入的 Set-Cookie 或私有指令会使本次响应不缓存; 但任何框架里, 外层在 `Next` 返回之后才写入的 Set-Cookie 或缓存控制 (如 Fiber session 中间件在 `Next` 之后保存会话) 都晚于缓存保存, 准入无法看到. 这类中间件必须注册在缓存内层, 或者该路由不启用缓存. Gin 业务调用 `Unwrap` (含 `http.ResponseController` 设置截止时间等) 后可能绕过包装写出, 记录的正文不再可信, 本次响应不缓存. Gin `HandleContext` 会把 `c.Writer` 重置为底层 writer, 正文未经缓存记录即提交的响应同样不缓存. 声明的 Content-Length 与记录正文不符 (写出中途失败且忽略错误的截断响应), 或首次写出后又改写已有表示头 (缓冲型外层 writer 会以最终头提交) 时, 本次响应也不缓存.

**Gin 空响应的缓存边界.** 仅设置响应头和状态、未写出也未显式提交的响应不存储, 避免 Gin 自动提交时才出现的 Cookie 或私有指令漏过检查. `WriteHeaderNow` 或零字节 Write 经过捕获路径后, 公开空响应仍可缓存. 缓存不为准入而提前提交, 外层仍可在返回后修改尚未提交的响应. 仅设置头的公开接口相比旧行为会增加业务执行次数.

- Gin: 按 `鉴权/日志等 -> 缓存 -> gzip -> 业务` 注册, 让缓存捕获实际压缩后的字节并按 Accept-Encoding 分区. 已在外层开启响应编码时会透传, 避免重复压缩. 注意 gin-contrib/gzip 等压缩中间件若注册在缓存外层 (例如全局 `r.Use(gzip.Gzip(...))` 后再给路由挂缓存), 进入缓存前已设置 Content-Encoding, 每个接受压缩的请求都会绕过缓存, 实际只有不压缩的请求能命中.
- Fiber: 推荐 `鉴权/日志等 -> compress -> 缓存 -> 业务`, 与 pkg engine 已注册 compress 的顺序一致. 此时缓存保存未压缩正文, 每次发送由外层 compress 处理. 原生适配也覆盖缓存位于 compress 外层的完整响应场景.
- 流式/SSE/文件响应使用原处理器. 按下述推荐顺序使用时, Fiber 超时工作协程未结束不会读取活动响应, 不把超时结果缓存; 接管连接 (Hijack) 的响应不缓存, 避免回放源站未实际发送的内容.

Fiber 响应缓存须注册在 pkg `WithTimeout` 外层, 推荐 `WebLogger -> 缓存 -> 唯一 WithTimeout -> 业务`: 命中不启动超时工作协程; miss 时缓存在请求协程中待超时中间件返回后, 先检查错误, 入口 context 和超时标记 (由同一请求协程写入), 真实截止时不读取响应也不缓存; Fiber 原生 `timeout.New` 的 OnTimeout 在截止分支写出兜底并返回 nil 时同样由标记识别. 外层普通中间件设置的同步 context 期限不影响这一判断, 已到期时同样不缓存. `WithTimeout` 在启动工作协程前用私有类型键调用 `SetLocal`, 内层缓存只在入口识别该标记并整段透传, 不命中也不存储. 全局 `WithTimeoutDefault` 位于路由缓存之前时, 路由缓存同样全部透传. 该标记只由本包 `WithTimeout` 设置; Fiber 原生 `timeout.New` 或第三方异步 timeout 注册在缓存外层时不受保护. 支持的异步超时契约为单层, 不包含异步 timeout 嵌套或业务自行启动协程访问 Ctx.

`WithTimeout` 的工作协程保存入口 context: 业务返回非 nil 错误时原样返回, 返回 nil 但 context 已截止或被上游取消时改为 `fiber.ErrServiceUnavailable`, 默认 ErrorHandler 下完成分支为 503, 不会把取消后的残缺结果当作成功; 自定义 ErrorHandler 仍可改变完成分支的输出, 不保证与独立截止响应相同. 业务显式返回 `context.Canceled` 时保留原错误, 完成分支交给 ErrorHandler (默认 500); 需要统一为超时响应时通过 `timeoutErrors` 注册 `context.Canceled`. 自定义超时错误按错误链匹配, 构造时用 `slices.Clone` 保存 `timeoutErrors` 切片快照, 调用方随后改写原切片不影响匹配. 时长非正时完全禁用期限, 同步执行, 不设置标记, 也不改写返回值. 原生 `timeout.New` 没有这一规范化: 完成分支上返回 nil 的取消结果, 或 OnTimeout 在完成分支返回 200/nil 的兜底, 都与正常成功响应无法区分, 应返回错误或设置 `Cache-Control: no-store`. 标记在 `WithTimeout` 返回后仍保留至请求回收, 其后在请求协程上 RestartRouting 再次经过的缓存同样透传. WebLogger 同样应位于 timeout 外侧, 相反顺序仍有已复现的标记读取竞态.

Fiber v3.6.1 的默认截止路径构造独立 503 响应并跳过应用 ErrorHandler, 不改写活动响应. 自定义 OnTimeout 若与尚未结束的业务同时操作活动响应, 不在并发安全保证内. 只通过其他协程调用 fasthttp `TimeoutError` 系列, 而不以 context 期限表达超时的自定义实现同样不在支持范围.

本实现不提供 Redis、跨进程失效、CDN 控制面、SWR、条件重验证或完整 RFC 9111 状态机. 需要这些能力时应选择专门的 HTTP 缓存层, 或在业务数据缓存层定义更新与失效规则.

## 从旧 Gin 接口迁移

这是允许破坏性的接口调整:

- `CacheByRequestURI(ttl, forgetTimeout, ...cache.Option)` 改为 `CacheByRequestURI(ttl, ...CacheConfig)`.
- `CacheByRawData(ttl, forgetTimeout, ...cache.Option)` 改为 `CacheByRawData(ttl, ...CacheConfig)`, 仅缓存只读 POST, 且键包含 URI 和表示维度.
- 删除 `CacheByRequestPath`, 不再提供忽略查询参数的快捷缓存. 旧 Path 包装实测可将不同查询值混用, 应迁移到完整 URI 缓存.
- 删除 `forgetTimeout` 和三方 `cache.Option`, 同键回源合并默认启用, 不按超时强制放开第二次并发回源. 领头响应未准入时按上文的 `min(ttl, 5s)` 直通窗口放开并发, 不可缓存接口不会被串行化.
- Gin 不再缓存仅设置头/状态但未经过捕获路径写出的响应; 已显式提交的公开空响应仍可缓存.
- 旧代码常把 gin-contrib/gzip 注册为全局中间件. 迁移后若压缩仍位于缓存外层, Content-Encoding 检查会让所有压缩请求绕过缓存. 须把缓存注册在压缩外层 (例如去掉全局 gzip, 在路由上按 `缓存 -> gzip -> 业务` 注册), 或把压缩移到反向代理.
- 原先依赖缓存库提供 Redis、自定义状态码或任意键策略的调用方需要自行选择对应方案.

旧包装的隔离探针还复现了同实例 RawData 缓存跨 URI 复用正文、URI 缓存跨 Host 复用的行为. 新接口有意收紧这些边界, 不保证旧缓存命中率保持不变.

上述 Gin 接口已在 v1 系列发布, 因而这次调整是破坏性升级, 不能因为版本改为 v1.4.0 就视为兼容. 按 Go 模块语义化版本, 保留这些改动应发布 v2 并同步模块和导入路径; 若维持 v1 兼容, 应保留旧接口并另增入口. 当前工作区仍为原模块路径, 本文不表示已执行版本或路径迁移.
