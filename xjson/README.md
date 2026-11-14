# xjson

内嵌的 JSON 工具子包. 除 `jsongen` 外均为 tidwall 系列上游源码副本, 包路径改为 `github.com/fufuok/pkg/xjson/...`, 不依赖上游模块, 也不会随 `go.mod` 升级自动更新.

## 子包与上游版本

| 子包 | 上游 | 已同步版本 | 本地差异 |
| --- | --- | --- | --- |
| `gjson` | [tidwall/gjson](https://github.com/tidwall/gjson) | v1.20.3 | 溢出 (不采纳 v1.20.3 的 `parseUint`)、NaN、Valid 深度、32 位下标截断四处修正, 修饰符表写时复制与 `Int()` 字符串回退免分配两处性能修正, 非法 UTF-8 输出保持历史写法, 本地扩展 `GetManyKeys`, 见 [gjson/README.md](gjson/README.md) |
| `sjson` | [tidwall/sjson](https://github.com/tidwall/sjson) | v1.2.9 | 补齐总预算、复杂路径偏移校验、转义引号键删除、原地替换转义四处修正, `atoui` 免分配一处性能修正, 见 [sjson/README.md](sjson/README.md) |
| `pretty` | [tidwall/pretty](https://github.com/tidwall/pretty) | v1.3.0 | 单行试排超宽即中止 (性能修正, 输出不变), 见 [pretty/README.md](pretty/README.md) |
| `match` | [tidwall/match](https://github.com/tidwall/match) | v1.2.0 | 仅中文注释和声明位置 |
| `jsongen` | 无 | 本地实现 | 自行实现和维护, 不参考也不同步任何上游 |

`jsongen` 的接口、性能取舍和边界以本仓库源码及测试为准, 上游同步时直接跳过.

## 对上游的修正与已知问题

- gjson、sjson、pretty 对上游的修正和暂不修改的已知问题分别见 [gjson/README.md](gjson/README.md)、[sjson/README.md](sjson/README.md)、[pretty/README.md](pretty/README.md), 同步上游时不得覆盖这些修正.
- match: 模式以单个反斜杠结尾时行为不一致: 字符串 `\` 能匹配模式 `*\`, 却不能匹配模式 `\`. 上游同样存在, 暂不修改.
- jsongen: `PutFloat`/`AppendFloat` 写 NaN/±Inf 会得到非法 JSON; `PutMap`/`PutArray` 传 nil 时 `Size`/`Serialize` panic; 空 `RawBytes` 写出 `[1,]`. 调用方需自行避免.

## 2026-11 同步带来的行为变化

升级前请确认调用方不依赖下列旧行为:

| 子包 | 变化 | 适用场景与收益 |
| --- | --- | --- |
| pretty | `Pretty`/`PrettyOptions` 默认把宽度内的短对象输出为单行 `{ "a": 1 }`, 此前只有数组会单行. 空对象 `{}` 输出为 `{ }`, `[{}]` 输出为 `[{ }]`. `Width <= 0` 时数组和对象都不单行 | 日志和配置文件更紧凑. 若比较格式化后的文本或摘要, 升级后首次比较会不同 |
| pretty | `Color` 正确处理 `\\` 转义, 不再把字符串结束引号染成转义色 | 终端彩色输出 JSON |
| gjson | `@pretty` 修饰符随 pretty v1.3.0 输出单行短对象 | 同上 |
| gjson | `Int()`/`Uint()` 溢出时饱和到类型端点, 不再回绕. 字符串整数解析失败后按 `strconv.ParseFloat` 回退: `"1.9"`/`"1e3"`/`"+1"`/`"5."`/`"0x1p4"`/`"1_000"` 得 1/1000/1/5/16/1000, `"Inf"`/`"1e400"` 饱和为最大值, 此前均为 0; 负数 `Uint()` 为 0 | 处理大整数 ID、计数器或字符串形式数值时不再得到错误的回绕值 |
| gjson | 超出 uint64 的数组下标不再回绕命中元素, 如 `Get("[1,2,3]", "18446744073709551616")` 此前返回 1 | 路径来自外部输入时避免读错元素 |
| gjson | 高代理后跟非低代理的 `\u` 转义按 encoding/json 解码, 不再吞掉后一个字符 | 解析含异常 emoji 转义的上游数据 |
| gjson | 新增 `Result.All/Keys/Values` 迭代器, 可配合 `for range` 使用 | 遍历对象或数组, 不生成中间切片 |
| gjson | `AddModifier` 可与查询并发调用; 内置修饰符 (如 `@pretty`) 不可再被覆盖 | 运行期注册自定义修饰符时不再触发 map 并发读写崩溃 |
| gjson | 删除 `DisableEscapeHTML`、`DisableModifiers`, 改用 `SetEscapeHTML` 和编译参数, 见 [gjson/README.md](gjson/README.md) | 运行期切换转义不再有数据竞争; 旧变量的使用方编译即报错, 升级时迁移 |
| sjson | 写入 NaN/±Inf 时结果为带引号的字符串 `"NaN"`, 同时返回错误, 此前静默写出非法 JSON | 浮点指标写回 JSON 时保证结果合法 |
| sjson | 按下标补齐数组最多 100000 个 null, 超出返回错误 (上游按每层计, 本地按单次调用累计); 超出 int 范围的下标按对象键处理 | 路径来自外部输入时防止超大下标耗尽内存; 对同一文档反复 Set 的累计增长仍需调用方限制 |
| sjson | 空路径表示空字符串键 `""`, 对数组则追加元素 (`Set("[1,2]", "", 9)` 得 `[1,2,9]`), 此前返回 `path cannot be empty` 错误 | 操作空键 |
| sjson | 含 `#` 等的复杂路径只在偏移可定位到原文时写入. 管道 (含查询值里的 `\|`)、`#` 后的静态值/修饰符/子选择器、嵌套多结果等无法定位的写入不修改也不报错, 此前可能 panic、输出非法 JSON 或改写别的字段; JSON Lines 单结果查询 (如 `..#(a==1)`) 命中首行时也会写入, 此前只有非首行会写入 | 外部输入作路径时不再写坏正文; 依赖旧的错写结果的调用方需改用明确路径 |
| sjson | 删除对象中的 `-1` 键按键名删除, `-1` 只在数组上表示最后一个元素 | 修正误删 |

## 同步方法

1. 用 `go mod download -json github.com/tidwall/<pkg>@<version>` 取得新旧版本源码, 逐版本对比差异.
2. 只合入功能改动, 保留本地导入路径、中文注释和各子包 README 记录的修正.
3. 迁移上游新增测试, 为本地差异补回归测试, 并更新本文件的版本表.
