// Package ledger is the SQLite store of leases. It is the only place port
// numbers are persisted. All writes run inside an immediate transaction so
// that concurrent CLI and MCP processes cannot double-allocate; the UNIQUE
// constraint on leases.port is the last line of defence.
package ledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Ledger is an open ledger database.
type Ledger struct {
	db   *sql.DB
	path string
}

// FileName is the ledger file name inside the state directory.
const FileName = "ledger.sqlite"

const schema = `
CREATE TABLE IF NOT EXISTS projects (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS slots (
  id INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects(id),
  name TEXT NOT NULL,
  root_path TEXT,
  infra_from_slot_id INTEGER REFERENCES slots(id),
  created_at TEXT NOT NULL,
  UNIQUE(project_id, name)
);
CREATE TABLE IF NOT EXISTS blocks (
  id INTEGER PRIMARY KEY,
  slot_id INTEGER NOT NULL REFERENCES slots(id) ON DELETE CASCADE,
  base INTEGER NOT NULL,
  size INTEGER NOT NULL,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS leases (
  id INTEGER PRIMARY KEY,
  slot_id INTEGER NOT NULL REFERENCES slots(id) ON DELETE CASCADE,
  service TEXT NOT NULL,
  port INTEGER NOT NULL UNIQUE,
  tier TEXT NOT NULL,
  proto TEXT NOT NULL,
  pin_reason TEXT,
  pinned_at TEXT,
  last_seen_at TEXT,
  UNIQUE(slot_id, service)
);
CREATE TABLE IF NOT EXISTS audit (
  id INTEGER PRIMARY KEY,
  at TEXT NOT NULL,
  actor TEXT NOT NULL CHECK(actor IN ('cli','mcp')),
  op TEXT NOT NULL,
  target TEXT NOT NULL
);
`

// Open creates the state directory (0700) and the ledger (0600) if needed and opens it.
func Open(stateDir string) (*Ledger, error) {
	if _, err := os.Stat(stateDir); errors.Is(err, os.ErrNotExist) {
		// Only a directory we create gets its mode forced; an existing one the
		// user pointed us at is left alone (doctor reports it).
		if err := os.MkdirAll(stateDir, 0o700); err != nil {
			return nil, fmt.Errorf("create state dir: %w", err)
		}
		_ = os.Chmod(stateDir, 0o700)
	}
	path := filepath.Join(stateDir, FileName)
	// Create the file ourselves so the mode is right from the first byte.
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create ledger: %w", err)
	}
	_ = f.Close()
	_ = os.Chmod(path, 0o600)

	// Probing happens inside write transactions (a block is verified free before
	// it is recorded), so a neighbour may wait a while: 15s covers a slow block.
	dsn := "file:" + url.PathEscape(path) + "?_txlock=immediate&_pragma=busy_timeout(15000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	// Several processes may open a brand-new ledger at the same instant (a
	// SessionStart hook in parallel sessions, a parallel test). Switching the
	// journal mode and creating the schema need an exclusive lock that the busy
	// handler does not always wait for, so retry briefly instead of failing.
	var migrateErr error
	for attempt := 0; attempt < 50; attempt++ {
		_, migrateErr = db.Exec(schema)
		if migrateErr == nil {
			break
		}
		msg := strings.ToLower(migrateErr.Error())
		if !strings.Contains(msg, "locked") && !strings.Contains(msg, "busy") {
			break
		}
		time.Sleep(time.Duration(20+rand.Intn(60)) * time.Millisecond)
	}
	if migrateErr != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate ledger: %w", migrateErr)
	}
	// WAL side files inherit the directory mode; tighten them anyway when present.
	for _, suffix := range []string{"-wal", "-shm"} {
		_ = os.Chmod(path+suffix, 0o600)
	}
	return &Ledger{db: db, path: path}, nil
}

// Path returns the ledger file path.
func (l *Ledger) Path() string { return l.path }

// Close closes the database.
func (l *Ledger) Close() error { return l.db.Close() }

// Project is a row of projects.
type Project struct {
	ID   int64
	Name string
}

// Slot is a row of slots.
type Slot struct {
	ID        int64
	ProjectID int64
	Name      string
	RootPath  string
	InfraFrom *int64
	CreatedAt time.Time
}

// Block is a contiguous range [Base, Base+Size) leased to a slot.
type Block struct {
	ID     int64
	SlotID int64
	Base   int
	Size   int
}

// Lease is one service's port.
type Lease struct {
	ID         int64
	SlotID     int64
	Service    string
	Port       int
	Tier       string
	Proto      string
	PinReason  string
	PinnedAt   *time.Time
	LastSeenAt *time.Time
}

// Pinned reports whether the lease is a pinned (out-of-pool or legacy) port.
func (l Lease) Pinned() bool { return l.PinnedAt != nil }

// Tx wraps a write transaction. Callers use the Tx methods and must Commit or Rollback.
type Tx struct {
	tx *sql.Tx
}

// Begin starts an immediate write transaction.
func (l *Ledger) Begin(ctx context.Context) (*Tx, error) {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &Tx{tx: tx}, nil
}

// Commit commits.
func (t *Tx) Commit() error { return t.tx.Commit() }

// Rollback rolls back (safe to call after Commit).
func (t *Tx) Rollback() { _ = t.tx.Rollback() }

func now() string { return time.Now().UTC().Format(time.RFC3339) }

func parseTime(s sql.NullString) *time.Time {
	if !s.Valid || s.String == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s.String)
	if err != nil {
		return nil
	}
	return &t
}

// querier is satisfied by *sql.DB and *sql.Tx.
type querier interface {
	QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
}

// --- projects ---

// EnsureProject returns the project named name, creating it if needed.
func (t *Tx) EnsureProject(ctx context.Context, name string) (*Project, error) {
	if _, err := t.tx.ExecContext(ctx, `INSERT OR IGNORE INTO projects(name, created_at) VALUES(?, ?)`, name, now()); err != nil {
		return nil, err
	}
	return getProject(ctx, t.tx, name)
}

// GetProject returns the project or nil when unknown.
func (l *Ledger) GetProject(ctx context.Context, name string) (*Project, error) {
	return getProject(ctx, l.db, name)
}

func getProject(ctx context.Context, q querier, name string) (*Project, error) {
	var p Project
	err := q.QueryRowContext(ctx, `SELECT id, name FROM projects WHERE name = ?`, name).Scan(&p.ID, &p.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ProjectSummary is a project with its slot names (no numbers).
type ProjectSummary struct {
	Name  string
	Slots []string
}

// ListProjects returns every project and its slot names.
func (l *Ledger) ListProjects(ctx context.Context) ([]ProjectSummary, error) {
	rows, err := l.db.QueryContext(ctx, `SELECT p.name, s.name FROM projects p LEFT JOIN slots s ON s.project_id = p.id ORDER BY p.name, s.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProjectSummary
	for rows.Next() {
		var p string
		var s sql.NullString
		if err := rows.Scan(&p, &s); err != nil {
			return nil, err
		}
		if len(out) == 0 || out[len(out)-1].Name != p {
			out = append(out, ProjectSummary{Name: p})
		}
		if s.Valid {
			out[len(out)-1].Slots = append(out[len(out)-1].Slots, s.String)
		}
	}
	return out, rows.Err()
}

// --- slots ---

const slotCols = `id, project_id, name, COALESCE(root_path, ''), infra_from_slot_id, created_at`

func scanSlot(row interface{ Scan(...any) error }) (*Slot, error) {
	var s Slot
	var infra sql.NullInt64
	var created string
	if err := row.Scan(&s.ID, &s.ProjectID, &s.Name, &s.RootPath, &infra, &created); err != nil {
		return nil, err
	}
	if infra.Valid {
		v := infra.Int64
		s.InfraFrom = &v
	}
	s.CreatedAt, _ = time.Parse(time.RFC3339, created)
	return &s, nil
}

func getSlot(ctx context.Context, q querier, projectID int64, name string) (*Slot, error) {
	s, err := scanSlot(q.QueryRowContext(ctx, `SELECT `+slotCols+` FROM slots WHERE project_id = ? AND name = ?`, projectID, name))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return s, err
}

// GetSlot returns the slot or nil.
func (l *Ledger) GetSlot(ctx context.Context, projectID int64, name string) (*Slot, error) {
	return getSlot(ctx, l.db, projectID, name)
}

// GetSlot returns the slot or nil, inside the transaction.
func (t *Tx) GetSlot(ctx context.Context, projectID int64, name string) (*Slot, error) {
	return getSlot(ctx, t.tx, projectID, name)
}

func slotByID(ctx context.Context, q querier, id int64) (*Slot, error) {
	s, err := scanSlot(q.QueryRowContext(ctx, `SELECT `+slotCols+` FROM slots WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return s, err
}

// SlotByID returns the slot or nil.
func (l *Ledger) SlotByID(ctx context.Context, id int64) (*Slot, error) {
	return slotByID(ctx, l.db, id)
}

// SlotByID returns the slot or nil, inside the transaction.
func (t *Tx) SlotByID(ctx context.Context, id int64) (*Slot, error) { return slotByID(ctx, t.tx, id) }

func slotByRoot(ctx context.Context, q querier, projectID int64, root string) (*Slot, error) {
	s, err := scanSlot(q.QueryRowContext(ctx, `SELECT `+slotCols+` FROM slots WHERE project_id = ? AND root_path = ? ORDER BY id LIMIT 1`, projectID, root))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return s, err
}

// SlotByRoot returns the slot bound to a worktree root within a project, or nil.
func (l *Ledger) SlotByRoot(ctx context.Context, projectID int64, root string) (*Slot, error) {
	return slotByRoot(ctx, l.db, projectID, root)
}

// SlotByRoot returns the slot bound to a worktree root, inside the transaction.
func (t *Tx) SlotByRoot(ctx context.Context, projectID int64, root string) (*Slot, error) {
	return slotByRoot(ctx, t.tx, projectID, root)
}

// ListSlots returns a project's slots in creation order.
func (l *Ledger) ListSlots(ctx context.Context, projectID int64) ([]Slot, error) {
	return listSlots(ctx, l.db, projectID)
}

// ListSlots returns a project's slots inside the transaction.
func (t *Tx) ListSlots(ctx context.Context, projectID int64) ([]Slot, error) {
	return listSlots(ctx, t.tx, projectID)
}

func listSlots(ctx context.Context, q querier, projectID int64) ([]Slot, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+slotCols+` FROM slots WHERE project_id = ? ORDER BY created_at, id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Slot
	for rows.Next() {
		s, err := scanSlot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

// CreateSlot inserts a slot. infraFrom may be nil.
func (t *Tx) CreateSlot(ctx context.Context, projectID int64, name, root string, infraFrom *int64) (*Slot, error) {
	var rootV any
	if root != "" {
		rootV = root
	}
	var infraV any
	if infraFrom != nil {
		infraV = *infraFrom
	}
	if _, err := t.tx.ExecContext(ctx, `INSERT INTO slots(project_id, name, root_path, infra_from_slot_id, created_at) VALUES(?, ?, ?, ?, ?)`,
		projectID, name, rootV, infraV, now()); err != nil {
		return nil, err
	}
	return getSlot(ctx, t.tx, projectID, name)
}

// BindRoot records the worktree root of a slot (clearing it from any other slot of the project).
func (t *Tx) BindRoot(ctx context.Context, slotID int64, projectID int64, root string) error {
	if _, err := t.tx.ExecContext(ctx, `UPDATE slots SET root_path = NULL WHERE project_id = ? AND root_path = ? AND id <> ?`, projectID, root, slotID); err != nil {
		return err
	}
	_, err := t.tx.ExecContext(ctx, `UPDATE slots SET root_path = ? WHERE id = ?`, root, slotID)
	return err
}

// Dependents returns the names of slots that reference slotID via infra_from.
func (l *Ledger) Dependents(ctx context.Context, slotID int64) ([]string, error) {
	rows, err := l.db.QueryContext(ctx, `SELECT name FROM slots WHERE infra_from_slot_id = ? ORDER BY name`, slotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// DeleteSlot removes a slot with its blocks and leases. Dependents must be handled by the caller.
func (t *Tx) DeleteSlot(ctx context.Context, slotID int64) error {
	if _, err := t.tx.ExecContext(ctx, `DELETE FROM leases WHERE slot_id = ?`, slotID); err != nil {
		return err
	}
	if _, err := t.tx.ExecContext(ctx, `DELETE FROM blocks WHERE slot_id = ?`, slotID); err != nil {
		return err
	}
	_, err := t.tx.ExecContext(ctx, `DELETE FROM slots WHERE id = ?`, slotID)
	return err
}

// --- blocks ---

func listBlocks(ctx context.Context, q querier, slotID int64) ([]Block, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, slot_id, base, size FROM blocks WHERE slot_id = ? ORDER BY base`, slotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Block
	for rows.Next() {
		var b Block
		if err := rows.Scan(&b.ID, &b.SlotID, &b.Base, &b.Size); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// Blocks returns the blocks of a slot.
func (l *Ledger) Blocks(ctx context.Context, slotID int64) ([]Block, error) {
	return listBlocks(ctx, l.db, slotID)
}

// Blocks returns the blocks of a slot inside the transaction.
func (t *Tx) Blocks(ctx context.Context, slotID int64) ([]Block, error) {
	return listBlocks(ctx, t.tx, slotID)
}

// AllBlocks returns every block in the ledger, ordered by base.
func (t *Tx) AllBlocks(ctx context.Context) ([]Block, error) {
	rows, err := t.tx.QueryContext(ctx, `SELECT id, slot_id, base, size FROM blocks ORDER BY base`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Block
	for rows.Next() {
		var b Block
		if err := rows.Scan(&b.ID, &b.SlotID, &b.Base, &b.Size); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// AllLeasedPorts returns every port currently leased (pooled or pinned).
func (t *Tx) AllLeasedPorts(ctx context.Context) (map[int]bool, error) {
	rows, err := t.tx.QueryContext(ctx, `SELECT port FROM leases`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]bool{}
	for rows.Next() {
		var p int
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out[p] = true
	}
	return out, rows.Err()
}

// InsertBlock records a block for a slot.
func (t *Tx) InsertBlock(ctx context.Context, slotID int64, base, size int) (*Block, error) {
	res, err := t.tx.ExecContext(ctx, `INSERT INTO blocks(slot_id, base, size, created_at) VALUES(?, ?, ?, ?)`, slotID, base, size, now())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &Block{ID: id, SlotID: slotID, Base: base, Size: size}, nil
}

// --- leases ---

const leaseCols = `id, slot_id, service, port, tier, proto, COALESCE(pin_reason, ''), pinned_at, last_seen_at`

func scanLease(row interface{ Scan(...any) error }) (*Lease, error) {
	var l Lease
	var pinned, seen sql.NullString
	if err := row.Scan(&l.ID, &l.SlotID, &l.Service, &l.Port, &l.Tier, &l.Proto, &l.PinReason, &pinned, &seen); err != nil {
		return nil, err
	}
	l.PinnedAt = parseTime(pinned)
	l.LastSeenAt = parseTime(seen)
	return &l, nil
}

func listLeases(ctx context.Context, q querier, slotID int64) ([]Lease, error) {
	rows, err := q.QueryContext(ctx, `SELECT `+leaseCols+` FROM leases WHERE slot_id = ? ORDER BY port`, slotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Lease
	for rows.Next() {
		l, err := scanLease(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *l)
	}
	return out, rows.Err()
}

// Leases returns the leases of a slot.
func (l *Ledger) Leases(ctx context.Context, slotID int64) ([]Lease, error) {
	return listLeases(ctx, l.db, slotID)
}

// Leases returns the leases of a slot inside the transaction.
func (t *Tx) Leases(ctx context.Context, slotID int64) ([]Lease, error) {
	return listLeases(ctx, t.tx, slotID)
}

// InsertLease records a pooled lease.
func (t *Tx) InsertLease(ctx context.Context, slotID int64, service string, port int, tier, proto string) error {
	_, err := t.tx.ExecContext(ctx, `INSERT INTO leases(slot_id, service, port, tier, proto) VALUES(?, ?, ?, ?, ?)`, slotID, service, port, tier, proto)
	return err
}

// InsertPinnedLease records a pinned lease.
func (t *Tx) InsertPinnedLease(ctx context.Context, slotID int64, service string, port int, tier, proto, reason string) error {
	_, err := t.tx.ExecContext(ctx, `INSERT INTO leases(slot_id, service, port, tier, proto, pin_reason, pinned_at) VALUES(?, ?, ?, ?, ?, ?, ?)`,
		slotID, service, port, tier, proto, reason, now())
	return err
}

// UpdateLeaseMeta refreshes tier/proto from the manifest.
func (t *Tx) UpdateLeaseMeta(ctx context.Context, id int64, tier, proto string) error {
	_, err := t.tx.ExecContext(ctx, `UPDATE leases SET tier = ?, proto = ? WHERE id = ?`, tier, proto, id)
	return err
}

// DeleteLease removes a lease by id.
func (t *Tx) DeleteLease(ctx context.Context, id int64) error {
	_, err := t.tx.ExecContext(ctx, `DELETE FROM leases WHERE id = ?`, id)
	return err
}

// Touch records that a lease was seen listening.
func (l *Ledger) Touch(ctx context.Context, id int64) error {
	_, err := l.db.ExecContext(ctx, `UPDATE leases SET last_seen_at = ? WHERE id = ?`, now(), id)
	return err
}

// PortOwner identifies who holds a port.
type PortOwner struct {
	Project string
	Slot    string
	Service string
	Pinned  bool
}

// OwnerOfPort returns who leases port, or nil when free.
func (t *Tx) OwnerOfPort(ctx context.Context, port int) (*PortOwner, error) {
	var o PortOwner
	var pinned sql.NullString
	err := t.tx.QueryRowContext(ctx, `SELECT p.name, s.name, l.service, l.pinned_at FROM leases l JOIN slots s ON s.id = l.slot_id JOIN projects p ON p.id = s.project_id WHERE l.port = ?`, port).
		Scan(&o.Project, &o.Slot, &o.Service, &pinned)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	o.Pinned = pinned.Valid
	return &o, nil
}

// PinInfo is one pinned lease with its owner, for doctor and slot ls.
type PinInfo struct {
	Project  string
	Slot     string
	Service  string
	Port     int
	Reason   string
	PinnedAt time.Time
}

// ListPins returns every pinned lease.
func (l *Ledger) ListPins(ctx context.Context) ([]PinInfo, error) {
	rows, err := l.db.QueryContext(ctx, `SELECT p.name, s.name, l.service, l.port, COALESCE(l.pin_reason,''), l.pinned_at FROM leases l JOIN slots s ON s.id = l.slot_id JOIN projects p ON p.id = s.project_id WHERE l.pinned_at IS NOT NULL ORDER BY l.pinned_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PinInfo
	for rows.Next() {
		var p PinInfo
		var at string
		if err := rows.Scan(&p.Project, &p.Slot, &p.Service, &p.Port, &p.Reason, &at); err != nil {
			return nil, err
		}
		p.PinnedAt, _ = time.Parse(time.RFC3339, at)
		out = append(out, p)
	}
	return out, rows.Err()
}

// PinnedSlotsOfProject returns the names of the project's slots that hold pinned leases.
func (t *Tx) PinnedSlotsOfProject(ctx context.Context, projectID int64) ([]string, error) {
	rows, err := t.tx.QueryContext(ctx, `SELECT DISTINCT s.name FROM leases l JOIN slots s ON s.id = l.slot_id WHERE s.project_id = ? AND l.pinned_at IS NOT NULL`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// --- audit ---

// Audit records an operation. Targets are names, never numbers.
func (t *Tx) Audit(ctx context.Context, actor, op, target string) error {
	_, err := t.tx.ExecContext(ctx, `INSERT INTO audit(at, actor, op, target) VALUES(?, ?, ?, ?)`, now(), actor, op, target)
	return err
}

// LeaseCount returns the number of leases in the ledger.
func (l *Ledger) LeaseCount(ctx context.Context) (int, error) {
	var n int
	err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM leases`).Scan(&n)
	return n, err
}

// AuditRows returns every audit entry as "op target" strings, oldest first.
func (l *Ledger) AuditRows(ctx context.Context) ([]string, error) {
	rows, err := l.db.QueryContext(ctx, `SELECT op, target FROM audit ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var op, target string
		if err := rows.Scan(&op, &target); err != nil {
			return nil, err
		}
		out = append(out, op+" "+target)
	}
	return out, rows.Err()
}

// BlockOwner identifies the slot whose pooled block contains a port.
type BlockOwner struct {
	Project string
	Slot    string
}

// BlockOwnerOfPort returns the slot whose block covers port, or nil.
func (t *Tx) BlockOwnerOfPort(ctx context.Context, port int) (*BlockOwner, error) {
	var o BlockOwner
	err := t.tx.QueryRowContext(ctx, `SELECT p.name, s.name FROM blocks b JOIN slots s ON s.id = b.slot_id JOIN projects p ON p.id = s.project_id WHERE b.base <= ? AND ? < b.base + b.size`, port, port).Scan(&o.Project, &o.Slot)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}
