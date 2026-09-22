# json

`pkg/json` 默认走 `github.com/goccy/go-json`, `std_json` 构建标签切到标准库 `encoding/json`.

Go 1.27 起 `encoding/json` 由 json v2 引擎实现, 但默认仍套 v1 语义. 直接使用 `encoding/json/v2` 才是新默认语义.

## 四实现日常应用基准

`json/cmd/jsonbench` 是可编译的独立测试程序, 用同一份固定数据对比:

- `stdjson_v1`: Go 1.27 `encoding/json`, v1 语义.
- `stdjson_v2`: Go 1.27 `encoding/json/v2`, v2 默认语义.
- `goccy`: `github.com/goccy/go-json v0.10.6`, 当前最新正式版和 `pkg/json` 默认实现.
- `sonic`: `github.com/bytedance/sonic v1.15.3`, 当前最新正式版, 原生支持 Go 1.27 的 amd64/arm64.

基准程序使用目录内独立的 `go.mod` 和 `go.sum`, 不依赖根模块 `pkg`. 根目录的 `go mod tidy` 和 `go test ./...` 不会扫描该子模块, Sonic 的基准依赖也不会被提升到根模块的直接依赖. 根模块仍可能因 Gin 的可选 Sonic 实现保留间接依赖, 这不表示 `pkg/json` 切换了实现. 本地基准目录仍保持 Git 忽略.

程序启动后先执行以下步骤, 任何一步失败都不会输出性能排名:

1. 读取二进制实际链接的 Go、goccy 和 sonic 版本.
2. 用 sonic 公开的 `APIKind` 判断 native/fallback. 默认拒绝 fallback 结果.
3. 对 sonic 的固定类型执行 `PretouchMany`, 将首次 JIT 成本移出稳态计时.
4. 验证四实现 Marshal 输出和 canonical v1 JSON 语义等价.
5. 验证四实现都能把相同 canonical v1 JSON 解码回等价值.
6. 单独输出 nil slice、HTML 转义、重复键和字段大小写兼容性探针.
7. 多轮轮转 codec 执行约 1 秒的自适应采样, 报告中位数.

### 固定负载

| 名称 | 日常场景 | 当前 canonical 大小 |
| --- | --- | ---: |
| `SmallAPI` | 单对象 HTTP API 响应 | 296 B |
| `NestedConfig` | 多层服务配置 | 1128 B |
| `StatsMap` | 动态 map 指标/诊断响应 | 511 B |
| `Batch100` | 100 条 typed event 批量数据 | 24702 B |
| `TextPayload` | 中文、HTML、路径和换行文本 | 315 B |
| `NumberArray512` | 512 个浮点采样值 | 5125 B |

所有 Unmarshal 使用完全相同的 `encoding/json` v1 canonical 字节. 每次解码都会创建新目标, 不复用 slice/map 容量. Marshal 按各实现实际输出大小计算吞吐.

### 构建和运行

在仓库根目录执行下列命令, 使用目标机器的实际 Go 工具链构建. `GOWORK=off` 确保独立使用基准模块的依赖, 避免父目录 `go.work` 干扰; 二进制输出回仓库根目录:

```bash
GOWORK=off go -C json/cmd/jsonbench build -trimpath -o ../../../jsonbench .
./jsonbench -list
```

Windows PowerShell:

```powershell
$benchPreviousGoWork = $env:GOWORK
try {
    $env:GOWORK = 'off'
    go -C json/cmd/jsonbench build -trimpath -o ../../../jsonbench.exe .
} finally {
    $env:GOWORK = $benchPreviousGoWork
}
.\jsonbench.exe -list
```

推荐分别保留串行延迟和并行吞吐结果. 默认 `count=3`, 完整串行约 144 秒, 完整并行约 144 秒; 实际时间取决于 `testing.Benchmark` 自适应校准:

```bash
./jsonbench -count=3 -mode=serial > jsonbench-serial.txt
./jsonbench -count=3 -mode=parallel -procs=16 > jsonbench-parallel.txt
./jsonbench -count=3 -mode=all -procs=16 -format=json > jsonbench-all.json
```

生产机先做短 smoke:

```bash
./jsonbench -count=1 -mode=serial -workload=SmallAPI
./jsonbench -count=1 -mode=parallel -codec=stdjson_v1,stdjson_v2,goccy,sonic -workload=Batch100
```

参数:

- `-count`: 每个场景采样轮数, 最终报告各指标中位数.
- `-mode`: `serial`, `parallel` 或 `all`.
- `-procs`: 设置 `GOMAXPROCS`; 0 保留运行时默认值. 并行测试应设置为生产服务的真实 CPU 配额.
- `-codec`: 逗号分隔的实现名, 默认 `all`.
- `-workload`: 逗号分隔的负载名, 默认 `all`.
- `-format`: `text` 或 `json`.
- `-list`: 只列出当前版本和负载, 不运行 JIT 或基准.
- `-allow-sonic-fallback`: 仅用于诊断不受支持的平台. fallback 数据不能用于四实现选型.

进度写入 stderr, 最终报告写入 stdout, 因此重定向不会丢失运行状态. 正式采样时应停止同机压测和高负载任务, 保持相同 CPU 配额、调频策略、容器限制和 Go 环境. 至少保留一份 `-format=json` 原始结果.

### 结果判读

- `NS/OP` 越低越好.
- `B/OP` 和 `ALLOCS/OP` 越低, GC 压力通常越小.
- `MIB/S` 越高越好. Marshal 使用实际输出大小, Unmarshal 使用 canonical 输入大小.
- `VS_V1` 以相同负载/操作/mode 的 `stdjson_v1` 为 `1.00x`; 小于 1 更快.
- `parallel` 的 `NS/OP` 是总吞吐折算的每操作墙钟成本, 不是单请求尾延迟.

速度不能覆盖兼容性. 当前四项探针的关键差异是:

| 实现 | nil slice | HTML | 重复键 | 大小写不一致字段 |
| --- | --- | --- | --- | --- |
| `stdjson_v1` | `null` | 转义 | 接受, 后值覆盖 | 匹配 |
| `stdjson_v2` | `[]` | 不转义 | 报错 | 不匹配 |
| `goccy` | `null` | 转义 | 接受, 后值覆盖 | 匹配 |
| `sonic` 默认配置 | `null` | 不转义 | 接受, 后值覆盖 | 匹配 |

若现有协议依赖 v1 输出或宽松解码, `stdjson_v2` 不能只凭性能直接替换. 若 JSON 会原样嵌入 HTML, sonic 默认配置的转义差异也必须先处理. 更完整的 v1/v2 冻结测试见 `json_v2_semantics_test.go`.

本工具只测常见的一次性 `Marshal`/`Unmarshal`. 流式 Encoder/Decoder、自定义 Marshaler、超大文档、未知字段策略和首请求 JIT 延迟不在当前排名内; 生产调用链存在这些场景时应补对应 workload 后再决策.

## 历史基线

2026-08 的上一轮生产 Linux Go 1.27.0 amd64 测试使用 sonic `v1.15.2`. 当时该版本在 Go 1.27 回退到 `encoding/json`, 因此 sonic 数据无效; typed struct 上 json v2 比 goccy 慢约 `1.3x-3.6x`, 当时结论是继续使用 goccy.

sonic `v1.15.3` 已改变这个前提. 当前仓库暂不改变 `pkg/json` 默认实现, 最终选型应以新程序在目标生产 CPU、真实 CPU 配额和 native sonic 下的串行/并行结果为准.
