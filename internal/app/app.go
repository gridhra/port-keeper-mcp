// Package app is the orchestration layer shared by the CLI and the MCP
// server: it resolves the current project and slot, keeps leases in sync
// with the manifest, and reconciles the ledger with what is listening.
package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gridhra/port-keeper-mcp/internal/config"
	"github.com/gridhra/port-keeper-mcp/internal/gitx"
	"github.com/gridhra/port-keeper-mcp/internal/ledger"
	"github.com/gridhra/port-keeper-mcp/internal/manifest"
	"github.com/gridhra/port-keeper-mcp/internal/probe"
	"github.com/gridhra/port-keeper-mcp/internal/render"
)

// Actor names who is calling, for the audit table.
type Actor string

// Actors.
const (
	ActorCLI Actor = "cli"
	ActorMCP Actor = "mcp"
)

// App bundles the configuration and the ledger.
type App struct {
	Cfg    *config.Config
	Ledger *ledger.Ledger
	Actor  Actor
	// Probe decides whether a port is taken by something outside the ledger.
	// Tests replace it; production uses probe.Listening.
	Probe func(port int) bool
	// Listeners returns who listens where (one lsof pass). Tests replace it;
	// production uses probe.ListenerMap.
	Listeners func(ctx context.Context) (map[int]probe.Owner, bool)
}

// New loads the config and opens the ledger.
func New(actor Actor) (*App, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	l, err := ledger.Open(config.StateDir())
	if err != nil {
		return nil, err
	}
	return &App{Cfg: cfg, Ledger: l, Actor: actor, Probe: probe.Listening, Listeners: probe.ListenerMap}, nil
}

// Close releases the ledger.
func (a *App) Close() error { return a.Ledger.Close() }

// Context is the project and slot resolved for a working directory.
type Context struct {
	Manifest *manifest.Manifest
	Project  *ledger.Project // nil until the first slot is created
	SlotName string
	Slot     *ledger.Slot // nil when the slot does not exist yet
	Root     string       // working copy root (or manifest dir) used to bind slots
	// How the slot name was chosen: env | root | default | explicit
	SlotSource string
	// BoundTo is set by SlotNew when the working copy stayed bound to another slot.
	BoundTo string
	// IgnoredEnvSlot is the value of PORT_KEEPER_SLOT when it was set but lost
	// to the working copy's binding (a stale export from an earlier hook).
	IgnoredEnvSlot string
	// UnboundRoot is set when this working copy has no slot of its own and the
	// default slot is bound to a different working copy. Commands that would hand
	// out that slot's ports refuse until the user creates a slot or passes
	// --slot explicitly (see RequireBound).
	UnboundRoot string
}

// ErrUnbound is returned by RequireBound.
var ErrUnbound = errors.New("working copy is not bound to a slot")

// Warnings returns human-readable notes about how the slot was resolved.
func (c *Context) Warnings() []string {
	var w []string
	if c.IgnoredEnvSlot != "" {
		w = append(w, fmt.Sprintf("PORT_KEEPER_SLOT=%s is set in this shell but this working copy is bound to slot %s; the binding wins. Unset the variable (or re-run your shell hook) to silence this", c.IgnoredEnvSlot, c.SlotName))
	}
	return w
}

// RequireBound refuses to act on a slot that belongs to another working copy.
func (c *Context) RequireBound() error {
	if c.UnboundRoot == "" {
		return nil
	}
	return fmt.Errorf("%w: slot %q belongs to %s, and this working copy (%s) has no slot yet. Run `port-keeper slot new` to lease its own ports, or pass `--slot %s` to share deliberately", ErrUnbound, c.SlotName, c.UnboundRoot, c.Root, c.SlotName)
}

// Resolve finds the manifest from cwd and decides the slot.
// Precedence: explicit argument, the slot bound to this working copy root,
// PORT_KEEPER_SLOT, then the manifest's slot_default. The environment
// variable ranks below the binding because port-keeper itself exports it
// (hooks, `env --format export`); a stale value in the shell must not hide
// the slot that `slot new` just bound to this working copy.
func (a *App) Resolve(ctx context.Context, cwd, explicitSlot string) (*Context, error) {
	mpath, err := manifest.Find(cwd)
	if err != nil {
		return nil, err
	}
	m, err := manifest.Load(mpath)
	if err != nil {
		return nil, err
	}
	c := &Context{Manifest: m}
	c.Root = gitx.Toplevel(m.Dir)
	if c.Root == "" {
		c.Root = m.Dir
	}
	if real, err := filepath.EvalSymlinks(c.Root); err == nil {
		c.Root = real
	}
	c.Project, err = a.Ledger.GetProject(ctx, m.Project.Name)
	if err != nil {
		return nil, err
	}
	var bound *ledger.Slot
	if c.Project != nil {
		bound, err = a.Ledger.SlotByRoot(ctx, c.Project.ID, c.Root)
		if err != nil {
			return nil, err
		}
	}
	switch {
	case explicitSlot != "":
		c.SlotName, c.SlotSource = explicitSlot, "explicit"
	case bound != nil:
		c.SlotName, c.SlotSource, c.Slot = bound.Name, "root", bound
		if v := os.Getenv("PORT_KEEPER_SLOT"); v != "" && v != bound.Name {
			c.IgnoredEnvSlot = v
		}
	case os.Getenv("PORT_KEEPER_SLOT") != "":
		c.SlotName, c.SlotSource = os.Getenv("PORT_KEEPER_SLOT"), "env"
	default:
		c.SlotName, c.SlotSource = m.Project.SlotDefault, "default"
	}
	if c.Slot == nil && c.Project != nil {
		c.Slot, err = a.Ledger.GetSlot(ctx, c.Project.ID, c.SlotName)
		if err != nil {
			return nil, err
		}
	}
	if c.SlotSource == "default" && c.Slot != nil && c.Slot.RootPath != "" && c.Slot.RootPath != c.Root {
		// Another working copy owns the default slot; do not hand its ports out here.
		c.UnboundRoot = c.Slot.RootPath
		c.Slot = nil
	}
	return c, nil
}

// ErrNoSlot is returned when an operation needs a slot that does not exist.
var ErrNoSlot = errors.New("slot does not exist")

func (a *App) slotMissing(ctx context.Context, c *Context) error {
	names, _ := a.slotNames(ctx, c)
	if len(names) == 0 {
		return fmt.Errorf("%w: project %q has no slots yet; run `port-keeper slot new` (creates slot %q)", ErrNoSlot, c.Manifest.Project.Name, c.SlotName)
	}
	return fmt.Errorf("%w: project %q has no slot %q (existing: %s); run `port-keeper slot new %s`", ErrNoSlot, c.Manifest.Project.Name, c.SlotName, strings.Join(names, ", "), c.SlotName)
}

func (a *App) slotNames(ctx context.Context, c *Context) ([]string, error) {
	if c.Project == nil {
		return nil, nil
	}
	slots, err := a.Ledger.ListSlots(ctx, c.Project.ID)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, s := range slots {
		names = append(names, s.Name)
	}
	return names, nil
}

// NewSlotOptions configures SlotNew.
type NewSlotOptions struct {
	Name      string // empty: the smallest unused positive integer
	InfraFrom string // empty: lease infra services too
	BindRoot  bool   // bind the current working copy root to the new slot
}

// SlotNew creates a slot (idempotent on name) and leases its ports.
// When BindRoot is set and the working copy root is already bound to a different
// slot, the binding is left alone and c.BoundTo names that slot.
func (a *App) SlotNew(ctx context.Context, c *Context, opt NewSlotOptions) (*render.Resolved, bool, error) {
	tx, err := a.Ledger.Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	p, err := tx.EnsureProject(ctx, c.Manifest.Project.Name)
	if err != nil {
		return nil, false, err
	}
	c.Project = p
	name := opt.Name
	if name == "" {
		if c.SlotSource == "root" && c.Slot != nil {
			// The working copy already has a slot: creating "the next one" on every
			// retry would burn blocks. Return the bound slot; a new one needs a name.
			name = c.Slot.Name
		} else {
			name, err = nextSlotName(ctx, tx, p.ID)
			if err != nil {
				return nil, false, err
			}
		}
	}
	if !validSlotName(name) {
		return nil, false, fmt.Errorf("slot name %q must be lowercase letters, digits and hyphens", name)
	}
	created := false
	s, err := tx.GetSlot(ctx, p.ID, name)
	if err != nil {
		return nil, false, err
	}
	if s == nil {
		var infra *int64
		if opt.InfraFrom != "" {
			if opt.InfraFrom == name {
				return nil, false, errors.New("--infra-from cannot point at the slot itself")
			}
			ref, err := tx.GetSlot(ctx, p.ID, opt.InfraFrom)
			if err != nil {
				return nil, false, err
			}
			if ref == nil {
				return nil, false, fmt.Errorf("--infra-from: slot %q does not exist", opt.InfraFrom)
			}
			if ref.InfraFrom != nil {
				return nil, false, fmt.Errorf("--infra-from: slot %q itself shares infra from another slot; point at the slot that owns it", opt.InfraFrom)
			}
			infra = &ref.ID
		}
		root := ""
		if opt.BindRoot {
			existing, err := tx.SlotByRoot(ctx, p.ID, c.Root)
			if err != nil {
				return nil, false, err
			}
			if existing == nil {
				root = c.Root
			} else {
				c.BoundTo = existing.Name
			}
		}
		s, err = tx.CreateSlot(ctx, p.ID, name, root, infra)
		if err != nil {
			return nil, false, err
		}
		if err := tx.Audit(ctx, string(a.Actor), "slot.new", p.Name+"/"+name); err != nil {
			return nil, false, err
		}
		created = true
	} else if opt.InfraFrom != "" {
		return nil, false, fmt.Errorf("slot %q already exists; --infra-from cannot be changed after creation (release it first)", name)
	}
	c.SlotName, c.Slot = name, s
	r, err := a.syncTx(ctx, tx, c, a.Probe)
	if err != nil {
		return nil, false, err
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return r, created, nil
}

func validSlotName(n string) bool {
	if n == "" || len(n) > 63 {
		return false
	}
	for _, r := range n {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

// SlotNameFromBranch turns a git branch name into a valid slot name: lower
// case, every run of characters outside [a-z0-9] becomes one hyphen, no
// leading or trailing hyphen, at most 63 bytes. "feature/Login_v2" becomes
// "feature-login-v2".
func SlotNameFromBranch(branch string) (string, error) {
	var b strings.Builder
	dash := true // suppress a leading hyphen
	for _, r := range strings.ToLower(branch) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	name := b.String()
	if len(name) > 63 {
		name = name[:63]
	}
	name = strings.TrimRight(name, "-")
	if !validSlotName(name) {
		return "", fmt.Errorf("branch %q leaves no characters usable in a slot name", branch)
	}
	return name, nil
}

func nextSlotName(ctx context.Context, tx *ledger.Tx, projectID int64) (string, error) {
	slots, err := tx.ListSlots(ctx, projectID)
	if err != nil {
		return "", err
	}
	used := map[int]bool{}
	for _, s := range slots {
		if n, err := strconv.Atoi(s.Name); err == nil {
			used[n] = true
		}
	}
	for n := 1; ; n++ {
		if !used[n] {
			return strconv.Itoa(n), nil
		}
	}
}

// Sync makes the slot's leases match the manifest and returns the resolved ports.
// Missing services get a port from the slot's blocks (allocating a new block if
// full); services no longer in the manifest are released.
func (a *App) Sync(ctx context.Context, c *Context) (*render.Resolved, error) {
	if c.Slot == nil {
		return nil, a.slotMissing(ctx, c)
	}
	tx, err := a.Ledger.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	r, err := a.syncTx(ctx, tx, c, a.Probe)
	if err != nil {
		return nil, err
	}
	return r, tx.Commit()
}

func (a *App) syncTx(ctx context.Context, tx *ledger.Tx, c *Context, probe ledger.Probe) (*render.Resolved, error) {
	m, s := c.Manifest, c.Slot
	leases, err := tx.Leases(ctx, s.ID)
	if err != nil {
		return nil, err
	}
	byService := map[string]ledger.Lease{}
	for _, l := range leases {
		byService[l.Service] = l
	}
	wanted := map[string]bool{}
	for _, svc := range m.Services {
		if svc.Tier == "infra" && s.InfraFrom != nil {
			continue // resolved from the referenced slot
		}
		wanted[svc.Name] = true
		if l, ok := byService[svc.Name]; ok {
			if l.Tier != svc.Tier || l.Proto != svc.Proto {
				if err := tx.UpdateLeaseMeta(ctx, l.ID, svc.Tier, svc.Proto); err != nil {
					return nil, err
				}
			}
			continue
		}
		port, err := a.nextFreePort(ctx, tx, s.ID, m.Project.BlockSize, probe)
		if err != nil {
			return nil, err
		}
		if err := tx.InsertLease(ctx, s.ID, svc.Name, port, svc.Tier, svc.Proto); err != nil {
			return nil, err
		}
		if err := tx.Audit(ctx, string(a.Actor), "lease.new", m.Project.Name+"/"+s.Name+"/"+svc.Name); err != nil {
			return nil, err
		}
	}
	for _, l := range leases {
		if !wanted[l.Service] {
			if l.Pinned() {
				return nil, fmt.Errorf("service %q is pinned to %d (%s) but is no longer in the manifest; run `port-keeper unpin %s` (or put the service back) before syncing", l.Service, l.Port, l.PinReason, l.Service)
			}
			if err := tx.DeleteLease(ctx, l.ID); err != nil {
				return nil, err
			}
			if err := tx.Audit(ctx, string(a.Actor), "lease.drop", m.Project.Name+"/"+s.Name+"/"+l.Service); err != nil {
				return nil, err
			}
		}
	}
	return a.resolveTx(ctx, tx, c)
}

// nextFreePort picks the lowest usable offset in the slot's blocks, or leases a
// new block. An offset is skipped when it is denied, no longer inside the pool
// (the pool was narrowed after the block was leased), leased elsewhere (a pin
// from another project landed inside this block), or taken by an untracked
// listener.
func (a *App) nextFreePort(ctx context.Context, tx *ledger.Tx, slotID int64, blockSize int, probe ledger.Probe) (int, error) {
	free, err := tx.FreeOffsets(ctx, slotID)
	if err != nil {
		return 0, err
	}
	leased, err := tx.AllLeasedPorts(ctx)
	if err != nil {
		return 0, err
	}
	for _, p := range free {
		if a.Cfg.Pool.Denied(p) || !a.Cfg.Pool.Contains(p) || leased[p] {
			continue
		}
		if probe != nil && probe(p) {
			continue // taken by something outside the ledger; leave it alone
		}
		return p, nil
	}
	b, err := tx.AllocateBlock(ctx, slotID, blockSize, a.Cfg.Pool, probe)
	if err != nil {
		return 0, err
	}
	return b.Base, nil
}

// leaseSource reads leases through either the ledger or an open transaction.
// Reads that happen inside a transaction must go through it: the ledger keeps a
// single connection, and a fresh connection would not see uncommitted rows.
type leaseSource interface {
	Leases(ctx context.Context, slotID int64) ([]ledger.Lease, error)
	Blocks(ctx context.Context, slotID int64) ([]ledger.Block, error)
	SlotByID(ctx context.Context, id int64) (*ledger.Slot, error)
}

func (a *App) resolveTx(ctx context.Context, tx *ledger.Tx, c *Context) (*render.Resolved, error) {
	return a.resolveWith(ctx, tx, c)
}

// Resolved returns the ports of the slot without changing anything.
func (a *App) Resolved(ctx context.Context, c *Context) (*render.Resolved, error) {
	if c.Slot == nil {
		return nil, a.slotMissing(ctx, c)
	}
	return a.resolveWith(ctx, a.Ledger, c)
}

func (a *App) resolveWith(ctx context.Context, src leaseSource, c *Context) (*render.Resolved, error) {
	m, s := c.Manifest, c.Slot
	r := &render.Resolved{Project: m.Project.Name, Slot: s.Name, InfraSlot: s.Name, Host: m.Render.Host, Ports: map[string]int{}}
	leases, err := src.Leases(ctx, s.ID)
	if err != nil {
		return nil, err
	}
	for _, l := range leases {
		r.Ports[l.Service] = l.Port
	}
	blocks, err := src.Blocks(ctx, s.ID)
	if err != nil {
		return nil, err
	}
	if len(blocks) > 0 {
		r.BlockBase = blocks[0].Base
	}
	if s.InfraFrom != nil {
		ref, err := src.SlotByID(ctx, *s.InfraFrom)
		if err != nil {
			return nil, err
		}
		if ref == nil {
			return nil, fmt.Errorf("slot %q shares infra from a slot that no longer exists", s.Name)
		}
		r.InfraSlot = ref.Name
		refLeases, err := src.Leases(ctx, ref.ID)
		if err != nil {
			return nil, err
		}
		refPorts := map[string]int{}
		for _, l := range refLeases {
			refPorts[l.Service] = l.Port
		}
		for _, svc := range m.Services {
			if svc.Tier != "infra" {
				continue
			}
			p, ok := refPorts[svc.Name]
			if !ok {
				return nil, fmt.Errorf("infra service %q is not leased in slot %q yet; run `port-keeper env` in that slot first", svc.Name, ref.Name)
			}
			r.Ports[svc.Name] = p
		}
	}
	for _, svc := range m.Services {
		if _, ok := r.Ports[svc.Name]; !ok {
			return nil, fmt.Errorf("service %q has no lease; run `port-keeper env` to sync", svc.Name)
		}
	}
	return r, nil
}

// ServiceURL returns the URL (or "port N" for tcp) of one service in a resolved slot.
func ServiceURL(m *manifest.Manifest, r *render.Resolved, service string) (url string, port int, err error) {
	svc, ok := m.Service(service)
	if !ok {
		return "", 0, fmt.Errorf("unknown service %q (services: %s)", service, strings.Join(m.ServiceNames(), ", "))
	}
	p, ok := r.Ports[service]
	if !ok {
		return "", 0, fmt.Errorf("service %q has no lease; run `port-keeper env`", service)
	}
	u, _ := render.URL(svc.Proto, r.Host, p)
	return u, p, nil
}

// LookupProject resolves another project by name from the ledger and its manifest
// from the slot's bound root. Used for cross-project URL lookups.
func (a *App) LookupProject(ctx context.Context, project, slot string) (*Context, error) {
	p, err := a.Ledger.GetProject(ctx, project)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, fmt.Errorf("unknown project %q", project)
	}
	slots, err := a.Ledger.ListSlots(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	var chosen *ledger.Slot
	var names []string
	var anyRoot string
	for i := range slots {
		names = append(names, slots[i].Name)
		if slots[i].RootPath != "" && anyRoot == "" {
			anyRoot = slots[i].RootPath
		}
		if slots[i].Name == slot {
			chosen = &slots[i]
		}
	}
	if chosen == nil {
		return nil, fmt.Errorf("project %q has no slot %q (existing: %s)", project, slot, strings.Join(names, ", "))
	}
	root := chosen.RootPath
	if root == "" {
		root = anyRoot
	}
	if root == "" {
		return nil, fmt.Errorf("project %q has no slot bound to a directory, so its manifest cannot be found; run `port-keeper env` inside that repository once", project)
	}
	mpath, err := manifest.Find(root)
	if err != nil {
		return nil, err
	}
	m, err := manifest.Load(mpath)
	if err != nil {
		return nil, err
	}
	return &Context{Manifest: m, Project: p, SlotName: slot, Slot: chosen, Root: root, SlotSource: "explicit"}, nil
}

// --- status ---

// State of a lease as seen by status.
type State string

// States.
const (
	StateLeased   State = "leased"   // nothing listening
	StateActive   State = "active"   // listening
	StateStale    State = "stale"    // not listening for longer than stale_days
	StateHijacked State = "hijacked" // listening process's cwd is outside this working copy
)

// ServiceStatus is one row of `port-keeper status`.
type ServiceStatus struct {
	Service  string
	Port     int
	Proto    string
	Tier     string
	Shared   bool // resolved from another slot
	Pinned   bool
	State    State
	Owner    probe.Owner // the listening process; Command is capped (see ListenerLabel)
	LastSeen *time.Time
}

// MaxListenerLabel caps the process-controlled command name before it reaches an agent.
const MaxListenerLabel = 64

// ListenerLabel formats the owner for display with the command name capped.
func (s ServiceStatus) ListenerLabel() string {
	if s.Owner.PID == 0 {
		return ""
	}
	cmd := []rune(s.Owner.Command)
	if len(cmd) > MaxListenerLabel {
		cmd = cmd[:MaxListenerLabel]
	}
	return probe.Owner{PID: s.Owner.PID, Command: string(cmd)}.String()
}

// Status reconciles the ledger with what is listening. When touch is set,
// active leases get their last_seen_at updated (the CLI does this; the MCP
// status tool is read-only and does not).
func (a *App) Status(ctx context.Context, c *Context, touch bool) ([]ServiceStatus, error) {
	if c.Slot == nil {
		return nil, a.slotMissing(ctx, c)
	}
	r, err := a.Resolved(ctx, c)
	if err != nil {
		return nil, err
	}
	leases, err := a.Ledger.Leases(ctx, c.Slot.ID)
	if err != nil {
		return nil, err
	}
	byService := map[string]ledger.Lease{}
	for _, l := range leases {
		byService[l.Service] = l
	}
	// One lsof pass for every port instead of two per active service.
	var listeners map[int]probe.Owner
	haveListeners := false
	if a.Listeners != nil {
		listeners, haveListeners = a.Listeners(ctx)
	}
	var out []ServiceStatus
	for _, svc := range c.Manifest.Services {
		st := ServiceStatus{Service: svc.Name, Port: r.Ports[svc.Name], Proto: svc.Proto, Tier: svc.Tier}
		l, own := byService[svc.Name]
		st.Shared = !own
		if own {
			st.Pinned = l.Pinned()
			st.LastSeen = l.LastSeenAt
		}
		listening := a.Probe(st.Port)
		switch {
		case listening:
			st.State = StateActive
			if own && touch {
				_ = a.Ledger.Touch(ctx, l.ID)
				now := time.Now()
				st.LastSeen = &now
			}
			if haveListeners {
				st.Owner = listeners[st.Port]
			}
			if own && st.Owner.Cwd != "" && c.Root != "" && !within(st.Owner.Cwd, c.Root) && !looksLikeContainerRuntime(st.Owner.Command) {
				st.State = StateHijacked
			}
		case own && isStale(l, a.Cfg.StaleDays):
			st.State = StateStale
		default:
			st.State = StateLeased
		}
		out = append(out, st)
	}
	return out, nil
}

func within(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || !strings.HasPrefix(rel, "..")
}

func looksLikeContainerRuntime(cmd string) bool {
	c := strings.ToLower(cmd)
	return strings.Contains(c, "docker") || strings.Contains(c, "podman") || strings.Contains(c, "containerd") || strings.Contains(c, "colima") || strings.Contains(c, "limactl") || strings.Contains(c, "orb")
}

func isStale(l ledger.Lease, days int) bool {
	if days <= 0 {
		return false
	}
	if l.LastSeenAt == nil {
		return false
	}
	return time.Since(*l.LastSeenAt) > time.Duration(days)*24*time.Hour
}

// --- release / gc ---

// ReleaseSlot removes a slot. It refuses while a service is listening unless force,
// and refuses while other slots share its infra unless cascade.
func (a *App) ReleaseSlot(ctx context.Context, c *Context, name string, force, cascade bool) ([]string, error) {
	if c.Project == nil {
		return nil, fmt.Errorf("project %q has no slots", c.Manifest.Project.Name)
	}
	s, err := a.Ledger.GetSlot(ctx, c.Project.ID, name)
	if err != nil {
		return nil, err
	}
	if s == nil {
		names, _ := a.slotNames(ctx, c)
		return nil, fmt.Errorf("no slot %q (existing: %s)", name, strings.Join(names, ", "))
	}
	deps, err := a.Ledger.Dependents(ctx, s.ID)
	if err != nil {
		return nil, err
	}
	if len(deps) > 0 && !cascade {
		return nil, fmt.Errorf("slot %q is the infra owner of %s; release those first or pass --cascade", name, strings.Join(deps, ", "))
	}
	var released []string
	tx, err := a.Ledger.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, depName := range deps {
		dep, err := tx.GetSlot(ctx, c.Project.ID, depName)
		if err != nil {
			return nil, err
		}
		if err := a.releaseOne(ctx, tx, c, dep, force); err != nil {
			return nil, err
		}
		released = append(released, depName)
	}
	if err := a.releaseOne(ctx, tx, c, s, force); err != nil {
		return nil, err
	}
	released = append(released, name)
	return released, tx.Commit()
}

func (a *App) releaseOne(ctx context.Context, tx *ledger.Tx, c *Context, s *ledger.Slot, force bool) error {
	leases, err := tx.Leases(ctx, s.ID)
	if err != nil {
		return err
	}
	if !force {
		for _, l := range leases {
			if a.Probe(l.Port) {
				return fmt.Errorf("slot %q: service %q is still listening; stop it first or pass --force", s.Name, l.Service)
			}
		}
	}
	if err := tx.DeleteSlot(ctx, s.ID); err != nil {
		return err
	}
	return tx.Audit(ctx, string(a.Actor), "slot.release", c.Manifest.Project.Name+"/"+s.Name)
}

// StaleSlot is a gc candidate.
type StaleSlot struct {
	Project  string
	Slot     string
	SlotID   int64
	LastSeen *time.Time // nil: never seen listening
	Created  time.Time
	// Orphan is set when the bound working copy no longer exists (or has no
	// manifest), so the slot can only be released through the ledger.
	Orphan bool
}

// StaleSlots lists slots where no lease is listening now and none has been seen
// within stale_days (a never-seen slot counts once it is older than stale_days),
// plus slots whose bound working copy no longer exists, whatever their age.
func (a *App) StaleSlots(ctx context.Context) ([]StaleSlot, error) {
	projects, err := a.Ledger.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	cutoff := time.Now().Add(-time.Duration(a.Cfg.StaleDays) * 24 * time.Hour)
	var out []StaleSlot
	for _, p := range projects {
		pr, err := a.Ledger.GetProject(ctx, p.Name)
		if err != nil || pr == nil {
			continue
		}
		slots, err := a.Ledger.ListSlots(ctx, pr.ID)
		if err != nil {
			return nil, err
		}
		for _, s := range slots {
			leases, err := a.Ledger.Leases(ctx, s.ID)
			if err != nil {
				return nil, err
			}
			var latest *time.Time
			listening := false
			for _, l := range leases {
				if a.Probe(l.Port) {
					listening = true
					break
				}
				if l.LastSeenAt != nil && (latest == nil || l.LastSeenAt.After(*latest)) {
					latest = l.LastSeenAt
				}
			}
			if listening {
				continue
			}
			ref := s.CreatedAt
			if latest != nil {
				ref = *latest
			}
			orphan := false
			if s.RootPath != "" {
				if _, err := manifest.Find(s.RootPath); err != nil {
					orphan = true // the bound working copy is gone: a candidate regardless of age
				}
			}
			if orphan || ref.Before(cutoff) {
				out = append(out, StaleSlot{Project: p.Name, Slot: s.Name, SlotID: s.ID, LastSeen: latest, Created: s.CreatedAt, Orphan: orphan})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Project+out[i].Slot < out[j].Project+out[j].Slot })
	return out, nil
}

// ReleaseOrphan releases a slot straight from the ledger, for projects whose
// repository is gone. It refuses while something listens on a leased port and
// while other slots share the slot's infra.
func (a *App) ReleaseOrphan(ctx context.Context, project string, slotID int64) error {
	deps, err := a.Ledger.Dependents(ctx, slotID)
	if err != nil {
		return err
	}
	if len(deps) > 0 {
		return fmt.Errorf("slot is the infra owner of %s; release those first", strings.Join(deps, ", "))
	}
	tx, err := a.Ledger.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	s, err := tx.SlotByID(ctx, slotID)
	if err != nil {
		return err
	}
	if s == nil {
		return errors.New("slot no longer exists")
	}
	leases, err := tx.Leases(ctx, slotID)
	if err != nil {
		return err
	}
	for _, l := range leases {
		if a.Probe(l.Port) {
			return fmt.Errorf("service %q is still listening", l.Service)
		}
	}
	if err := tx.DeleteSlot(ctx, slotID); err != nil {
		return err
	}
	if err := tx.Audit(ctx, string(a.Actor), "slot.release", project+"/"+s.Name); err != nil {
		return err
	}
	return tx.Commit()
}

// --- pin ---

// Pin registers a fixed port for a service of the project's default slot.
func (a *App) Pin(ctx context.Context, c *Context, service string, port int, reason string, force bool) error {
	if strings.TrimSpace(reason) == "" {
		return errors.New("--reason is required: pins are an exception for migrating existing projects, and the reason is shown in `slot ls` until you unpin")
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("port %d out of range", port)
	}
	svc, ok := c.Manifest.Service(service)
	if !ok {
		return fmt.Errorf("unknown service %q (services: %s)", service, strings.Join(c.Manifest.ServiceNames(), ", "))
	}
	if c.SlotName != c.Manifest.Project.SlotDefault {
		return fmt.Errorf("pins are only allowed in the default slot %q (current: %q); every other slot uses the pool", c.Manifest.Project.SlotDefault, c.SlotName)
	}
	if c.Slot == nil {
		return a.slotMissing(ctx, c)
	}
	if svc.Tier == "infra" && c.Slot.InfraFrom != nil {
		return fmt.Errorf("service %q is shared from another slot; pin it there", service)
	}
	tx, err := a.Ledger.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if owner, err := tx.OwnerOfPort(ctx, port); err != nil {
		return err
	} else if owner != nil && !(owner.Project == c.Manifest.Project.Name && owner.Slot == c.SlotName && owner.Service == service) {
		kind := "leased"
		if owner.Pinned {
			kind = "pinned"
		}
		return fmt.Errorf("port %d is already %s by %s/%s/%s; the ledger arbitrates pins across every project, so pick another port or release that lease", port, kind, owner.Project, owner.Slot, owner.Service)
	}
	if bo, err := tx.BlockOwnerOfPort(ctx, port); err != nil {
		return err
	} else if bo != nil && !(bo.Project == c.Manifest.Project.Name && bo.Slot == c.SlotName) && !force {
		return fmt.Errorf("port %d lies inside the pooled block of %s/%s; pinning it would starve that slot. Pick a port outside the pool (or pass --force if you accept that)", port, bo.Project, bo.Slot)
	}
	pinnedSlots, err := tx.PinnedSlotsOfProject(ctx, c.Project.ID)
	if err != nil {
		return err
	}
	for _, s := range pinnedSlots {
		if s != c.SlotName {
			return fmt.Errorf("project %q already has pins in slot %q; a project may pin in one slot only", c.Manifest.Project.Name, s)
		}
	}
	if a.Probe(port) && !force {
		return fmt.Errorf("port %d is currently taken by %s (not in the ledger); stop it or pass --force to pin anyway", port, probe.LookupOwner(ctx, port))
	}
	leases, err := tx.Leases(ctx, c.Slot.ID)
	if err != nil {
		return err
	}
	for _, l := range leases {
		if l.Service == service {
			if err := tx.DeleteLease(ctx, l.ID); err != nil {
				return err
			}
		}
	}
	if err := tx.InsertPinnedLease(ctx, c.Slot.ID, service, port, svc.Tier, svc.Proto, strings.TrimSpace(reason)); err != nil {
		return err
	}
	if err := tx.Audit(ctx, string(a.Actor), "lease.pin", c.Manifest.Project.Name+"/"+c.SlotName+"/"+service); err != nil {
		return err
	}
	return tx.Commit()
}

// Unpin drops the pinned lease and immediately leases a pooled port instead.
func (a *App) Unpin(ctx context.Context, c *Context, service string) (*render.Resolved, error) {
	if c.Slot == nil {
		return nil, a.slotMissing(ctx, c)
	}
	tx, err := a.Ledger.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	leases, err := tx.Leases(ctx, c.Slot.ID)
	if err != nil {
		return nil, err
	}
	found := false
	for _, l := range leases {
		if l.Service == service {
			if !l.Pinned() {
				return nil, fmt.Errorf("service %q is not pinned", service)
			}
			if err := tx.DeleteLease(ctx, l.ID); err != nil {
				return nil, err
			}
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("service %q has no lease in slot %q", service, c.SlotName)
	}
	if err := tx.Audit(ctx, string(a.Actor), "lease.unpin", c.Manifest.Project.Name+"/"+c.SlotName+"/"+service); err != nil {
		return nil, err
	}
	r, err := a.syncTx(ctx, tx, c, a.Probe)
	if err != nil {
		return nil, err
	}
	return r, tx.Commit()
}

// Reassign drops a service's pooled lease and leases a different port for it,
// skipping ports that something outside the ledger is listening on.
func (a *App) Reassign(ctx context.Context, c *Context, service string) (old, new int, err error) {
	if c.Slot == nil {
		return 0, 0, a.slotMissing(ctx, c)
	}
	if _, ok := c.Manifest.Service(service); !ok {
		return 0, 0, fmt.Errorf("unknown service %q (services: %s)", service, strings.Join(c.Manifest.ServiceNames(), ", "))
	}
	tx, err := a.Ledger.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	leases, err := tx.Leases(ctx, c.Slot.ID)
	if err != nil {
		return 0, 0, err
	}
	var cur *ledger.Lease
	for i := range leases {
		if leases[i].Service == service {
			cur = &leases[i]
		}
	}
	if cur == nil {
		return 0, 0, fmt.Errorf("service %q is not leased in slot %q (shared infra is reassigned in its owning slot)", service, c.SlotName)
	}
	if cur.Pinned() {
		return 0, 0, fmt.Errorf("service %q is pinned to %d; unpin it instead", service, cur.Port)
	}
	old = cur.Port
	if err := tx.DeleteLease(ctx, cur.ID); err != nil {
		return 0, 0, err
	}
	// Hold the old port out of the candidate set even if nothing listens right now.
	base := a.Probe
	avoidOld := func(p int) bool { return p == old || (base != nil && base(p)) }
	r, err := a.syncTx(ctx, tx, c, avoidOld)
	if err != nil {
		return 0, 0, err
	}
	if err := tx.Audit(ctx, string(a.Actor), "lease.reassign", c.Manifest.Project.Name+"/"+c.SlotName+"/"+service); err != nil {
		return 0, 0, err
	}
	return old, r.Ports[service], tx.Commit()
}

// --- rendering helpers shared by CLI and MCP ---

// EnvText renders the current slot in a format; for "dotenv" it returns the block body.
func (a *App) EnvText(ctx context.Context, c *Context, format string) (string, *render.Resolved, error) {
	r, err := a.Sync(ctx, c)
	if err != nil {
		return "", nil, err
	}
	vars, err := render.Vars(c.Manifest, r)
	if err != nil {
		return "", nil, err
	}
	text, err := render.Format(format, c.Manifest, r, vars)
	if err != nil {
		return "", nil, err
	}
	return text, r, nil
}

// WriteDotenv writes the marker block, refusing git-tracked targets.
func (a *App) WriteDotenv(ctx context.Context, c *Context, body string) (path string, changed bool, err error) {
	m := c.Manifest
	path = m.DotenvAbs()
	rel, _ := filepath.Rel(m.Dir, path)
	if strings.HasPrefix(rel, "..") {
		return path, false, fmt.Errorf("render.dotenv_path %q escapes the manifest directory", m.Render.DotenvPath)
	}
	if st, err := os.Lstat(path); err == nil && st.Mode()&os.ModeSymlink != 0 {
		return path, false, fmt.Errorf("%s is a symbolic link; port-keeper does not write through symlinks", path)
	}
	// A directory on the way may be a symlink out of the repository.
	realDir, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return path, false, fmt.Errorf("%s: %w", filepath.Dir(path), err)
	}
	realRoot, err := filepath.EvalSymlinks(m.Dir)
	if err != nil {
		return path, false, err
	}
	if rel, err := filepath.Rel(realRoot, realDir); err != nil || strings.HasPrefix(rel, "..") {
		return path, false, fmt.Errorf("render.dotenv_path %q resolves outside the manifest directory (%s)", m.Render.DotenvPath, realDir)
	}
	if gitx.InRepo(m.Dir) && gitx.Tracked(m.Dir, m.Render.DotenvPath) {
		return path, false, fmt.Errorf("%s is tracked by git; port numbers must not be committed. Add it to .gitignore and `git rm --cached` it", m.Render.DotenvPath)
	}
	changed, err = render.WriteDotenv(path, m.Render.DotenvMarker, m.Project.Name, c.SlotName, body)
	return path, changed, err
}

// EnvDrift reports, in words and without port numbers, why the rendered
// dotenv file no longer matches the manifest and the ledger; it returns "" when
// the file is current. It reads only: the block is rendered in memory from the
// leases and compared with what is on disk, so a service added to the manifest,
// a `reassign`, a hand edit and a deleted file are all caught without keeping a
// manifest hash in the ledger.
func (a *App) EnvDrift(ctx context.Context, c *Context) (reason string, err error) {
	m := c.Manifest
	if c.Slot == nil {
		return fmt.Sprintf("slot %s has no ports yet", c.SlotName), nil
	}
	// Resolved refuses a slot with an unleased service; name those first.
	leases, err := a.Ledger.Leases(ctx, c.Slot.ID)
	if err != nil {
		return "", err
	}
	leased := map[string]bool{}
	for _, l := range leases {
		leased[l.Service] = true
	}
	var missing []string
	for _, s := range m.Services {
		if !leased[s.Name] && !(s.Tier == "infra" && c.Slot.InfraFrom != nil) {
			missing = append(missing, s.Name)
		}
	}
	switch len(missing) {
	case 0:
	case 1:
		return fmt.Sprintf("service %s has no port yet", missing[0]), nil
	default:
		return fmt.Sprintf("services %s have no ports yet", strings.Join(missing, ", ")), nil
	}
	r, err := a.Resolved(ctx, c)
	if err != nil {
		return "", err
	}
	rel := m.Render.DotenvPath
	content, err := os.ReadFile(m.DotenvAbs())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return rel + " does not exist", nil
		}
		return "", err
	}
	header, body, found := render.DotenvBlock(string(content), m.Render.DotenvMarker)
	if !found {
		return fmt.Sprintf("%s has no %s block", rel, m.Render.DotenvMarker), nil
	}
	if header != m.Project.Name+"/"+c.SlotName {
		if _, slot, ok := strings.Cut(header, "/"); ok {
			return fmt.Sprintf("%s was rendered for slot %s", rel, slot), nil
		}
		return fmt.Sprintf("%s was rendered for another slot", rel), nil
	}
	vars, err := render.Vars(m, r)
	if err != nil {
		return "", err
	}
	want, err := render.Format("dotenv", m, r, vars)
	if err != nil {
		return "", err
	}
	if body != want {
		return rel + " is out of date", nil
	}
	return "", nil
}
