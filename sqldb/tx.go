// Package sqldb 提供 database/sql 的事务和结果集生命周期助手.
// SQL, 驱动, 连接池配置和数据库的关闭责任仍由应用持有.
package sqldb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// WithTx 在一个事务中同步执行 fn, 成功后提交, 失败或 panic 时尝试回滚.
// ctx, db 和 fn 必须非 nil, opts 可为 nil. fn 不得自行提交或回滚,
// 所有事务内工作必须在 fn 返回前完成, SQL 使用传入的 tx.
// 错误保留 errors.Is/As 语义, panic 原样传播; 不提供嵌套事务或自动重试.
func WithTx(ctx context.Context, db *sql.DB, opts *sql.TxOptions, fn func(*sql.Tx) error) error {
	_, err := WithTxValue(ctx, db, opts, func(tx *sql.Tx) (struct{}, error) {
		return struct{}{}, fn(tx)
	})
	return err
}

// WithTxValue 执行回调事务, 仅在提交成功后交付 fn 的结果.
// 任意返回错误都会使结果归零, 但不会撤销 fn 对外部对象或资源的副作用.
// 输入和回调约束与 WithTx 相同. Commit 失败时结果可能不明, 不声称已回滚.
// 清理阶段的 sql.ErrTxDone 忽略, Commit 自身的该错误仍返回.
// 只能保留标准库公开 API 可观察的清理错误; panic 期间不改写原 panic.
func WithTxValue[T any](ctx context.Context, db *sql.DB, opts *sql.TxOptions, fn func(*sql.Tx) (T, error)) (value T, err error) {
	tx, err := db.BeginTx(ctx, opts)
	if err != nil {
		return value, fmt.Errorf("begin transaction: %w", err)
	}
	// defer 同时覆盖回调失败和 panic; 已提交/自动回滚的事务无需重复清理.
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			err = errors.Join(err, fmt.Errorf("rollback transaction: %w", rollbackErr))
		}
		if err != nil {
			var zero T
			value = zero
		}
	}()

	value, err = fn(tx)
	if err != nil {
		return value, err
	}
	if err = tx.Commit(); err != nil {
		return value, fmt.Errorf("commit transaction: %w", err)
	}
	return value, nil
}
