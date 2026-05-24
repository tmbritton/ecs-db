package world

import "context"

// mockTx is a hand-written mock transaction for unit testing.
type mockTx struct {
	insertEntityResults []insertEntityResult
	insertEntityIdx     int
	insertCompErr       error
	commitErr           error
	rollbackErr         error
	committed           bool
	rolledBack          bool
}

type insertEntityResult struct {
	id  int64
	err error
}

func (m *mockTx) InsertEntity(ctx context.Context, entityType string, createdTick int64) (int64, error) {
	if m.insertEntityIdx >= len(m.insertEntityResults) {
		return 0, nil
	}
	r := m.insertEntityResults[m.insertEntityIdx]
	m.insertEntityIdx++
	return r.id, r.err
}

func (m *mockTx) InsertComponent(ctx context.Context, entityID int64, compName string, values map[string]interface{}) error {
	return m.insertCompErr
}

func (m *mockTx) Commit() error {
	m.committed = true
	return m.commitErr
}

func (m *mockTx) Rollback() error {
	m.rolledBack = true
	return m.rollbackErr
}

// mockStore is a hand-written mock EntityStore.
type mockStore struct {
	beginTxErr     error
	currentTick    int64
	currentTickErr error
	tx             *mockTx
}

func (m *mockStore) BeginTx(ctx context.Context) (Tx, error) {
	if m.beginTxErr != nil {
		return nil, m.beginTxErr
	}
	return m.tx, nil
}

func (m *mockStore) GetCurrentTick(ctx context.Context) (int64, error) {
	return m.currentTick, m.currentTickErr
}
