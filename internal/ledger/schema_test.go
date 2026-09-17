package ledger

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// setUserVersion rewrites the schema version the way a different binary would.
func setUserVersion(t *testing.T, path string, v int) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("PRAGMA user_version = " + strconv.Itoa(v)); err != nil {
		t.Fatal(err)
	}
}

func userVersion(t *testing.T, l *Ledger) int {
	t.Helper()
	v, err := readSchemaVersion(context.Background(), l.db)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestNewLedgerIsStamped(t *testing.T) {
	l := openTest(t)
	if got := userVersion(t, l); got != SchemaVersion {
		t.Fatalf("user_version = %d, want %d", got, SchemaVersion)
	}
}

// A ledger written before versioning existed has user_version 0: it keeps its
// leases and becomes version 1.
func TestUnversionedLedgerKeepsLeases(t *testing.T) {
	ctx := context.Background()
	state := filepath.Join(t.TempDir(), "state")
	l, err := Open(state)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := l.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	pr, _ := tx.EnsureProject(ctx, "shop")
	s, err := tx.CreateSlot(ctx, pr.ID, "default", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.AllocateBlock(ctx, s.ID, 4, pool(), nil); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	path := l.Path()
	_ = l.Close()
	setUserVersion(t, path, 0)

	l, err = Open(state)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if got := userVersion(t, l); got != SchemaVersion {
		t.Fatalf("user_version = %d, want %d", got, SchemaVersion)
	}
	p, err := l.GetProject(ctx, "shop")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.GetSlot(ctx, p.ID, "default"); err != nil {
		t.Fatalf("slot lost across the version stamp: %v", err)
	}
}

// A newer ledger is refused without changing a byte of it, and the message
// tells the user what to do without leaking a port number.
func TestNewerLedgerIsRefusedUntouched(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	l, err := Open(state)
	if err != nil {
		t.Fatal(err)
	}
	path := l.Path()
	_ = l.Close()
	setUserVersion(t, path, SchemaVersion+1)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Open(state)
	var newer *NewerSchemaError
	if !errors.As(err, &newer) {
		t.Fatalf("Open = %v, want NewerSchemaError", err)
	}
	if newer.Found != SchemaVersion+1 || newer.Supported != SchemaVersion {
		t.Fatalf("unexpected versions in %+v", newer)
	}
	if regexp.MustCompile(`\b[0-9]{4,5}\b`).MatchString(err.Error()) {
		t.Fatalf("message looks like it contains a port number: %s", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("refusing a newer ledger modified the file")
	}
}

// A process that opened the ledger before it was upgraded notices on its next check.
func TestCheckSchemaSeesLaterUpgrade(t *testing.T) {
	l := openTest(t)
	if err := l.CheckSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	setUserVersion(t, l.Path(), SchemaVersion+1)
	var newer *NewerSchemaError
	if err := l.CheckSchema(context.Background()); !errors.As(err, &newer) {
		t.Fatalf("CheckSchema = %v, want NewerSchemaError", err)
	}
}
