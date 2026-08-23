# json

`pkg/json` 默认走 `github.com/goccy/go-json`, `std_json` 构建标签切到标准库 `encoding/json`.

Go 1.27 起 `encoding/json` 由 json v2 引擎实现, 但默认仍套 v1 语义. 直接使用 `encoding/json/v2` 才是新默认语义.

## 四实现对比基准

对比对象:

- `goccy`: 当前默认实现
- `sonic`: `github.com/bytedance/sonic` (需原生 JIT; 已发布 v1.15.2 在 Go 1.27 会回退到标准库)
- `stdjson_v1`: `encoding/json` (Go 1.27 默认, v1 语义)
- `stdjson_v2`: `encoding/json/v2` (新默认语义)

负载覆盖短 API 响应, 嵌套配置, stats map, 100 条批量, HTML/中文转义, 数字数组.

对比工具仅存在于本地 `scripts/` (不入库), 不进入 `go test ./...`. 根模块也不把 sonic 记成直接依赖.

生产 Linux Go 1.27.0 amd64, `COUNT=3 BENCHTIME=1s`, 中位数 ns/op. 已发布 sonic v1.15.2 在两台机器上都是 `fallback=true`, 不能当原生 sonic 看:

| 负载 | 操作 | goccy 4c | v2 4c | v2/goccy 4c | goccy 16c | v2 16c | v2/goccy 16c |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| SmallAPI | Marshal | 1049 | 1396 | 1.33x | 959 | 1451 | 1.51x |
| SmallAPI | Unmarshal | 1641 | 2101 | 1.28x | 1381 | 2171 | 1.57x |
| NestedConf | Marshal | 1354 | 4925 | 3.64x | 1337 | 4324 | 3.23x |
| NestedConf | Unmarshal | 3887 | 5890 | 1.52x | 3459 | 7060 | 2.04x |
| Batch100 | Marshal | 31113 | 78317 | 2.52x | 33029 | 83239 | 2.52x |
| Batch100 | Unmarshal | 98744 | 170279 | 1.72x | 77754 | 167101 | 2.15x |

结论: 不要把默认实现换成 json v2. 生产机 typed struct 路径上 json v2 仍明显慢于 goccy; 只有 `map[string]any` 的 Marshal 偶尔更快. 已发布 sonic 在 Go 1.27 回退, 不能作为生产依赖.
