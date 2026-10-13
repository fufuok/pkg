package sqldb_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/fufuok/pkg/sqldb"
)

type (
	testScanError    struct{ message string }
	rejectingScanner struct{ err error }
)

// Error 提供可由 errors.As 辨认的业务扫描错误, 不依赖标准库转换文本.
func (e *testScanError) Error() string { return e.message }

// Scan 拒绝当前字段, 用标准库承诺的 Scanner 错误包装验证身份保留.
func (s rejectingScanner) Scan(any) error { return s.err }

// TestForEachRows 验证显式扫描, NULL, 行顺序及标准查询参数透传.
func TestForEachRows(t *testing.T) {
	type contextKey struct{}
	type row struct {
		ID   int64
		Note sql.NullString
	}
	ctx := context.WithValue(context.Background(), contextKey{}, "query")
	state := &testDriverState{sets: []testResultSet{{
		columns: []string{"id", "note"},
		values:  [][]driver.Value{{int64(1), nil}, {int64(2), "second"}},
	}}}
	db := newTestDB(t, state)
	query := "SELECT id, note FROM entries WHERE id > ? AND label = @label"
	var got []row
	err := sqldb.ForEach(ctx, db, query, func(scanner sqldb.Scanner) error {
		if _, ok := scanner.(*sql.Rows); ok {
			t.Error("scanner exposed result set ownership")
		}
		if _, ok := scanner.(interface{ Next() bool }); ok {
			t.Error("scanner exposed result set iteration")
		}
		var value row
		if err := scanner.Scan(&value.ID, &value.Note); err != nil {
			return fmt.Errorf("scan entry: %w", err)
		}
		got = append(got, value)
		return nil
	}, int64(0), sql.Named("label", "active"))
	if err != nil {
		t.Fatal(err)
	}
	want := []row{{ID: 1}, {ID: 2, Note: sql.NullString{String: "second", Valid: true}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected rows: got %+v, want %+v", got, want)
	}
	calls := state.snapshot()
	wantArgs := []driver.NamedValue{{Ordinal: 1, Value: int64(0)}, {Name: "label", Ordinal: 2, Value: "active"}}
	if calls.queryCtx != ctx || calls.query != query || !reflect.DeepEqual(calls.args, wantArgs) || calls.rowsClosed != 1 {
		t.Fatalf("query arguments or ownership changed: %+v", calls)
	}
}

// TestForEachErrors 区分提前关闭的可见失败和自动关闭已遮蔽的底层失败.
func TestForEachErrors(t *testing.T) {
	queryErr := errors.New("query failed")
	businessErr := errors.New("row callback failed")
	iterationErr := errors.New("iteration failed")
	closeErr := errors.New("close failed")
	cases := []struct {
		name        string
		state       testDriverState
		callbackErr error
		wantErrors  []error
		hiddenError error
		callbacks   int
		closed      int
	}{
		{name: "empty result", closed: 1},
		{name: "query failure", state: testDriverState{queryErr: queryErr}, wantErrors: []error{queryErr}},
		{name: "callback failure", callbackErr: businessErr, wantErrors: []error{businessErr}, callbacks: 1, closed: 1},
		{name: "callback and close failure", state: testDriverState{closeErr: closeErr}, callbackErr: businessErr, wantErrors: []error{businessErr, closeErr}, callbacks: 1, closed: 1},
		{name: "EOF close failure", state: testDriverState{closeErr: closeErr}, wantErrors: []error{closeErr}, callbacks: 2, closed: 1},
		{name: "iteration masks close failure", state: testDriverState{nextErr: iterationErr, closeErr: closeErr}, wantErrors: []error{iterationErr}, hiddenError: closeErr, callbacks: 2, closed: 1},
	}
	for i := range cases {
		test := &cases[i]
		t.Run(test.name, func(t *testing.T) {
			if test.name != "empty result" {
				test.state.sets = []testResultSet{{columns: []string{"value"}, values: [][]driver.Value{{int64(1)}, {int64(2)}}}}
			}
			db := newTestDB(t, &test.state)
			callbacks := 0
			err := sqldb.ForEach(context.Background(), db, "SELECT value", func(sqldb.Scanner) error {
				callbacks++
				return test.callbackErr
			})
			if len(test.wantErrors) == 0 && err != nil {
				t.Fatalf("unexpected iteration error: %v", err)
			}
			for _, want := range test.wantErrors {
				if !errors.Is(err, want) {
					t.Errorf("expected error %v, got %v", want, err)
				}
			}
			if test.hiddenError != nil && errors.Is(err, test.hiddenError) {
				t.Errorf("standard library hidden error was synthesized: %v", err)
			}
			if errors.Is(err, closeErr) && strings.Count(err.Error(), closeErr.Error()) != 1 {
				t.Errorf("close error was duplicated: %v", err)
			}
			if callbacks != test.callbacks || test.state.snapshot().rowsClosed != test.closed {
				t.Fatalf("unexpected callback/close calls: callbacks=%d close=%d", callbacks, test.state.snapshot().rowsClosed)
			}
		})
	}
}

// TestForEachScanError 验证字段 Scanner 的错误身份保留并及时关闭.
func TestForEachScanError(t *testing.T) {
	state := &testDriverState{sets: []testResultSet{{columns: []string{"value"}, values: [][]driver.Value{{int64(1)}, {int64(2)}}}}}
	db := newTestDB(t, state)
	scanErr := &testScanError{message: "field rejected"}
	callbacks := 0
	err := sqldb.ForEach(context.Background(), db, "SELECT value", func(scanner sqldb.Scanner) error {
		callbacks++
		value := rejectingScanner{err: scanErr}
		return scanner.Scan(&value)
	})
	var fieldErr *testScanError
	if !errors.Is(err, scanErr) || !errors.As(err, &fieldErr) || callbacks != 1 || state.snapshot().rowsClosed != 1 {
		t.Fatalf("scan error identity or cleanup changed: error=%v callbacks=%d close=%d", err, callbacks, state.snapshot().rowsClosed)
	}
}

// TestForEachPanic 保持原 panic, 关闭失败只能清理而不能作为第二个返回值交付.
func TestForEachPanic(t *testing.T) {
	panicValue := &struct{ message string }{message: "row panic"}
	state := &testDriverState{
		closeErr: errors.New("close failed"),
		sets:     []testResultSet{{columns: []string{"value"}, values: [][]driver.Value{{int64(1)}}}},
	}
	db := newTestDB(t, state)
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_ = sqldb.ForEach(context.Background(), db, "SELECT value", func(sqldb.Scanner) error {
			panic(panicValue)
		})
	}()
	if recovered != panicValue || state.snapshot().rowsClosed != 1 {
		t.Fatalf("panic or cleanup changed: panic=%v close=%d", recovered, state.snapshot().rowsClosed)
	}
}

// TestForEachCanceled 固定自动关闭已经发生, 避免 EOF 和取消的竞争影响断言.
func TestForEachCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	state := &testDriverState{
		closeDone: make(chan struct{}),
		sets:      []testResultSet{{columns: []string{"value"}, values: [][]driver.Value{{int64(1)}, {int64(2)}}}},
	}
	db := newTestDB(t, state)
	callbacks := 0
	err := sqldb.ForEach(ctx, db, "SELECT value", func(sqldb.Scanner) error {
		callbacks++
		cancel()
		waitTestSignal(t, state.closeDone)
		return nil
	})
	if !errors.Is(err, context.Canceled) || callbacks != 1 || state.snapshot().rowsClosed != 1 {
		t.Fatalf("cancellation contract changed: error=%v callbacks=%d close=%d", err, callbacks, state.snapshot().rowsClosed)
	}
}

// TestForEachFirstResultSet 确认首集结束就关闭, 包括首集为空而后续非空的情况.
func TestForEachFirstResultSet(t *testing.T) {
	for _, first := range [][][]driver.Value{nil, {{int64(1)}, {int64(2)}}} {
		t.Run(fmt.Sprintf("first rows %d", len(first)), func(t *testing.T) {
			state := &testDriverState{sets: []testResultSet{
				{columns: []string{"value"}, values: first},
				{columns: []string{"value"}, values: [][]driver.Value{{int64(3)}}},
			}}
			db := newTestDB(t, state)
			got, err := sqldb.Collect(context.Background(), db, "SELECT first; SELECT second", func(scanner sqldb.Scanner) (int, error) {
				var value int
				err := scanner.Scan(&value)
				return value, err
			})
			if err != nil || got == nil || len(got) != len(first) || state.snapshot().nextResultSets != 0 || state.snapshot().rowsClosed != 1 {
				t.Fatalf("result set ownership changed: values=%v error=%v calls=%+v", got, err, state.snapshot())
			}
			for i, value := range got {
				if int64(value) != first[i][0] {
					t.Fatalf("unexpected first result set: %v", got)
				}
			}
		})
	}
}

// TestForEachQueryers 验证 DB, Tx 和专用 Conn 均可使用, helper 不结束外部所有者.
func TestForEachQueryers(t *testing.T) {
	ctx := context.Background()
	state := &testDriverState{sets: []testResultSet{{columns: []string{"value"}, values: [][]driver.Value{{int64(1)}}}}}
	db := newTestDB(t, state)
	visit := func(q sqldb.Queryer) {
		t.Helper()
		if err := sqldb.ForEach(ctx, q, "SELECT value", func(scanner sqldb.Scanner) error {
			var value int
			return scanner.Scan(&value)
		}); err != nil {
			t.Fatal(err)
		}
	}
	visit(db)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	visit(tx)
	if err := tx.Commit(); err != nil {
		t.Fatalf("ForEach ended caller transaction: %v", err)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	visit(conn)
	if err := conn.PingContext(ctx); err != nil {
		t.Fatalf("ForEach closed caller connection: %v", err)
	}
	if calls := state.snapshot(); calls.queries != 3 || calls.rowsClosed != 3 {
		t.Fatalf("unexpected query ownership: %+v", calls)
	}
}

// TestCollect 结果集完整成功后才交付列表, mapper 或清理失败丢弃部分结果.
func TestCollect(t *testing.T) {
	queryErr := errors.New("query failed")
	mapperErr := errors.New("mapper failed")
	iterationErr := errors.New("iteration failed")
	closeErr := errors.New("close failed")
	cases := []struct {
		name      string
		state     testDriverState
		values    [][]driver.Value
		mapperErr error
		wantErr   error
		want      []int
	}{
		{name: "success", values: [][]driver.Value{{int64(1)}, {int64(2)}}, want: []int{1, 2}},
		{name: "empty", want: []int{}},
		{name: "query failure", state: testDriverState{queryErr: queryErr}, wantErr: queryErr},
		{name: "mapper failure", values: [][]driver.Value{{int64(1)}, {int64(2)}}, mapperErr: mapperErr, wantErr: mapperErr},
		{name: "iteration failure", state: testDriverState{nextErr: iterationErr}, values: [][]driver.Value{{int64(1)}}, wantErr: iterationErr},
		{name: "close failure", state: testDriverState{closeErr: closeErr}, values: [][]driver.Value{{int64(1)}}, wantErr: closeErr},
	}
	for i := range cases {
		test := &cases[i]
		t.Run(test.name, func(t *testing.T) {
			test.state.sets = []testResultSet{{columns: []string{"value"}, values: test.values}}
			db := newTestDB(t, &test.state)
			got, err := sqldb.Collect(context.Background(), db, "SELECT value", func(scanner sqldb.Scanner) (int, error) {
				var value int
				if err := scanner.Scan(&value); err != nil {
					return 0, err
				}
				if value == 2 && test.mapperErr != nil {
					return value, test.mapperErr
				}
				return value, nil
			})
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) || got != nil {
					t.Fatalf("expected nil list and %v, got %v, %v", test.wantErr, got, err)
				}
				return
			}
			if err != nil || got == nil || !reflect.DeepEqual(got, test.want) {
				t.Fatalf("expected complete list %v, got %v, %v", test.want, got, err)
			}
		})
	}
}
