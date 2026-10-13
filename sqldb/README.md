# sqldb

`sqldb` 为 `database/sql` 提供事务和结果集生命周期助手, 生产代码只依赖标准库. 应用继续管理驱动、SQL、连接池、表结构和 DB 的关闭, 不需要注册全局数据库或 `master` 模块.

导入路径: `github.com/fufuok/pkg/sqldb`. 下文提供可复用的仓储查询、扫描和事务范式.

## 公共接口

```go
func WithTx(ctx context.Context, db *sql.DB, opts *sql.TxOptions, fn func(*sql.Tx) error) error
func WithTxValue[T any](ctx context.Context, db *sql.DB, opts *sql.TxOptions, fn func(*sql.Tx) (T, error)) (T, error)

type Queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type Scanner interface {
	Scan(...any) error
}

func ForEach(ctx context.Context, q Queryer, query string, fn func(Scanner) error, args ...any) error
func Collect[T any](ctx context.Context, q Queryer, query string, mapper func(Scanner) (T, error), args ...any) ([]T, error)
```

`Queryer` 由 `*sql.DB`、`*sql.Tx` 和 `*sql.Conn` 自动满足, 用于同一个查询函数在不同执行对象之间复用. `Scanner` 表示整行扫描, 可由 `*sql.Row` 或助手提供的当前行视图满足; 它与标准库用于单字段转换的 `sql.Scanner` 不同.

## 生命周期和错误

| 助手 | 成功行为 | 失败行为 |
| --- | --- | --- |
| `WithTx` | 回调成功后提交 | Begin、回调或 Commit 错误向上传递, 尝试回滚清理 |
| `WithTxValue` | Commit 成功后交付回调结果 | 任意返回错误时结果归零 |
| `ForEach` | 同步遍历第一个结果集, 推进和关闭 Rows | 保留回调、迭代和可观察的关闭错误, 调用方丢弃部分结果 |
| `Collect` | 返回完整列表; 空集为非 nil 空 slice | 任意返回错误时返回 nil 列表 |

ctx、数据库或查询对象、回调必须非 nil, `opts` 可以为 nil. 事务回调不自行 Commit / Rollback, 所有 SQL 使用传入的 tx, 并在返回前完成事务内工作和资源使用. 单连接池中混用 db 查询可能等待当前事务占用的唯一连接.

错误保留 `errors.Is` / `errors.As` 语义, 独立清理错误通过 `errors.Join` 附加. 清理阶段的 `sql.ErrTxDone` 忽略, Commit 返回它仍报错. Commit 失败可能意味着提交结果不明, 不承诺已成功回滚, 不自动重试. panic 经 deferred 清理后原样传播. 标准库自动关闭 Rows 或取消后自动回滚可能隐藏独立清理错误, 助手只能保留公开 API 可观察的错误.

当前行 Scanner 只在回调期间有效. SQL、参数、字段顺序、NULL 和类型转换遵循标准库; 如需保留 `sql.RawBytes` 等借用数据, 回调内复制. 多结果集或提前结束遍历需求直接管理 `sql.Rows`. 回调对外部对象、文件或消息的副作用不会因事务失败或收集失败撤销.

`ForEach` 的回调和 `Collect` 的 mapper 执行期间, Rows 仍占用查询连接. 对同一个单连接 DB 嵌套查询会等待自身释放连接; 需要额外查询时, 可先 Collect 完成扫描并关闭 Rows, 再处理结果, 或将关联读取合并到合适的 SQL 中.

## 复用查询与扫描

以下片段假设应用已有 `users(id, name)` 表, `name` 非 NULL. `?` 占位符沿用应用驱动语义, 助手不改写方言. 可空字段由应用显式使用 `sql.NullString` 等扫描目标.

```go
package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/fufuok/pkg/sqldb"
)

const listUsersSQL = "SELECT id, name FROM users ORDER BY id"

type user struct {
	ID   int64
	Name string
}

// scanUser 固定字段顺序, 同时处理列表当前行和标准库单行查询.
func scanUser(s sqldb.Scanner) (user, error) {
	var u user
	if err := s.Scan(&u.ID, &u.Name); err != nil {
		return user{}, fmt.Errorf("scan user: %w", err)
	}
	return u, nil
}

// listUsers 对 DB, Tx 和 Conn 复用查询, 失败时不交付部分列表.
func listUsers(ctx context.Context, q sqldb.Queryer) ([]user, error) {
	users, err := sqldb.Collect(ctx, q, listUsersSQL, scanUser)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	return users, nil
}

// findUser 继续使用标准库单行查询, sql.ErrNoRows 保留在错误链中.
func findUser(ctx context.Context, db *sql.DB, id int64) (user, error) {
	return scanUser(db.QueryRowContext(ctx,
		"SELECT id, name FROM users WHERE id = ?", id))
}
```

应用直接调用 `listUsers(ctx, db)`, 在事务中调用 `listUsers(ctx, tx)`, 或对已有专用连接调用 `listUsers(ctx, conn)`, 不需要创建适配对象. 助手只关闭本次取得的 Rows, DB / Tx / Conn 的所有权仍由调用方管理.

## 共同提交与提交后交付

下列函数接在上述仓储片段之后. `WithTx` 适合不返回业务结果的写入; `WithTxValue` 适合提交后才交付数量、记录或列表.

```go
// insertUsers 共同提交一批用户, 任一写入失败时不交付部分成功.
func insertUsers(ctx context.Context, db *sql.DB, users []user) error {
	return sqldb.WithTx(ctx, db, nil, func(tx *sql.Tx) error {
		for _, u := range users {
			if _, err := tx.ExecContext(ctx,
				"INSERT INTO users (id, name) VALUES (?, ?)", u.ID, u.Name); err != nil {
				return fmt.Errorf("insert user: %w", err)
			}
		}
		return nil
	})
}

// createAndList 事务内写后读, 只在提交成功后向调用方交付列表.
func createAndList(ctx context.Context, db *sql.DB, u user) ([]user, error) {
	return sqldb.WithTxValue(ctx, db, nil, func(tx *sql.Tx) ([]user, error) {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO users (id, name) VALUES (?, ?)", u.ID, u.Name); err != nil {
			return nil, fmt.Errorf("insert user: %w", err)
		}
		return listUsers(ctx, tx)
	})
}
```

通知或响应发送放在函数成功返回之后. 返回零值只控制助手的交付结果, 不会撤销回调已修改的外部 slice / map.

## 逐行处理

需要逐行检查而无需列表时使用 `ForEach`. 下例同样复用 `scanUser`, 失败时舍弃已计算的部分计数.

```go
// countNamedUsers 完整检查查询结果, 仅成功遍历后返回名称非空的用户数.
func countNamedUsers(ctx context.Context, q sqldb.Queryer) (int, error) {
	count := 0
	err := sqldb.ForEach(ctx, q, listUsersSQL, func(s sqldb.Scanner) error {
		u, err := scanUser(s)
		if err != nil {
			return err
		}
		if u.Name != "" {
			count++
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("count named users: %w", err)
	}
	return count, nil
}
```

已有 GORM 应用继续使用其事务与扫描接口. 文件持久化、WAL、历史迁移及退出保存由实际应用另行验证.
