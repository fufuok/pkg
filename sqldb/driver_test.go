package sqldb_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

// testResultSet 固定一个结果集的列和行, 不解析 SQL 或模拟数据库业务.
type testResultSet struct {
	columns []string
	values  [][]driver.Value
}

// testDriverState 注入生命周期失败并记录调用, 配置在开池前固定.
// 自动取消清理可能在后台执行, 记录和通知由互斥锁保护.
type testDriverState struct {
	mu sync.Mutex

	beginErr    error
	commitErr   error
	rollbackErr error
	queryErr    error
	nextErr     error
	closeErr    error
	sets        []testResultSet

	beginCtx context.Context
	options  driver.TxOptions
	queryCtx context.Context
	query    string
	args     []driver.NamedValue

	begins         int
	commits        int
	rollbacks      int
	queries        int
	rowsClosed     int
	nextResultSets int
	rollbackDone   chan struct{}
	closeDone      chan struct{}
}

type testDriverSnapshot struct {
	beginCtx       context.Context
	options        driver.TxOptions
	queryCtx       context.Context
	query          string
	args           []driver.NamedValue
	begins         int
	commits        int
	rollbacks      int
	queries        int
	rowsClosed     int
	nextResultSets int
}

type (
	testConnector struct{ state *testDriverState }
	testDriver    struct{}
	testConn      struct{ state *testDriverState }
	testTx        struct{ state *testDriverState }
	testRows      struct {
		state *testDriverState
		set   int
		row   int
	}
)

// newTestDB 创建每用例独有的 Connector, 避免污染 sql.Register 的全局表.
func newTestDB(t *testing.T, state *testDriverState) *sql.DB {
	t.Helper()
	if len(state.sets) == 0 {
		state.sets = []testResultSet{{columns: []string{"value"}}}
	}
	db := sql.OpenDB(testConnector{state: state})
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close test database: %v", err)
		}
	})
	return db
}

// snapshot 在后台回滚或关闭期间安全读取记录, 参数切片独立复制.
func (s *testDriverState) snapshot() testDriverSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return testDriverSnapshot{
		beginCtx: s.beginCtx, options: s.options,
		queryCtx: s.queryCtx, query: s.query, args: append([]driver.NamedValue(nil), s.args...),
		begins: s.begins, commits: s.commits, rollbacks: s.rollbacks,
		queries: s.queries, rowsClosed: s.rowsClosed, nextResultSets: s.nextResultSets,
	}
}

// waitTestSignal 等待已经发生的清理事件, 用上限防止失败用例挂住整个套件.
func waitTestSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for driver cleanup")
	}
}

// Connect 只建立内存测试连接, 已取消的 context 不会取得连接.
func (c testConnector) Connect(ctx context.Context) (driver.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return testConn{state: c.state}, nil
}

// Driver 满足 Connector 契约, 不向标准库注册任何全局驱动.
func (c testConnector) Driver() driver.Driver { return testDriver{} }

// Open 拒绝绕过 Connector, 让测试意外走旧驱动入口时立即失败.
func (testDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("test driver requires a connector")
}

// Prepare 不属于这些助手的职责, 意外调用时返回明确错误.
func (testConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("test driver does not support prepare")
}

// Close 释放无外部资源的连接, 结果集关闭单独记录.
func (testConn) Close() error { return nil }

// Begin 拒绝没有 context 的事务入口, 确保助手确实使用 BeginTx.
func (testConn) Begin() (driver.Tx, error) {
	return nil, errors.New("test driver requires BeginTx")
}

// BeginTx 记录原 context 和选项, 不自行增加隔离级别校验或默认值.
func (c testConn) BeginTx(ctx context.Context, options driver.TxOptions) (driver.Tx, error) {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	c.state.begins++
	c.state.beginCtx = ctx
	c.state.options = options
	if c.state.beginErr != nil {
		return nil, c.state.beginErr
	}
	return testTx{state: c.state}, nil
}

// QueryContext 保存标准库转换后的参数, 每次查询从首个结果集重新开始.
func (c testConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	c.state.queries++
	c.state.queryCtx = ctx
	c.state.query = query
	c.state.args = append([]driver.NamedValue(nil), args...)
	if c.state.queryErr != nil {
		return nil, c.state.queryErr
	}
	return &testRows{state: c.state}, nil
}

// Commit 只记录驱动调用, 事务结束标记仍由真正的 database/sql 管理.
func (tx testTx) Commit() error {
	tx.state.mu.Lock()
	defer tx.state.mu.Unlock()
	tx.state.commits++
	return tx.state.commitErr
}

// Rollback 通知底层回滚已经被调用, 不将自动清理的错误伪造为公开可见错误.
func (tx testTx) Rollback() error {
	tx.state.mu.Lock()
	defer tx.state.mu.Unlock()
	tx.state.rollbacks++
	if tx.state.rollbackDone != nil {
		close(tx.state.rollbackDone)
	}
	return tx.state.rollbackErr
}

// Columns 返回当前集的固定列, 允许后续结果集具有不同形状.
func (r *testRows) Columns() []string { return r.state.sets[r.set].columns }

// Close 注入独立关闭失败, 由 sql.Rows 的真实实现控制幂等和错误可见性.
func (r *testRows) Close() error {
	r.state.mu.Lock()
	defer r.state.mu.Unlock()
	r.state.rowsClosed++
	if r.state.closeDone != nil {
		close(r.state.closeDone)
	}
	return r.state.closeErr
}

// Next 顺序交付当前集, 首集末尾可注入迭代失败以验证自动关闭的错误优先级.
func (r *testRows) Next(dest []driver.Value) error {
	set := r.state.sets[r.set]
	if r.row < len(set.values) {
		copy(dest, set.values[r.row])
		r.row++
		return nil
	}
	if r.set == 0 && r.state.nextErr != nil {
		return r.state.nextErr
	}
	return io.EOF
}

// HasNextResultSet 让标准库决定首集 EOF 后是否自动关闭, 不主动推进.
func (r *testRows) HasNextResultSet() bool { return r.set+1 < len(r.state.sets) }

// NextResultSet 记录实际跨集推进, 单集助手不应调用这个入口.
func (r *testRows) NextResultSet() error {
	r.state.mu.Lock()
	r.state.nextResultSets++
	r.state.mu.Unlock()
	if !r.HasNextResultSet() {
		return io.EOF
	}
	r.set++
	r.row = 0
	return nil
}
