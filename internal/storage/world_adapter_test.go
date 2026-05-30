package storage_test

import (
	"database/sql"
	"testing"

	"github.com/tmbritton/ecs-db/internal/storage"
	_ "modernc.org/sqlite"
)

func setupAdapterDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	stmts := []string{
		`CREATE TABLE entities (id INTEGER PRIMARY KEY AUTOINCREMENT, entity_type TEXT NOT NULL, created_tick INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE comp_position (entity_id INTEGER PRIMARY KEY, x REAL NOT NULL DEFAULT 0, y REAL NOT NULL DEFAULT 0)`,
		`CREATE TABLE comp_health   (entity_id INTEGER PRIMARY KEY, hp REAL NOT NULL DEFAULT 100)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	return db
}

func beginAdapterTx(t *testing.T, db *sql.DB) *sql.Tx {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() { tx.Rollback() })
	return tx
}

// ── txWorldWriter ─────────────────────────────────────────────────────────────

func TestTxWorldWriter_SpawnEntity(t *testing.T) {
	db := setupAdapterDB(t)
	tx := beginAdapterTx(t, db)
	w := storage.NewTxWorldWriter(tx)

	id, err := w.SpawnEntity("Goblin")
	if err != nil {
		t.Fatalf("SpawnEntity: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var entityType string
	_ = db.QueryRow("SELECT entity_type FROM entities WHERE id = ?", id).Scan(&entityType)
	if entityType != "Goblin" {
		t.Errorf("entity_type = %q, want Goblin", entityType)
	}
}

func TestTxWorldWriter_AttachComponent(t *testing.T) {
	db := setupAdapterDB(t)
	tx := beginAdapterTx(t, db)
	w := storage.NewTxWorldWriter(tx)

	_, _ = tx.Exec("INSERT INTO entities (entity_type, created_tick) VALUES ('Goblin', 0)")
	if err := w.AttachComponent(1, "Position", map[string]any{"x": 3.0, "y": 4.0}); err != nil {
		t.Fatalf("AttachComponent: %v", err)
	}
	_ = tx.Commit()

	var x, y float64
	_ = db.QueryRow("SELECT x, y FROM comp_position WHERE entity_id = 1").Scan(&x, &y)
	if x != 3.0 || y != 4.0 {
		t.Errorf("position = (%v, %v), want (3, 4)", x, y)
	}
}

func TestTxWorldWriter_DetachComponent(t *testing.T) {
	db := setupAdapterDB(t)
	_, _ = db.Exec("INSERT INTO entities (entity_type, created_tick) VALUES ('Goblin', 0)")
	_, _ = db.Exec("INSERT INTO comp_position (entity_id, x, y) VALUES (1, 0, 0)")

	tx := beginAdapterTx(t, db)
	w := storage.NewTxWorldWriter(tx)
	if err := w.DetachComponent(1, "Position"); err != nil {
		t.Fatalf("DetachComponent: %v", err)
	}
	_ = tx.Commit()

	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM comp_position WHERE entity_id = 1").Scan(&count)
	if count != 0 {
		t.Errorf("comp_position rows = %d after detach, want 0", count)
	}
}

func TestTxWorldWriter_SetComponentValue(t *testing.T) {
	db := setupAdapterDB(t)
	_, _ = db.Exec("INSERT INTO entities (entity_type, created_tick) VALUES ('Goblin', 0)")
	_, _ = db.Exec("INSERT INTO comp_health (entity_id, hp) VALUES (1, 100)")

	tx := beginAdapterTx(t, db)
	w := storage.NewTxWorldWriter(tx)
	if err := w.SetComponentValue(1, "Health", "hp", 75.0); err != nil {
		t.Fatalf("SetComponentValue: %v", err)
	}
	_ = tx.Commit()

	var hp float64
	_ = db.QueryRow("SELECT hp FROM comp_health WHERE entity_id = 1").Scan(&hp)
	if hp != 75.0 {
		t.Errorf("hp = %v, want 75", hp)
	}
}

// ── txWorldReader ─────────────────────────────────────────────────────────────

func TestTxWorldReader_GetComponentValue(t *testing.T) {
	db := setupAdapterDB(t)
	_, _ = db.Exec("INSERT INTO comp_health (entity_id, hp) VALUES (1, 42)")

	tx := beginAdapterTx(t, db)
	r := storage.NewTxWorldReader(tx)
	val, err := r.GetComponentValue(1, "Health", "hp")
	if err != nil {
		t.Fatalf("GetComponentValue: %v", err)
	}
	if val == nil {
		t.Fatal("GetComponentValue returned nil")
	}
	switch v := val.(type) {
	case float64:
		if v != 42 {
			t.Errorf("hp = %v, want 42", v)
		}
	case int64:
		if v != 42 {
			t.Errorf("hp = %v, want 42", v)
		}
	default:
		t.Errorf("unexpected type %T", val)
	}
}

func TestTxWorldReader_GetComponentValue_Missing(t *testing.T) {
	db := setupAdapterDB(t)
	tx := beginAdapterTx(t, db)
	r := storage.NewTxWorldReader(tx)
	val, err := r.GetComponentValue(99, "Health", "hp")
	if err != nil {
		t.Fatalf("GetComponentValue on missing row: %v", err)
	}
	if val != nil {
		t.Errorf("expected nil for missing entity, got %v", val)
	}
}

func TestTxWorldReader_HasComponent(t *testing.T) {
	db := setupAdapterDB(t)
	_, _ = db.Exec("INSERT INTO comp_health (entity_id, hp) VALUES (1, 100)")

	tx := beginAdapterTx(t, db)
	r := storage.NewTxWorldReader(tx)

	has, err := r.HasComponent(1, "Health")
	if err != nil {
		t.Fatalf("HasComponent: %v", err)
	}
	if !has {
		t.Error("HasComponent = false, want true")
	}

	has2, _ := r.HasComponent(99, "Health")
	if has2 {
		t.Error("HasComponent(missing) = true, want false")
	}
}

func TestTxWorldReader_FindEntityByType(t *testing.T) {
	db := setupAdapterDB(t)
	res, _ := db.Exec("INSERT INTO entities (entity_type, created_tick) VALUES ('Player', 0)")
	wantID, _ := res.LastInsertId()

	tx := beginAdapterTx(t, db)
	r := storage.NewTxWorldReader(tx)

	id, err := r.FindEntityByType("Player")
	if err != nil {
		t.Fatalf("FindEntityByType: %v", err)
	}
	if id != wantID {
		t.Errorf("id = %d, want %d", id, wantID)
	}
}

func TestTxWorldReader_FindEntityByType_Missing(t *testing.T) {
	db := setupAdapterDB(t)
	tx := beginAdapterTx(t, db)
	r := storage.NewTxWorldReader(tx)

	_, err := r.FindEntityByType("Dragon")
	if err == nil {
		t.Error("expected error for missing entity type, got nil")
	}
}
