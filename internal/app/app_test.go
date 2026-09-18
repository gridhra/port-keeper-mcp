package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gridhra/port-keeper-mcp/internal/config"
	"github.com/gridhra/port-keeper-mcp/internal/ledger"
	"github.com/gridhra/port-keeper-mcp/internal/manifest"
	"github.com/gridhra/port-keeper-mcp/internal/probe"
)

const shop = `
[project]
name = "shop"
block_size = 4

[[service]]
name = "web"
env = "WEB_PORT"

[[service]]
name = "api"
env = "API_PORT"

[[service]]
name = "db"
env = "DB_PORT"
proto = "tcp"
tier = "infra"
`

const blog = `
[project]
name = "blog"
block_size = 4

[[service]]
name = "web"
env = "WEB_PORT"
`

// newApp builds an App on a temporary ledger with a tiny pool and no real probing.
func newApp(t *testing.T) *App {
	t.Helper()
	l, err := ledger.Open(filepath.Join(t.TempDir(), "state")) // a directory port-keeper creates itself
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	cfg := config.Default()
	cfg.Pool = config.Pool{Ranges: [][]int{{20000, 20031}}}
	cfg.MCP = config.MCP{}
	return &App{Cfg: cfg, Ledger: l, Actor: ActorCLI, Probe: func(int) bool { return false }, Listeners: func(context.Context) (map[int]probe.Owner, bool) { return nil, false }}
}

// project writes a manifest into a fresh directory (no git) and returns it.
func project(t *testing.T, text string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, manifest.FileName), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestSlotLifecycleAndInfraSharing(t *testing.T) {
	ctx := context.Background()
	a := newApp(t)
	dir := project(t, shop)
	t.Setenv("PORT_KEEPER_SLOT", "")

	c, err := a.Resolve(ctx, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if c.SlotName != "1" || c.Slot != nil || c.SlotSource != "default" {
		t.Fatalf("fresh resolve: %+v", c)
	}
	r1, created, err := a.SlotNew(ctx, c, NewSlotOptions{BindRoot: true})
	if err != nil || !created {
		t.Fatalf("slot new: %v %v", created, err)
	}
	if r1.Ports["web"] != 20000 || r1.Ports["api"] != 20001 || r1.Ports["db"] != 20002 {
		t.Fatalf("ports %v", r1.Ports)
	}
	// Idempotent.
	if _, created, err := a.SlotNew(ctx, c, NewSlotOptions{Name: "1"}); err != nil || created {
		t.Fatalf("second slot new: %v %v", created, err)
	}
	// The worktree root now resolves to slot 1.
	c, _ = a.Resolve(ctx, dir, "")
	if c.SlotSource != "root" || c.Slot == nil {
		t.Fatalf("root binding: %+v", c)
	}
	// Slot 2 shares infra from 1: db is not leased, resolves to 20002.
	c2, _ := a.Resolve(ctx, dir, "2")
	r2, _, err := a.SlotNew(ctx, c2, NewSlotOptions{Name: "2", InfraFrom: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if r2.Ports["db"] != 20002 || r2.InfraSlot != "1" || r2.Ports["web"] != 20004 {
		t.Fatalf("slot 2: %+v", r2)
	}
	leases, _ := a.Ledger.Leases(ctx, c2.Slot.ID)
	if len(leases) != 2 {
		t.Fatalf("slot 2 must lease only app services, got %d", len(leases))
	}
	// Owner cannot be released while referenced, unless cascade.
	if _, err := a.ReleaseSlot(ctx, c, "1", false, false); err == nil {
		t.Fatal("release of infra owner should be refused")
	}
	released, err := a.ReleaseSlot(ctx, c, "1", false, true)
	if err != nil || len(released) != 2 {
		t.Fatalf("cascade: %v %v", released, err)
	}
	if n, _ := a.Ledger.LeaseCount(ctx); n != 0 {
		t.Fatalf("leases left: %d", n)
	}
}

func TestSyncFollowsManifest(t *testing.T) {
	ctx := context.Background()
	a := newApp(t)
	dir := project(t, shop)
	c, _ := a.Resolve(ctx, dir, "")
	if _, _, err := a.SlotNew(ctx, c, NewSlotOptions{}); err != nil {
		t.Fatal(err)
	}
	// Add two services: the block (4) is full after one, so a second block is leased.
	extra := shop + "\n[[service]]\nname = \"mail\"\nenv = \"MAIL_PORT\"\n\n[[service]]\nname = \"worker\"\nenv = \"WORKER_PORT\"\n"
	os.WriteFile(filepath.Join(dir, manifest.FileName), []byte(extra), 0o644)
	c, _ = a.Resolve(ctx, dir, "")
	r, err := a.Sync(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	if r.Ports["mail"] != 20003 || r.Ports["worker"] != 20004 {
		t.Fatalf("growth: %v", r.Ports)
	}
	blocks, _ := a.Ledger.Blocks(ctx, c.Slot.ID)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
	// Existing ports are stable across a reorder; a removed service is dropped.
	reordered := "[project]\nname = \"shop\"\nblock_size = 4\n[[service]]\nname = \"api\"\nenv = \"API_PORT\"\n[[service]]\nname = \"web\"\nenv = \"WEB_PORT\"\n"
	os.WriteFile(filepath.Join(dir, manifest.FileName), []byte(reordered), 0o644)
	c, _ = a.Resolve(ctx, dir, "")
	r, err = a.Sync(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	if r.Ports["web"] != 20000 || r.Ports["api"] != 20001 || len(r.Ports) != 2 {
		t.Fatalf("stability: %v", r.Ports)
	}
}

func TestPinArbitrationAcrossProjects(t *testing.T) {
	ctx := context.Background()
	a := newApp(t)
	shopDir := project(t, shop)
	blogDir := project(t, blog)
	cs, _ := a.Resolve(ctx, shopDir, "")
	if _, _, err := a.SlotNew(ctx, cs, NewSlotOptions{}); err != nil {
		t.Fatal(err)
	}
	cb, _ := a.Resolve(ctx, blogDir, "")
	if _, _, err := a.SlotNew(ctx, cb, NewSlotOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := a.Pin(ctx, cs, "web", 3000, "", false); err == nil {
		t.Fatal("pin without reason accepted")
	}
	if err := a.Pin(ctx, cs, "web", 3000, "bookmarks", false); err != nil {
		t.Fatal(err)
	}
	err := a.Pin(ctx, cb, "web", 3000, "me too", false)
	if err == nil {
		t.Fatal("second project pinned the same port")
	}
	if got := err.Error(); !contains(got, "shop/1/web") {
		t.Fatalf("error must name the owner: %s", got)
	}
	// Pins only in the default slot.
	cs2, _ := a.Resolve(ctx, shopDir, "2")
	if _, _, err := a.SlotNew(ctx, cs2, NewSlotOptions{Name: "2"}); err != nil {
		t.Fatal(err)
	}
	if err := a.Pin(ctx, cs2, "web", 3001, "x", false); err == nil {
		t.Fatal("pin in a non-default slot accepted")
	}
	// A port taken by an untracked listener is refused without force.
	a.Probe = func(p int) bool { return p == 4000 }
	if err := a.Pin(ctx, cb, "web", 4000, "x", false); err == nil {
		t.Fatal("pin on a busy port accepted")
	}
	a.Probe = func(int) bool { return false }
	// Unpin returns web to the pool and the pinned port frees up.
	r, err := a.Unpin(ctx, cs, "web")
	if err != nil {
		t.Fatal(err)
	}
	if r.Ports["web"] < 20000 {
		t.Fatalf("after unpin web is %d", r.Ports["web"])
	}
	if err := a.Pin(ctx, cb, "web", 3000, "now free", false); err != nil {
		t.Fatalf("pin after unpin: %v", err)
	}
}

func TestReassignAvoidsBusyPort(t *testing.T) {
	ctx := context.Background()
	a := newApp(t)
	dir := project(t, shop)
	c, _ := a.Resolve(ctx, dir, "")
	if _, _, err := a.SlotNew(ctx, c, NewSlotOptions{}); err != nil {
		t.Fatal(err)
	}
	old, port, err := a.Reassign(ctx, c, "web")
	if err != nil {
		t.Fatal(err)
	}
	if old != 20000 || port == 20000 {
		t.Fatalf("reassign gave %d -> %d", old, port)
	}
	if port != 20003 {
		t.Fatalf("expected the next free offset in the block, got %d", port)
	}
}

func TestStatusHijackHeuristic(t *testing.T) {
	// The cwd heuristic only fires when an owner is known; with no real listener
	// the state is "leased". This guards the default path from false positives.
	ctx := context.Background()
	a := newApp(t)
	dir := project(t, shop)
	c, _ := a.Resolve(ctx, dir, "")
	if _, _, err := a.SlotNew(ctx, c, NewSlotOptions{}); err != nil {
		t.Fatal(err)
	}
	rows, err := a.Status(ctx, c, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.State != StateLeased {
			t.Fatalf("%s: %s", r.Service, r.State)
		}
	}
}

func TestDotenvRefusesTrackedFile(t *testing.T) {
	ctx := context.Background()
	a := newApp(t)
	dir := project(t, shop)
	if err := run(dir, "git", "init", "-q"); err != nil {
		t.Skip("git not available")
	}
	_ = run(dir, "git", "config", "user.email", "t@example.com")
	_ = run(dir, "git", "config", "user.name", "t")
	os.WriteFile(filepath.Join(dir, ".env.local"), []byte("X=1\n"), 0o600)
	_ = run(dir, "git", "add", ".env.local")
	_ = run(dir, "git", "commit", "-q", "-m", "oops")
	c, _ := a.Resolve(ctx, dir, "")
	if _, _, err := a.SlotNew(ctx, c, NewSlotOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.WriteDotenv(ctx, c, "A=1\n"); err == nil {
		t.Fatal("wrote into a git-tracked file")
	}
}

func run(dir string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	return cmd.Run()
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

func TestDotenvRefusesSymlinkAndEscape(t *testing.T) {
	ctx := context.Background()
	a := newApp(t)
	dir := project(t, shop)
	c, _ := a.Resolve(ctx, dir, "")
	if _, _, err := a.SlotNew(ctx, c, NewSlotOptions{}); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "elsewhere.env")
	os.WriteFile(target, []byte("X=1\n"), 0o600)
	if err := os.Symlink(target, filepath.Join(dir, ".env.local")); err != nil {
		t.Skip("symlinks not supported")
	}
	if _, _, err := a.WriteDotenv(ctx, c, "A=1\n"); err == nil {
		t.Fatal("wrote through a symlink")
	}
	os.Remove(filepath.Join(dir, ".env.local"))
	escaped := shop + "\n[render]\ndotenv_path = \"../outside.env\"\n"
	os.WriteFile(filepath.Join(dir, manifest.FileName), []byte(escaped), 0o644)
	c, err := a.Resolve(ctx, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.WriteDotenv(ctx, c, "A=1\n"); err == nil {
		t.Fatal("wrote outside the manifest directory")
	}
}

func TestSlotResolutionOrder(t *testing.T) {
	ctx := context.Background()
	a := newApp(t)
	dir := project(t, shop)
	c, _ := a.Resolve(ctx, dir, "")
	if _, _, err := a.SlotNew(ctx, c, NewSlotOptions{Name: "feat-x"}); err != nil { // unbound slot
		t.Fatal(err)
	}
	// No binding: the environment variable decides.
	t.Setenv("PORT_KEEPER_SLOT", "feat-x")
	c, err := a.Resolve(ctx, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if c.SlotName != "feat-x" || c.SlotSource != "env" || c.Slot == nil {
		t.Fatalf("env resolution: %+v", c)
	}
	// A binding outranks a stale PORT_KEEPER_SLOT left in the shell by a hook.
	c, _ = a.Resolve(ctx, dir, "")
	if _, _, err := a.SlotNew(ctx, c, NewSlotOptions{Name: "2", BindRoot: true}); err != nil {
		t.Fatal(err)
	}
	c, _ = a.Resolve(ctx, dir, "")
	if c.SlotName != "2" || c.SlotSource != "root" {
		t.Fatalf("binding must outrank env: %+v", c)
	}
	if c.IgnoredEnvSlot != "feat-x" || len(c.Warnings()) != 1 || !strings.Contains(c.Warnings()[0], "PORT_KEEPER_SLOT=feat-x") {
		t.Fatalf("expected a stale-env warning, got %+v", c.Warnings())
	}
	t.Setenv("PORT_KEEPER_SLOT", "2")
	c, _ = a.Resolve(ctx, dir, "")
	if len(c.Warnings()) != 0 {
		t.Fatalf("no warning when env agrees with the binding: %v", c.Warnings())
	}
	// Explicit wins over everything.
	c, _ = a.Resolve(ctx, dir, "feat-x")
	if c.SlotName != "feat-x" || c.SlotSource != "explicit" {
		t.Fatalf("explicit resolution: %+v", c)
	}
}

// A second worktree without a slot must not silently receive the default
// slot's ports (that slot belongs to the first worktree).
func TestUnboundWorktreeIsRefusedDefaultSlot(t *testing.T) {
	ctx := context.Background()
	a := newApp(t)
	main := project(t, shop)
	t.Setenv("PORT_KEEPER_SLOT", "")
	c, _ := a.Resolve(ctx, main, "")
	if _, _, err := a.SlotNew(ctx, c, NewSlotOptions{BindRoot: true}); err != nil {
		t.Fatal(err)
	}
	other := project(t, shop) // same project name, different directory: a sibling worktree
	c, err := a.Resolve(ctx, other, "")
	if err != nil {
		t.Fatal(err)
	}
	if c.UnboundRoot == "" || c.Slot != nil || c.SlotSource != "default" {
		t.Fatalf("expected unbound context, got %+v", c)
	}
	if err := c.RequireBound(); err == nil || !strings.Contains(err.Error(), "slot new") {
		t.Fatalf("RequireBound: %v", err)
	}
	if _, err := a.Sync(ctx, c); err == nil {
		t.Fatal("Sync handed out another worktree's slot")
	}
	// slot_new without a name leases a fresh slot and binds this worktree.
	r, created, err := a.SlotNew(ctx, c, NewSlotOptions{BindRoot: true})
	if err != nil || !created || r.Slot != "2" || r.Ports["web"] == 20000 {
		t.Fatalf("slot new from unbound worktree: %+v created=%v err=%v", r, created, err)
	}
	c, _ = a.Resolve(ctx, other, "")
	if c.SlotName != "2" || c.SlotSource != "root" || c.UnboundRoot != "" {
		t.Fatalf("after slot new: %+v", c)
	}
	// Sharing the main slot on purpose is still possible with --slot.
	c, _ = a.Resolve(ctx, other, "1")
	if err := c.RequireBound(); err != nil || c.Slot == nil {
		t.Fatalf("explicit --slot 1: %+v %v", c, err)
	}
}

func TestOrphanSlotCanBeReleased(t *testing.T) {
	ctx := context.Background()
	a := newApp(t)
	dir := project(t, shop)
	c, _ := a.Resolve(ctx, dir, "")
	if _, _, err := a.SlotNew(ctx, c, NewSlotOptions{BindRoot: true}); err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(dir) // the repository is gone
	stale, err := a.StaleSlots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 1 || !stale[0].Orphan {
		t.Fatalf("stale = %+v", stale)
	}
	if err := a.ReleaseOrphan(ctx, stale[0].Project, stale[0].SlotID); err != nil {
		t.Fatal(err)
	}
	if n, _ := a.Ledger.LeaseCount(ctx); n != 0 {
		t.Fatalf("leases left: %d", n)
	}
}

func TestStatusDoesNotTouchWhenNothingListens(t *testing.T) {
	ctx := context.Background()
	a := newApp(t)
	dir := project(t, shop)
	c, _ := a.Resolve(ctx, dir, "")
	if _, _, err := a.SlotNew(ctx, c, NewSlotOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Status(ctx, c, true); err != nil {
		t.Fatal(err)
	}
	leases, _ := a.Ledger.Leases(ctx, c.Slot.ID)
	for _, l := range leases {
		if l.LastSeenAt != nil {
			t.Fatalf("%s: last_seen set although nothing listens", l.Service)
		}
	}
	// With a listener, last_seen is set.
	a.Probe = func(int) bool { return true }
	if _, err := a.Status(ctx, c, false); err != nil {
		t.Fatal(err)
	}
	leases, _ = a.Ledger.Leases(ctx, c.Slot.ID)
	if leases[0].LastSeenAt != nil {
		t.Fatal("read-only status must not touch last_seen")
	}
	if _, err := a.Status(ctx, c, true); err != nil {
		t.Fatal(err)
	}
	leases, _ = a.Ledger.Leases(ctx, c.Slot.ID)
	if leases[0].LastSeenAt == nil {
		t.Fatal("last_seen not set for an active lease")
	}
}

func TestAuditHasNoNumbersAndLedgerIsPrivate(t *testing.T) {
	ctx := context.Background()
	a := newApp(t)
	dir := project(t, shop)
	c, _ := a.Resolve(ctx, dir, "")
	if _, _, err := a.SlotNew(ctx, c, NewSlotOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := a.Pin(ctx, c, "web", 3000, "legacy", false); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ReleaseSlot(ctx, c, "1", false, false); err != nil {
		t.Fatal(err)
	}
	rows, err := a.Ledger.AuditRows(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("no audit rows")
	}
	for _, r := range rows {
		for _, needle := range []string{"3000", "2000"} {
			if contains(r, needle) {
				t.Fatalf("audit row leaks a number: %s", r)
			}
		}
	}
	st, err := os.Stat(a.Ledger.Path())
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("ledger mode %04o", st.Mode().Perm())
	}
	dst, _ := os.Stat(filepath.Dir(a.Ledger.Path()))
	if dst.Mode().Perm() != 0o700 {
		t.Fatalf("state dir mode %04o", dst.Mode().Perm())
	}
}

func TestPinInsideAnotherBlockIsRefusedAndSyncSurvives(t *testing.T) {
	ctx := context.Background()
	a := newApp(t)
	shopDir := project(t, shop)
	blogDir := project(t, blog)
	cs, _ := a.Resolve(ctx, shopDir, "")
	if _, _, err := a.SlotNew(ctx, cs, NewSlotOptions{}); err != nil { // shop/1 owns 20000-20003
		t.Fatal(err)
	}
	cb, _ := a.Resolve(ctx, blogDir, "")
	if _, _, err := a.SlotNew(ctx, cb, NewSlotOptions{}); err != nil { // blog/1 owns 20004-20007
		t.Fatal(err)
	}
	// 20003 is a free offset inside shop's block: refused without force.
	err := a.Pin(ctx, cb, "web", 20003, "squat", false)
	if err == nil || !strings.Contains(err.Error(), "shop/1") {
		t.Fatalf("pin inside another block: %v", err)
	}
	// With force it is allowed, and shop's next sync must still succeed by skipping it.
	if err := a.Pin(ctx, cb, "web", 20003, "squat", true); err != nil {
		t.Fatal(err)
	}
	extra := shop + "\n[[service]]\nname = \"mail\"\nenv = \"MAIL_PORT\"\n"
	os.WriteFile(filepath.Join(shopDir, manifest.FileName), []byte(extra), 0o644)
	cs, _ = a.Resolve(ctx, shopDir, "")
	r, err := a.Sync(ctx, cs)
	if err != nil {
		t.Fatalf("sync after a foreign pin inside the block: %v", err)
	}
	if r.Ports["mail"] == 20003 || r.Ports["mail"] < 20000 {
		t.Fatalf("mail got %d", r.Ports["mail"])
	}
}

func TestNarrowedPoolIsHonoured(t *testing.T) {
	ctx := context.Background()
	a := newApp(t)
	dir := project(t, shop)
	c, _ := a.Resolve(ctx, dir, "")
	if _, _, err := a.SlotNew(ctx, c, NewSlotOptions{}); err != nil { // block 20000-20003, 3 leases
		t.Fatal(err)
	}
	a.Cfg.Pool.Ranges = [][]int{{20008, 20031}} // the old block is now outside the pool
	extra := shop + "\n[[service]]\nname = \"mail\"\nenv = \"MAIL_PORT\"\n"
	os.WriteFile(filepath.Join(dir, manifest.FileName), []byte(extra), 0o644)
	c, _ = a.Resolve(ctx, dir, "")
	r, err := a.Sync(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	if r.Ports["mail"] < 20008 {
		t.Fatalf("mail %d was leased outside the narrowed pool", r.Ports["mail"])
	}
}

func TestRemovedPinnedServiceBlocksSync(t *testing.T) {
	ctx := context.Background()
	a := newApp(t)
	dir := project(t, shop)
	c, _ := a.Resolve(ctx, dir, "")
	if _, _, err := a.SlotNew(ctx, c, NewSlotOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := a.Pin(ctx, c, "api", 3002, "legacy", false); err != nil {
		t.Fatal(err)
	}
	without := "[project]\nname = \"shop\"\nblock_size = 4\n[[service]]\nname = \"web\"\nenv = \"WEB_PORT\"\n"
	os.WriteFile(filepath.Join(dir, manifest.FileName), []byte(without), 0o644)
	c, _ = a.Resolve(ctx, dir, "")
	if _, err := a.Sync(ctx, c); err == nil || !strings.Contains(err.Error(), "unpin api") {
		t.Fatalf("sync dropped a pinned service silently: %v", err)
	}
}

func TestSlotNewWithoutNameIsIdempotentOnBoundWorktree(t *testing.T) {
	ctx := context.Background()
	a := newApp(t)
	dir := project(t, shop)
	c, _ := a.Resolve(ctx, dir, "")
	if _, created, err := a.SlotNew(ctx, c, NewSlotOptions{BindRoot: true}); err != nil || !created {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		c, _ = a.Resolve(ctx, dir, "")
		r, created, err := a.SlotNew(ctx, c, NewSlotOptions{BindRoot: true})
		if err != nil || created || r.Slot != "1" {
			t.Fatalf("retry %d: created=%v slot=%s err=%v", i, created, r.Slot, err)
		}
	}
	slots, _ := a.Ledger.ListSlots(ctx, c.Project.ID)
	if len(slots) != 1 {
		t.Fatalf("%d slots after retries", len(slots))
	}
}

func TestSlotNameFromBranch(t *testing.T) {
	long := strings.Repeat("a", 70) + "-b"
	cases := []struct{ in, want string }{
		{"main", "main"},
		{"feature/Login_v2.0", "feature-login-v2-0"},
		{"--x--", "x"},
		{"a//b", "a-b"},
		{long, strings.Repeat("a", 63)},
	}
	for _, c := range cases {
		got, err := SlotNameFromBranch(c.in)
		if err != nil || got != c.want {
			t.Errorf("SlotNameFromBranch(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
	for _, in := range []string{"", "日本語", "///"} {
		if got, err := SlotNameFromBranch(in); err == nil {
			t.Errorf("SlotNameFromBranch(%q) = %q; want error", in, got)
		}
	}
}

func TestEnvDrift(t *testing.T) {
	ctx := context.Background()
	a := newApp(t)
	dir := project(t, shop)
	numbers := regexp.MustCompile(`\d{4,}`)
	check := func(want string) {
		t.Helper()
		c, err := a.Resolve(ctx, dir, "")
		if err != nil {
			t.Fatal(err)
		}
		got, err := a.EnvDrift(ctx, c)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("drift: got %q, want %q", got, want)
		}
		if numbers.MatchString(got) {
			t.Fatalf("drift reason carries a number: %q", got)
		}
	}
	check("slot 1 has no ports yet")
	c, _ := a.Resolve(ctx, dir, "")
	if _, _, err := a.SlotNew(ctx, c, NewSlotOptions{BindRoot: true}); err != nil {
		t.Fatal(err)
	}
	check(".env.local does not exist")
	c, _ = a.Resolve(ctx, dir, "")
	text, _, err := a.EnvText(ctx, c, "dotenv")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.WriteDotenv(ctx, c, text); err != nil {
		t.Fatal(err)
	}
	check("")
	// A service added to the manifest has no lease until env runs.
	os.WriteFile(filepath.Join(dir, manifest.FileName), []byte(shop+"\n[[service]]\nname = \"mail\"\nenv = \"MAIL_PORT\"\n"), 0o644)
	check("service mail has no port yet")
	// Once leased, the file is still the old rendering.
	c, _ = a.Resolve(ctx, dir, "")
	if _, err := a.Sync(ctx, c); err != nil {
		t.Fatal(err)
	}
	check(".env.local is out of date")
	// A block rendered for another slot, a missing block, and a missing file.
	envPath := filepath.Join(dir, ".env.local")
	content, _ := os.ReadFile(envPath)
	os.WriteFile(envPath, []byte(strings.Replace(string(content), "shop/1", "shop/9", 1)), 0o600)
	check(".env.local was rendered for slot 9")
	os.WriteFile(envPath, []byte("OTHER=1\n"), 0o600)
	check(".env.local has no port-keeper block")
	os.Remove(envPath)
	check(".env.local does not exist")
}
