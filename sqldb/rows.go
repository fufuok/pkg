package sqldb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Queryer 是 ForEach 和 Collect 所需的查询能力, 由 sql.DB, sql.Tx 和 sql.Conn 自动满足.
// 调用方仍持有查询对象, 助手只关闭本次查询取得的 Rows.
type Queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// Scanner 表示整行扫描, 可由 sql.Row 或助手提供的当前行视图满足.
// 它与标准库 sql.Scanner 的单字段转换接口不同. 当前行视图仅在回调期间有效.
type Scanner interface {
	Scan(...any) error
}

// rowScanner 只提供扫描, 避免回调通过类型断言接管 Rows 的推进或关闭.
type rowScanner struct {
	rows *sql.Rows
}

// Scan 直接委托标准库, 保持字段顺序, NULL 和类型转换语义.
func (s rowScanner) Scan(dest ...any) error {
	return s.rows.Scan(dest...)
}

// ForEach 按顺序同步处理第一个结果集, 并负责推进和关闭 Rows.
// ctx, q 和 fn 必须非 nil. SQL 和参数原样透传, fn 负责 Scan 及返回处理错误.
// 空集成功; 任意错误时调用方应丢弃部分结果, 外部副作用不会自动撤销.
// 保留回调, 迭代和显式关闭可观察的错误, panic 清理后原样传播.
// 回调期间 Rows 仍占用连接, 不要通过同一个单连接 DB 嵌套查询; 可先 Collect 再处理.
// 借用的 sql.RawBytes 等数据如需保留必须复制; 多结果集或提前停止请直接管理 Rows.
func ForEach(ctx context.Context, q Queryer, query string, fn func(Scanner) error, args ...any) (err error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("query rows: %w", err)
	}
	defer func() {
		// EOF 已自动关闭时, Err 才能交付关闭错误; 在显式 Close 前读取以免重复加入.
		if rowsErr := rows.Err(); rowsErr != nil {
			err = errors.Join(err, fmt.Errorf("iterate rows: %w", rowsErr))
		}
		if closeErr := rows.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close rows: %w", closeErr))
		}
	}()

	scanner := rowScanner{rows: rows}
	for rows.Next() {
		if err = fn(scanner); err != nil {
			return err
		}
	}
	return nil
}

// Collect 用显式 mapper 收集第一个结果集, 不提供反射或自动字段映射.
// 输入, 扫描和借用数据的约束与 ForEach 相同. 完整遍历及可观察的关闭成功后
// 才交付列表; 任意错误返回 nil 列表, 空集返回非 nil 空 slice.
func Collect[T any](ctx context.Context, q Queryer, query string, mapper func(Scanner) (T, error), args ...any) ([]T, error) {
	values := make([]T, 0)
	err := ForEach(ctx, q, query, func(scanner Scanner) error {
		value, err := mapper(scanner)
		if err != nil {
			return err
		}
		values = append(values, value)
		return nil
	}, args...)
	if err != nil {
		return nil, err
	}
	return values, nil
}
