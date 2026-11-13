# sjson

源自 [tidwall/sjson](https://github.com/tidwall/sjson) v1.2.9, 包路径改为 `github.com/fufuok/pkg/xjson/sjson`, 依赖本仓库的 `xjson/gjson`.

## 对上游的修正

以下修正在同步上游时必须保留, 每项都有回归测试.

| 位置 | 上游问题 | 本地修正 | 回归测试 |
| --- | --- | --- | --- |
| `maxpad` (`appendBuild`/`appendRawPaths`) | 只限制每层补齐. 路径 `a` 后重复 100 次 `.100000` (701 字节) 仍生成约 50MB 合法 JSON, 累计分配约 240MB, 且不报错 | 单次调用中所有层的补齐合计不超过 100000 个 null, 超出返回错误并保留原 JSON; 单层超限仍返回上游原有错误信息 | `TestPadTotalBudget` |
| `setComplexPath` | 路径中 `#` 后接修饰符、`!` 静态值、子选择器或嵌套 `#` 时, gjson 返回的偏移不指向原文, 直接切片会 panic (如 `Set("[1]", "#.!true", v)`) 或拼出非法 JSON (如 `a.#.#`). 路径来自外部输入时, 单个请求即可触发 | 替换前逐个校验偏移和内容, 并确认区间互不重叠. 任一结果无法定位到原文时不修改, 也不报错, 与路径不存在时一致 | `TestComplexPathUnlocatable` |
| `deleteTailItem` | 删除以转义引号开头的键 (如 `\"`) 时多退一位, 越过键的起始引号, 误删前一个键或留下 `{"\"":,"y":2}` 这类非法 JSON | 去掉多余的回退 | `TestDeleteKeyLeadingEscapedQuote` |
| `set` 乐观原地替换 | 同时开启 `Optimistic` 和 `ReplaceInPlace` 时, 写入需要转义的字符串 (非 ASCII、引号、控制字符) 会原样返回输入, 修改被静默丢弃 | 需要转义时改走分配新切片的路径 | `TestOptimisticInPlaceEscape` |

## 已知问题 (上游同样存在, 暂不修改)

- `maxpad` 只约束单次调用. 对同一文档用多个外部路径反复 `Set` 时, 每次仍可补齐 100000 个 null (约 500KB), 增长会累积. 例如约 4KB 的查询串含 364 个 `xx.99999` 键时, 正文累计约 173MB, 分配约 70GB. 把外部输入当路径的调用方须自行限制总大小, 或先用转义把外部键当作普通键名.
- 路径键含非法 UTF-8 时, 每次 `Set` 都会追加一个重复键. 写入时键被替换为 U+FFFD, 下次查找按原字节比较而找不到.
- 开启 `Optimistic` 时, 以 `..` 开头的路径按 gjson 的 JSON Lines 语义处理, 可能替换整个文档.
- 上游 v1.2.6 起的行为变化 (NaN/Inf 报错、补齐上限 100000、空路径表示空键等) 见 [xjson/README.md](../README.md).

## 同步上游

用 `go mod download -json github.com/tidwall/sjson@<version>` 取得源码, 逐版本对比后合入功能改动. 保留导入路径、中文注释和本文件列出的修正, 迁移上游新增测试, 并更新 [xjson/README.md](../README.md) 的版本表.
