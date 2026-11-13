# gjson

源自 [tidwall/gjson](https://github.com/tidwall/gjson) v1.20.2, 包路径改为 `github.com/fufuok/pkg/xjson/gjson`, 依赖本仓库的 `xjson/match` 与 `xjson/pretty`.

## 本地保留

- 非法 UTF-8 在 `AppendJSONString` 等编码路径输出 `\ufffd` 转义. 上游 v1.19.1 起改为原样写入 U+FFFD 字节, 两种写法都是合法 JSON, 反序列化结果相同. 本地保持历史输出, 测试中对比 Go 标准库时只接受这一处差异.

## 已移除的 API

上游 v1.20.0 已让以下全局变量失效, 本地直接删除, 不保留兼容. 应用升级时编译报错即需迁移:

| 已删除 | 替代方式 |
| --- | --- |
| `DisableEscapeHTML = true` | `gjson.SetEscapeHTML(false)`, 并发安全, 可在运行期切换 |
| `DisableModifiers = true` | 编译参数 `-ldflags="-X 'github.com/fufuok/pkg/xjson/gjson.disableModifiers=true'"` |

另外, `AddModifier` 不能再覆盖 `@pretty`、`@this` 等内置修饰符, 同名注册会被忽略.

## 同步上游

用 `go mod download -json github.com/tidwall/gjson@<version>` 取得源码, 逐版本对比后合入功能改动. 保留导入路径、中文注释和本文件列出的差异, 迁移上游新增测试, 并更新 [xjson/README.md](../README.md) 的版本表.
