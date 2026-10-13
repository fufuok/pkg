package sqldb_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"testing"

	"github.com/fufuok/pkg/sqldb"
)

// TestWithTxLifecycle 验证每种退出阶段的唯一提交所有权和可见错误链.
func TestWithTxLifecycle(t *testing.T) {
	beginErr := errors.New("begin failed")
	businessErr := errors.New("business failed")
	commitErr := errors.New("commit failed")
	rollbackErr := errors.New("rollback failed")
	cases := []struct {
		name        string
		state       testDriverState
		callbackErr error
		wantErrors  []error
		callbacks   int
		commits     int
		rollbacks   int
	}{
		{name: "success", callbacks: 1, commits: 1},
		{name: "begin failure", state: testDriverState{beginErr: beginErr}, wantErrors: []error{beginErr}},
		{name: "business failure", callbackErr: businessErr, wantErrors: []error{businessErr}, callbacks: 1, rollbacks: 1},
		{name: "rollback failure", state: testDriverState{rollbackErr: rollbackErr}, callbackErr: businessErr, wantErrors: []error{businessErr, rollbackErr}, callbacks: 1, rollbacks: 1},
		{name: "cleanup already done", state: testDriverState{rollbackErr: sql.ErrTxDone}, callbackErr: businessErr, wantErrors: []error{businessErr}, callbacks: 1, rollbacks: 1},
		{name: "commit failure", state: testDriverState{commitErr: commitErr}, wantErrors: []error{commitErr}, callbacks: 1, commits: 1},
		{name: "commit already done", state: testDriverState{commitErr: sql.ErrTxDone}, wantErrors: []error{sql.ErrTxDone}, callbacks: 1, commits: 1},
	}
	for i := range cases {
		test := &cases[i]
		t.Run(test.name, func(t *testing.T) {
			db := newTestDB(t, &test.state)
			callbacks := 0
			err := sqldb.WithTx(context.Background(), db, nil, func(tx *sql.Tx) error {
				callbacks++
				if tx == nil {
					t.Fatal("callback received a nil transaction")
				}
				if test.callbackErr != nil {
					return fmt.Errorf("store changes: %w", test.callbackErr)
				}
				return nil
			})
			if len(test.wantErrors) == 0 && err != nil {
				t.Fatalf("unexpected transaction error: %v", err)
			}
			for _, want := range test.wantErrors {
				if !errors.Is(err, want) {
					t.Errorf("expected error %v, got %v", want, err)
				}
			}
			if test.name == "cleanup already done" && errors.Is(err, sql.ErrTxDone) {
				t.Errorf("cleanup ErrTxDone was retained: %v", err)
			}
			calls := test.state.snapshot()
			if callbacks != test.callbacks || calls.begins != 1 || calls.commits != test.commits || calls.rollbacks != test.rollbacks {
				t.Fatalf("unexpected calls: callbacks=%d begin=%d commit=%d rollback=%d", callbacks, calls.begins, calls.commits, calls.rollbacks)
			}
			if calls.options != (driver.TxOptions{}) {
				t.Errorf("nil options changed driver defaults: %+v", calls.options)
			}
		})
	}
}

// TestWithTxContextAndOptions 确认事务使用调用方的 context 和非默认选项.
func TestWithTxContextAndOptions(t *testing.T) {
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "transaction")
	state := &testDriverState{}
	db := newTestDB(t, state)
	opts := &sql.TxOptions{Isolation: sql.LevelSerializable, ReadOnly: true}
	if err := sqldb.WithTx(ctx, db, opts, func(*sql.Tx) error { return nil }); err != nil {
		t.Fatal(err)
	}
	calls := state.snapshot()
	if calls.beginCtx != ctx || calls.options.Isolation != driver.IsolationLevel(opts.Isolation) || !calls.options.ReadOnly {
		t.Fatalf("transaction arguments changed: context=%v options=%+v", calls.beginCtx == ctx, calls.options)
	}
}

// TestWithTxValue 只在提交成功后交付结果, 各错误阶段均丢弃回调产生的非零值.
func TestWithTxValue(t *testing.T) {
	beginErr := errors.New("begin failed")
	businessErr := errors.New("business failed")
	commitErr := errors.New("commit failed")
	type result struct{ ID int }
	cases := []struct {
		name        string
		state       testDriverState
		callbackErr error
		wantErr     error
	}{
		{name: "success"},
		{name: "begin failure", state: testDriverState{beginErr: beginErr}, wantErr: beginErr},
		{name: "business failure", callbackErr: businessErr, wantErr: businessErr},
		{name: "commit failure", state: testDriverState{commitErr: commitErr}, wantErr: commitErr},
	}
	for i := range cases {
		test := &cases[i]
		t.Run(test.name, func(t *testing.T) {
			db := newTestDB(t, &test.state)
			value, err := sqldb.WithTxValue(context.Background(), db, nil, func(*sql.Tx) (result, error) {
				return result{ID: 42}, test.callbackErr
			})
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) || value != (result{}) {
					t.Fatalf("expected zero result and %v, got %+v, %v", test.wantErr, value, err)
				}
				return
			}
			if err != nil || value.ID != 42 {
				t.Fatalf("expected committed result, got %+v, %v", value, err)
			}
		})
	}
}

// TestWithTxPanic 验证自然 panic 传播, 独立回滚失败不得替换原 panic.
func TestWithTxPanic(t *testing.T) {
	panicValue := &struct{ message string }{message: "callback panic"}
	state := &testDriverState{rollbackErr: errors.New("rollback failed")}
	db := newTestDB(t, state)
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_ = sqldb.WithTx(context.Background(), db, nil, func(*sql.Tx) error {
			panic(panicValue)
		})
	}()
	calls := state.snapshot()
	if recovered != panicValue || calls.commits != 0 || calls.rollbacks != 1 {
		t.Fatalf("panic or cleanup changed: panic=%v commit=%d rollback=%d", recovered, calls.commits, calls.rollbacks)
	}
}

// TestWithTxCanceledBeforeBegin 验证已取消输入不进入回调且不交付结果.
func TestWithTxCanceledBeforeBegin(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	state := &testDriverState{}
	db := newTestDB(t, state)
	called := false
	value, err := sqldb.WithTxValue(ctx, db, nil, func(*sql.Tx) (int, error) {
		called = true
		return 42, nil
	})
	if !errors.Is(err, context.Canceled) || value != 0 || called || state.snapshot().begins != 0 {
		t.Fatalf("canceled begin leaked work: value=%d called=%v error=%v", value, called, err)
	}
}

// TestWithTxAutomaticRollback 固定取消先于提交, 不要求标准库交付内部回滚失败.
func TestWithTxAutomaticRollback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rollbackErr := errors.New("automatic rollback failed")
	state := &testDriverState{rollbackErr: rollbackErr, rollbackDone: make(chan struct{})}
	db := newTestDB(t, state)
	value, err := sqldb.WithTxValue(ctx, db, nil, func(*sql.Tx) (int, error) {
		cancel()
		// 等待自动回滚取得结束标记, 避免将调度竞争写成固定错误断言.
		waitTestSignal(t, state.rollbackDone)
		return 42, nil
	})
	calls := state.snapshot()
	if !errors.Is(err, sql.ErrTxDone) || errors.Is(err, rollbackErr) || value != 0 || calls.commits != 0 || calls.rollbacks != 1 {
		t.Fatalf("automatic rollback contract changed: value=%d error=%v commit=%d rollback=%d", value, err, calls.commits, calls.rollbacks)
	}
}
