# pretty

源自 [tidwall/pretty](https://github.com/tidwall/pretty) v1.3.0, 包路径改为 `github.com/fufuok/pkg/xjson/pretty`.

## 对上游的修正

以下修正在同步上游时必须保留.

| 位置 | 上游问题 | 本地修正 | 回归测试 |
| --- | --- | --- | --- |
| `appendPrettyObject` 单行试排 | v1.3.0 起对象也尝试单行输出. 试排时先完整渲染整个子树, 再比较宽度, 放不下就整段丢弃, 嵌套越深重复越多. 同机基准中比 v1.2.2 慢 3–6 倍, `SortKeys` 时分配约多 3 倍 | 试排中长度一旦超过可用宽度就立即放弃. 试排结果本来就会被丢弃, 输出不变 | `TestSingleLineTrialAbort`, `BenchmarkPrettyNested` |

修正后的输出与上游 v1.3.0 逐字节一致: 随机配置类文档 30 万份, 各在 6 种宽度及开关 SortKeys 下比较 (共 360 万次), 另有 400 万次畸形输入, 均无差异. WSL Ubuntu 20.04 基准 (Width 80):

| 文档 | v1.2.2 | 上游 v1.3.0 | 本地 |
| --- | --- | --- | --- |
| 25KB 配置 | 74µs | 359µs | 117µs |
| 25KB 配置, SortKeys | 258µs, 411KB | 1182µs, 1221KB | 342µs, 495KB |
| 8.8KB, 12 层嵌套 | 39µs | 247µs | 72µs |

与 v1.2.2 相比剩余的开销, 来自对象单行试排这个功能本身.

## 已知问题 (上游同样存在, 暂不修改)

- 输出长度随嵌套深度平方增长, 递归过深还会栈溢出. 只用于格式化可信数据.

## 同步上游

用 `go mod download -json github.com/tidwall/pretty@<version>` 取得源码, 逐版本对比后合入功能改动. 保留导入路径、中文注释和本文件列出的修正, 并更新 [xjson/README.md](../README.md) 的版本表.
