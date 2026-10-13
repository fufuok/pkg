// sqldb 示例用独立的内存 SQLite 演示事务和显式扫描, 不连接外部数据库.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/fufuok/pkg/sqldb"
	_ "modernc.org/sqlite"
)

const listUsersSQL = "SELECT id, name FROM users ORDER BY id"

type user struct {
	ID   int64
	Name string
}

// main 把演示和资源释放错误交给调用进程, 失败时返回非零退出码.
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run 校验提交、回滚和完整列表, 数据只在本次进程的唯一连接内存中存在.
func run() (err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return fmt.Errorf("open demo database: %w", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close demo database: %w", closeErr))
		}
	}()
	// 普通 :memory: 按连接独立建库, 保留唯一连接避免空闲关闭时丢失数据.
	// 这些参数只适用于演示, 不作为 SQLite 生产连接池默认值.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	db.SetConnMaxIdleTime(0)

	if err := sqldb.WithTx(ctx, db, nil, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL)"); err != nil {
			return fmt.Errorf("create users: %w", err)
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO users (id, name) VALUES (?, ?)", 1, "Alice")
		return err
	}); err != nil {
		return fmt.Errorf("prepare demo: %w", err)
	}

	// 写后读全部经传入的 tx, 相同的 listUsers 也可接受 DB 或专用 Conn.
	committed, err := sqldb.WithTxValue(ctx, db, nil, func(tx *sql.Tx) ([]user, error) {
		if _, err := tx.ExecContext(ctx, "INSERT INTO users (id, name) VALUES (?, ?)", 2, "Bob"); err != nil {
			return nil, fmt.Errorf("insert user: %w", err)
		}
		return listUsers(ctx, tx)
	})
	if err != nil {
		return fmt.Errorf("commit users: %w", err)
	}
	// WithTxValue 只在 Commit 成功后交付结果, 回调内不要提前发布或发送通知.
	want := []user{{ID: 1, Name: "Alice"}, {ID: 2, Name: "Bob"}}
	if !slices.Equal(committed, want) {
		return fmt.Errorf("unexpected committed users: %v", committed)
	}

	rejected := errors.New("reject demo user")
	discarded, err := sqldb.WithTxValue(ctx, db, nil, func(tx *sql.Tx) (int, error) {
		if _, err := tx.ExecContext(ctx, "INSERT INTO users (id, name) VALUES (?, ?)", 3, "Carol"); err != nil {
			return 0, fmt.Errorf("insert rejected user: %w", err)
		}
		// 业务失败后整体回滚, helper 不交付本次回调产生的非零结果.
		return 99, rejected
	})
	if err == nil {
		return errors.New("expected rollback business error")
	}
	if !errors.Is(err, rejected) {
		return fmt.Errorf("rollback rejected user: %w", err)
	}
	if discarded != 0 {
		return fmt.Errorf("unexpected rollback value: %d", discarded)
	}
	users, err := listUsers(ctx, db)
	if err != nil {
		return err
	}
	if !slices.Equal(users, want) {
		return fmt.Errorf("unexpected users after rollback: %v", users)
	}
	if err := verifyUserScans(ctx, db, want); err != nil {
		return err
	}
	fmt.Printf("committed: %v\nrolled back: true (value=%d)\nusers: %v\n", committed, discarded, users)
	return nil
}

// verifyUserScans 验证列表和单行共用显式映射, 每次查询结束后才进行下一次查询.
func verifyUserScans(ctx context.Context, db *sql.DB, want []user) error {
	var visited []user
	if err := sqldb.ForEach(ctx, db, listUsersSQL, func(s sqldb.Scanner) error {
		u, err := scanUser(s)
		if err == nil {
			visited = append(visited, u)
		}
		return err
	}); err != nil {
		// 遍历失败时不交付 visited 中已收集的部分行.
		return fmt.Errorf("visit users: %w", err)
	}
	if !slices.Equal(visited, want) {
		return fmt.Errorf("unexpected visited users: %v", visited)
	}
	// Scanner 只要求整行 Scan, 同一映射也适用于标准库单行查询.
	first, err := scanUser(db.QueryRowContext(ctx, "SELECT id, name FROM users WHERE id = ?", 1))
	if err != nil {
		return err
	}
	if first != want[0] {
		return fmt.Errorf("unexpected first user: %v", first)
	}
	return nil
}

// scanUser 固定应用字段顺序; 本表 name 非 NULL, 实际可空字段应显式使用 sql.NullString 等.
func scanUser(s sqldb.Scanner) (user, error) {
	var u user
	if err := s.Scan(&u.ID, &u.Name); err != nil {
		return user{}, fmt.Errorf("scan user: %w", err)
	}
	return u, nil
}

// listUsers 对 DB, Tx 和 Conn 复用 SQL; Collect 遍历失败时返回 nil 列表而非部分结果.
func listUsers(ctx context.Context, q sqldb.Queryer) ([]user, error) {
	users, err := sqldb.Collect(ctx, q, listUsersSQL, scanUser)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	return users, nil
}
