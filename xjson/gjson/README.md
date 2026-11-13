# gjson

源自 [tidwall/gjson](https://github.com/tidwall/gjson) v1.20.2, 包路径改为 `github.com/fufuok/pkg/xjson/gjson`, 依赖本仓库的 `xjson/match` 与 `xjson/pretty`.

## 对上游的修正

以下修正在同步上游时必须保留, 每项都有回归测试.

| 位置 | 上游问题 | 本地修正 | 回归测试 |
| --- | --- | --- | --- |
| `parseUint` | 用 `next < n` 判断溢出, 漏掉乘 10 回绕后仍大于旧值的情况. 如 `25000000000000000000` 的 `Int()`/`Uint()` 得到 `6553255926290448384` | 乘加前与 `MaxUint64/10` 比较, 所有溢出都饱和为最大值 | `TestIntOverflowWrapMiss` |
| `f2i`, `f2u`, `safeInt` | NaN 直接转换为整数, 结果随 CPU 架构不同. amd64 上 `"NaN"` 的 `Int()` 为 MinInt64 | NaN 统一返回 0 | `TestIntStringFallback` |
| `Valid`, `ValidBytes` | 递归校验没有深度上限, 约 600 万层 `[` (6MB 输入) 即耗尽 1GB 栈, 进程直接退出, 无法 recover | 嵌套超过 10000 层返回 false, 与 `encoding/json.Valid` 一致 | `TestValidNestingDepth` |

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
- 经过管道 `|`、计算值 (修饰符、`!` 静态值、`{}`/`[]` 子选择器) 或嵌套 `#` 后, `Index`/`Indexes` 可能指向父元素, `Path`/`Paths` 随之出错. 不要用这类结果的偏移定位原文.
- 键路径中转义的 `\*`、`\?` 仍按通配符匹配.
- `#(...)#.x|pipe` 在没有元素匹配时会丢掉管道后的部分.
- `!` 静态值后接管道或含 `]` 的字符串时解析错误.
- 通配路径匹配超长键时耗时可达秒级. 例如 5 万字节的键匹配 `*a*b*`. 不要把外部输入当作 gjson 路径.
- 对数组调用 `Map()` 返回 nil map.

## 同步上游

用 `go mod download -json github.com/tidwall/gjson@<version>` 取得源码, 逐版本对比后合入功能改动. 保留导入路径、中文注释和本文件列出的差异, 迁移上游新增测试, 并更新 [xjson/README.md](../README.md) 的版本表.
