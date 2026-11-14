# gjson

源自 [tidwall/gjson](https://github.com/tidwall/gjson) v1.20.3 (该版唯一的代码改动 `parseUint` 未采纳, 见下表), 包路径改为 `github.com/fufuok/pkg/xjson/gjson`, 依赖本仓库的 `xjson/match` 与 `xjson/pretty`.

## 对上游的修正

以下修正在同步上游时必须保留, 每项都有回归测试.

| 位置 | 上游问题 | 本地修正 | 回归测试 |
| --- | --- | --- | --- |
| `parseUint` | v1.20.2 用 `next < n` 判断溢出, 漏掉乘 10 回绕后仍大于旧值的情况, 如 `25000000000000000000` 的 `Int()`/`Uint()` 得到 `6553255926290448384`. v1.20.3 (Issue 404) 改为: 20 字节输入按字典序与 `"18446744073709551615"` 比较, 大于即返回最大值, 且不检查是否全为数字. 于是 `"2026-11-14T08:00:00Z"`、`"hello world 12345678"`、`"2.000000000000000000"` 等 20 字节字符串的 `Int()`/`Uint()` 都得到类型最大值 | 不同步 v1.20.3 的 `parseUint`, 保留本地实现: 乘加前与 `MaxUint64/10` 比较, 所有溢出都饱和为最大值, 含非数字字符即判为非整数 | `TestIntOverflowWrapMiss`, `TestParseUintNotLexical`; 上游 `TestIssue404` 已迁移并通过 |
| `f2i`, `f2u`, `safeInt` | NaN 直接转换为整数, 结果随 CPU 架构不同. amd64 上 `"NaN"` 的 `Int()` 为 MinInt64 | NaN 统一返回 0 | `TestIntStringFallback` |
| `Valid`, `ValidBytes` | 递归校验没有深度上限, 约 600 万层 `[` (6MB 输入) 即耗尽 1GB 栈, 进程直接退出, 无法 recover | 嵌套超过 10000 层返回 false, 与 `encoding/json.Valid` 一致 | `TestValidNestingDepth` |
| 自定义修饰符表 (`AddModifier`/`getModifier`) | v1.20.0 起用 `sync.RWMutex` 保护. 以 `@` 开头的普通键 (如 ES 日志的 `@timestamp`) 每次查询都会先查修饰符表并加读锁, 16 核并发查询时约 26ns, v1.4.2 为 8.6ns | 写时复制: `atomic.Pointer` 无锁读取, `AddModifier` 复制整表后原子替换. 注册仍可与查询并发 | `TestUserModifierCopyOnWrite`, `TestAddModifierSafety`, `BenchmarkAtKeyParallel` |
| `Int()`/`Uint()` 字符串回退 | v1.19.1 起整数解析失败后无条件调用 `strconv.ParseFloat`, 其错误对象会复制整个输入: `""`、`"N/A"`、`"null"` 每次 1~2 次分配 (约 3ns 变为 50ns), 4KB 文本每次复制 4KB. 另外 `ErrRange` 也被当作失败, 字符串 `"1e400"` 得 0, 而数字 `1e400` 得 `MaxInt64` | 先按字符集、符号位置、小数点个数等必要条件排除不可能是数值的串 (含日期、时间戳、IP、版本号、UUID 这类以数字开头的字段), 不再分配; 超出 float64 范围按 ±Inf 饱和, 与数字类型一致 | `TestIntStringNoAlloc`, `TestIntStringRange`, `TestMayParseFloat` |
| 数组下标 (`parseArray`) | `int(n)` 在 32 位平台截断, `GOARCH=386` 下 `Get("[10,20,30]", "4294967296")` 命中首个元素 | 超出 `int` 的下标视为不命中 | `TestHugeIndexNoWrap` (32 位需 `GOARCH=386 go test` 复现) |
| 查询单匹配分支 (`procQuery`) | `#(...)` 单匹配后再取多结果 (如 `a.#(b==1).c.#.d`、JSON Lines `..#(id==3).t.#.v`) 时, `Indexes` 相对匹配元素而非原文, `Paths` 得到 `[a.3]` 这类错误路径, sjson 据此写到第 1 个元素甚至字符串内部. 上游只在 `#(...)#` 分支加了元素偏移 | 单匹配分支同样加上匹配元素的偏移 | `TestQueryFirstMatchIndexes` |

## 本地保留

- 非法 UTF-8 在 `AppendJSONString` 等编码路径输出 `\ufffd` 转义. 上游 v1.19.1 起改为原样写入 U+FFFD 字节, 两种写法都是合法 JSON, 反序列化结果相同. 本地保持历史输出, 测试中对比 Go 标准库时只接受这一处差异.

## 已移除的 API

上游 v1.20.0 已让以下全局变量失效, 本地直接删除, 不保留兼容. 应用升级时编译报错即需迁移:

| 已删除 | 替代方式 |
| --- | --- |
| `DisableEscapeHTML = true` | `gjson.SetEscapeHTML(false)`, 并发安全, 可在运行期切换 |
| `DisableModifiers = true` | 编译参数 `-ldflags="-X 'github.com/fufuok/pkg/xjson/gjson.disableModifiers=true'"` |

另外, `AddModifier` 不能再覆盖 `@pretty`、`@this` 等内置修饰符, 同名注册会被忽略.

## 已知问题 (上游同样存在, 暂不修改)

2026-11 审查确认以下问题, 修复面大或没有现实触发路径, 暂不修改. 使用时请避开:

- `Value()` 和 `@pretty` 的耗时与嵌套深度成平方关系, `@pretty` 还会递归到栈溢出. 不要对不可信输入调用它们. 不可信输入先用 `Valid` 或 `pkg/json.Valid` 校验, 再按路径 `Get`.
- 对字符串结果继续取值时会扫描字符串内容. 例如 `Get({"s":"[1,2]"}, "s|0")` 得到 1. 需要数组时先检查 `IsArray()`.
- 经过管道 `|`、计算值 (修饰符、`!` 静态值、`{}`/`[]` 子选择器) 或嵌套多结果 (如 `a.#.b.#.c`) 后, `Index`/`Indexes` 可能相对子值或指向父元素, `Path`/`Paths` 随之出错. 不要用这类结果的偏移定位原文 (sjson 已按此规则拒绝写入). 单匹配查询后再取多结果的偏移已修正, 见上表.
- 路径段中同时含未转义的通配符时, 转义的 `\*`、`\?` 也按通配符匹配, 如 `a\*b*` 能匹配键 `axbc`. 只含转义通配符时按字面量匹配, `Escape` 生成的路径不受影响.
- `#(...)#.x|pipe` 在没有元素匹配时会丢掉管道后的部分.
- `!` 静态值后接管道或含 `]` 的字符串时解析错误.
- 通配路径匹配超长键时耗时可达秒级. 例如 5 万字节的键匹配 `*a*b*`. 不要把外部输入当作 gjson 路径.
- 对数组调用 `Map()` 返回 nil map.

## 免复制查询

`GetBytes`、`GetManyBytes` 会把命中的 `Raw`/`Str` 复制成独立字符串, 结果可以长期保存. 只在当前调用内同步使用结果时, 可以借用原文, 不再复制:

| 复制版本 | 借用写法 | 说明 |
| --- | --- | --- |
| `GetBytes(b, path)` | `Get(utils.B2S(b), path)` | 结果引用 `b`; 值含转义时 `Str` 仍会分配 |
| `GetManyBytes(b, paths...)` | 循环 `Get(utils.B2S(b), p)` 写入调用方的定长数组 | `GetMany` 仍会分配结果切片 |
| `GetBytes(b, path).Exists()` | `Get(utils.B2S(b), path).Exists()` | 命中对象或数组时, 复制版本会复制整段 `Raw` |

借用的约束: 结果用完之前, `b` 不能被修改、归还缓冲池或交给其他协程改写; 结果也不能交给异步消费者或长期缓存.

`GetMany`/`GetManyBytes` 对每个路径各扫描一次文档, 并不是一次扫描取多个字段.

## 同步上游

用 `go mod download -json github.com/tidwall/gjson@<version>` 取得源码, 逐版本对比后合入功能改动. 保留导入路径、中文注释和本文件列出的差异, 迁移上游新增测试, 并更新 [xjson/README.md](../README.md) 的版本表.
