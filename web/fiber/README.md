# Fiber 封装

当前适配 Fiber v3.6.1. `engine.Run` 和 `RunGroups` 先装配恢复/压缩中间件, 再执行应用 setup; `SetupExceptionRouter` 应最后注册.

公开 GET 与只读 POST 的响应缓存见 [HTTP 缓存说明](../README.md). 缓存为路由显式注册, 不由 engine 全局启用.

## 启动信息与临时消息

HTTP/HTTPS 监听均设置 `DisableStartupMessage: true`, 不显示 Fiber 启动欢迎信息. pkg 的服务启动日志仍按日志配置输出.

`DisableFlashMessages` 是 v3.6 新增的重定向临时消息开关, 默认 false, 与欢迎信息无关. 它影响 `Redirect().With`、`WithInput` 和 `fiber_flash` cookie. pkg 保留上游默认行为.

## 请求体上限

`web_conf.body_limit` 只用于 Fiber, 单位为字节. 正数作为明确上限, 0、-1 和其他负数统一回退 `config.WebBodyLimit`, 默认 8MiB. 不再约定 -1 无限; Fiber v3.5/v3.6 本身都将非正数回退为4MiB, pkg 在传入上游前统一默认值.

分组配置中, `body_limit=0` 表示沿用主配置的上限, 正数覆盖主配置, 负数回退 `config.WebBodyLimit`.

默认值和边界由配置层处理: 主配置加载使用 `parseWebConfig`, 程序传入的覆盖配置使用 `NormalizeWebConf`, 分组经 `NormalizeWebGroupConf` 复用同一归一化规则. 引擎直接使用已处理的 `BodyLimit`, 不重复补默认值.

```json
{
  "web_conf": {
    "body_limit": 67108864
  }
}
```

以上允许最大64MiB请求体. 上传文件、multipart边界和普通表单字段共用该预算, 文件大小应为其他表单内容留余量; 超出限制返回413. 下载响应、`SendFile` 和代理响应不受该请求体设置限制. 大文件上传应按业务设置明确预算, 不用极大数值模拟无限.

Gin 封装当前没有使用该字段, 不因本次 Fiber 适配增加 Gin 请求体限制. Gin 的 `MaxMultipartMemory` 是 multipart 内存阈值, 超出部分可落临时文件, 不是上传总大小限制; 如需限制 Gin 请求体, 应另行使用 `http.MaxBytesReader` 等明确实现.

BodyLimit、监听地址、TLS 和代理信任配置在创建 App 时生效, 修改后需要重启相应服务.

## 代理 IP 验证

Gin/Fiber 共用 `trusted_proxies` 这一份可信代理 IP/CIDR 配置. Fiber 的两个开关职责独立:

| 配置项 | 作用 |
| --- | --- |
| `enable_trusted_proxy_check` | 检查直接连接来源是否可信, 决定是否采用代理传来的 IP、Host 和协议等信息 |
| `enable_ip_validation` | 校验代理头中的 IP, 并从多级 XFF 链中提取单个客户端 IP |

pkg 原有 Fiber 封装已经支持可信来源检查和列表, 本次补齐 IP 校验开关映射. Gin 的常规 `ClientIP()` 路径内置来源检查和代理链解析, 无需这两个独立开关; `trusted_platform` 是 Gin 优先读取的平台 IP 头, 该头须由受控入口保证可靠.

使用 X-Forwarded-For 多级代理链作为日志或名单的单一客户端标识时, 配置:

```json
{
  "web_conf": {
    "proxy_header": "X-Forwarded-For",
    "enable_trusted_proxy_check": true,
    "enable_ip_validation": true,
    "trusted_proxies": ["192.0.2.10/32"]
  }
}
```

把示例地址替换为实际代理地址或网段. `enable_ip_validation` 默认 false, 保持旧配置兼容; 开启后验证地址并从右向左跳过可信代理, 选取第一个不可信地址. 未启用时 XFF 可能返回整串, 不能直接按单IP匹配名单. 未受信来源或未启用可信代理检查时, 当前 Fiber v3 忽略代理头并使用连接来源IP.

分组沿用 WebConf 的布尔继承规则: 主配置为 true 时, 分组零值 false 表示继承, 不用于关闭主配置的选项. Gin 忽略这些 Fiber 专属开关.

## 超时与日志

`WithTimeout` 沿用新版默认503. 真实截止使用独立文本响应 `Service Unavailable`; 业务立即返回 deadline 等超时错误时仍经过应用 ErrorHandler, 可以得到 JSON. 回包不强制结束业务, Handler 应监听 `c.Context().Done()` 合作退出. 收到取消时应返回保存的 `ctx.Err()`, 不要吞掉取消错误后返回 nil; 完成与截止同时就绪时, 上游可能选中完成分支.

WebLogger 遇到已捕获的真实超时响应时, 不主动记录日志, 直接返回原错误, 跳过客户端IP、方法、URL、正文及自定义日志条件, 避免与尚未结束的 worker 竞争访问 Ctx. Fiber 此时也会跳过应用 ErrorHandler; 应用如需记录, 应在 timeout 外层通过 `c.Next()` 接收错误, 只使用错误、入口耗时和请求进入时提前复制的快照, 不再读取活动 Ctx. 正常完成路径由 `cond` 决定是否记录; 没有真实截止标记的错误仍沿用原错误日志分支, 不受 `cond` 控制, Gin 的错误日志也独立于其条件函数. `c.Elapsed()` 是从服务器开始处理请求计时, 用途不同.

`WithTimeout -> WebLogger ->业务` 顺序会让 WebLogger 在业务退出时返回; 若已捕获真实截止响应, 仍按上述规则不主动记录日志, 且外层 timeout 可能已结束等待, 不保证继续处理这个迟到错误. 此顺序不保证任意自定义中间件或嵌套 timeout 安全. 若并发限制器要限制实际执行中的业务, 也应放在 timeout 内部.

## HTTPS 与代理路径

同一 App 的多个 HTTPS 地址共享一份证书和 TLSHandler, 关闭连接后由同一状态持有者清理握手记录. setup 只执行一次, TLS最低版本保持1.2. 最终配置实际启动 HTTPS 监听时, 证书加载失败直接退出. 常规配置解析阶段发现证书文件不存在时, 仍沿用原行为关闭 HTTPS 地址, HTTP 按原配置运行.

`tproxy.ProxyHandler` 接收管理员配置的固定可信 HTTP(S) 上游, 保留私网、IPv6和跨协议转发. 路径沿用新版 RFC3986规范化和默认安全检查, 查询串保留原编码. 编码斜杠、重复斜杠、反斜杠及 Fiber 无法确认自定义客户端保留的 `%3B`、`%40`、`%3A` 等路径可返回400, 不放宽该限制以兼容旧版.

新增 `SetLocal` 和 `StatusCode` 已用于等价场景. 其他新增能力可直接通过 `fiber.Ctx` 或 setup 收到的 `*fiber.App` 使用, pkg 不增加重复包装.

上游依据: [v3.6.1 配置说明](https://github.com/gofiber/fiber/blob/v3.6.1/docs/api/fiber.md) 和 [代理路径策略](https://github.com/gofiber/fiber/blob/v3.6.1/docs/middleware/proxy.md).
