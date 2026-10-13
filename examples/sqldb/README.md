# sqldb 内存 SQLite 示例

此独立 Go module 演示 `WithTx`、`WithTxValue`、`ForEach`、`Collect[T]` 及 `Queryer` / `Scanner`. 生产包 `github.com/fufuok/pkg/sqldb` 只依赖标准库, SQLite 驱动及其依赖仅属于本示例. 不需要数据库服务或 CGO, 不读取真实业务数据, 不写入数据库文件.

示例先共同提交建表和 Alice, 再在事务中新增 Bob 并查询完整列表, 成功提交后交付结果. 第三个事务新增 Carol 后返回业务错误, 程序检查 `errors.Is` 和结果零值, 再确认数据库中只有 Alice / Bob. 最后验证 `ForEach` 和单行查询使用同一 `scanUser` 能读到相同数据.

`listUsers(ctx, q sqldb.Queryer)` 的 SQL 可以直接接受 `*sql.DB`、`*sql.Tx` 或 `*sql.Conn`. `scanUser(s sqldb.Scanner)` 显式决定字段顺序与 NULL 处理, 同时复用 `Collect` / `ForEach` 的当前行和 `*sql.Row`; 这里的 `Scanner` 是整行扫描接口, 与标准库单字段转换接口 `sql.Scanner` 不同. 单条查询继续调用 `QueryRowContext().Scan`, 不增加反射映射.

## 从当前仓库运行

本例固定依赖已发布的 `github.com/fufuok/pkg v1.3.11` 和 `modernc.org/sqlite v1.47.0`, 不需要 workspace 或本机 `replace`. 以下命令从仓库根目录执行, 用 `GOWORK=off` 确保使用示例声明的发布版本.

PowerShell:

```powershell
$sqldbPreviousWork = $env:GOWORK
try {
    $env:GOWORK = 'off'
    go -C examples/sqldb run .
    if ($LASTEXITCODE -ne 0) { throw 'Run example failed' }
} finally {
    $env:GOWORK = $sqldbPreviousWork
}
```

Git Bash / WSL2:

```bash
GOWORK=off go -C examples/sqldb run .
```

成功输出:

```text
committed: [{1 Alice} {2 Bob}]
rolled back: true (value=0)
users: [{1 Alice} {2 Bob}]
```

根模块的 `go test ./...` 不会检查本独立 module. CI 单独以 `GOWORK=off` 执行 build / vet / run, 校验本例可以独立使用发布版本; 再用临时 workspace 选中当前根模块运行一次, 检查当前源码与 SQLite 示例的兼容性. 本地维护 pkg 时也可使用临时 workspace, 不修改本例的发布依赖或提交本机 `replace`.

## 演示边界

- 普通 `:memory:` 数据库按连接独立存在. 本例设置 `MaxOpenConns=1`、`MaxIdleConns=1`, 连接生命期和空闲超时均为 0, 最后关闭 DB. 这使演示始终保留同一内存库, 不代表 SQLite 的生产默认配置.
- 事务回调中的工作全部使用传入的 tx 并在返回前完成. 单连接池中混用 db 查询可能等待本事务占用的唯一连接.
- 回调不自行提交或回滚, 不在提交成功前发送通知或交付成功结果. Commit 失败不能当作已成功回滚, 不自动重试.
- `ForEach` 失败时丢弃已收集的部分列表; `Collect` 失败返回 nil 列表. 当前行 Scanner 不能保存到回调结束后继续使用.
- 内存库只证明调用和事务行为, 不验证文件持久化、WAL、锁等待、崩溃恢复、历史迁移或退出保存. 这些仍在实际应用验证.
