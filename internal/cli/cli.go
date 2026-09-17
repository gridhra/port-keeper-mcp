// Package cli implements the port-keeper command line.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/gridhra/port-keeper-mcp/internal/app"
	"github.com/gridhra/port-keeper-mcp/internal/config"
	"github.com/gridhra/port-keeper-mcp/internal/gitx"
	"github.com/gridhra/port-keeper-mcp/internal/manifest"
	"github.com/gridhra/port-keeper-mcp/internal/mcpserver"
	"github.com/gridhra/port-keeper-mcp/internal/probe"
)

// Version is set by the build (ldflags) and reported by `port-keeper --version`.
var Version = "dev"

const usage = `port-keeper — local ledger for development ports

Usage:
  port-keeper init [--name <project>] [--here]  write port-keeper.toml and the .gitignore entry
  port-keeper slot new [<name>] [--infra-from <slot>] [--no-bind]
  port-keeper slot ls [--pins]
  port-keeper slot rm <name> [--force] [--cascade]
  port-keeper env [--format dotenv|export|json|mise|direnv|claude-env] [--stdout] [--if-present]
  port-keeper url <service> | <project>/<slot>/<service> [--open]
  port-keeper status
  port-keeper gc [--yes]
  port-keeper doctor [--fix]
  port-keeper pin <service> <port> --reason <text> [--force]
  port-keeper pin <service>=<port> [...] --reason <text> [--force]
  port-keeper unpin <service> [...] | --all
  port-keeper reassign <service>                move a service to a different pooled port
  port-keeper mcp                               run the stdio MCP server
  port-keeper context [--json]                  where am I: project, slot, readiness and what to do next (no numbers)
  port-keeper hook claude                       Claude Code adapter for SessionStart / CwdChanged (uses context + env)
  port-keeper version

Global flags:
  --slot <name>   act on this slot instead of the resolved one (also PORT_KEEPER_SLOT)

Slots are resolved from --slot, then the slot bound to this git working copy, then
PORT_KEEPER_SLOT, then the manifest's slot_default. A working copy without a slot
of its own is refused the default slot's ports until you run 'slot new'. Ports never appear in the manifest
or in git; they live in ` + "`~/.local/state/port-keeper/ledger.sqlite`" + ` (0600).
`

// Main runs the CLI and returns the exit code.
func Main(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	// Peel the global --slot flag wherever it appears.
	var slot string
	var rest []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--" {
			rest = append(rest, args[i:]...)
			break
		}
		switch {
		case args[i] == "--slot" && i+1 < len(args):
			slot = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--slot="):
			slot = strings.TrimPrefix(args[i], "--slot=")
		default:
			rest = append(rest, args[i])
		}
	}
	if len(rest) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	cmd, cargs := rest[0], rest[1:]
	switch cmd {
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usage)
		return 0
	case "version", "--version", "-V":
		fmt.Fprintf(stdout, "port-keeper %s (%s/%s)\n", Version, runtime.GOOS, runtime.GOARCH)
		return 0
	}
	if err := run(context.Background(), cmd, cargs, slot, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "port-keeper: %v\n", err)
		return 1
	}
	return 0
}

func run(ctx context.Context, cmd string, args []string, slot string, stdout, stderr io.Writer) error {
	if cmd == "init" {
		return cmdInit(args, stdout)
	}
	a, err := app.New(app.ActorCLI)
	if err != nil {
		return err
	}
	defer a.Close()
	cwd, _ := os.Getwd()
	switch cmd {
	case "slot":
		return cmdSlot(ctx, a, cwd, slot, args, stdout)
	case "env":
		return cmdEnv(ctx, a, cwd, slot, args, stdout, stderr)
	case "url":
		return cmdURL(ctx, a, cwd, slot, args, stdout)
	case "status":
		return cmdStatus(ctx, a, cwd, slot, args, stdout)
	case "gc":
		return cmdGC(ctx, a, args, stdout)
	case "doctor":
		return cmdDoctor(ctx, a, cwd, args, stdout)
	case "pin":
		return cmdPin(ctx, a, cwd, slot, args, stdout)
	case "unpin":
		return cmdUnpin(ctx, a, cwd, slot, args, stdout)
	case "reassign":
		return cmdReassign(ctx, a, cwd, slot, args, stdout)
	case "mcp":
		a.Actor = app.ActorMCP
		return mcpserver.RunStdio(ctx, a, cwd, slot, Version)
	case "context":
		return cmdContext(ctx, a, cwd, slot, args, stdout)
	case "hook":
		return cmdHook(ctx, a, cwd, slot, args, os.Stdin, stdout)
	default:
		return fmt.Errorf("unknown command %q\n%s", cmd, usage)
	}
}

// parseMixed parses flags that may appear before or after positional arguments
// (the standard flag package stops at the first positional).
func parseMixed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for i, a := range args {
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			args = args[:i]
			break
		}
	}
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return pos, nil
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
}

// noPositionals rejects stray arguments on commands that take none.
func noPositionals(cmd string, fs *flag.FlagSet, args []string) error {
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return fmt.Errorf("%s takes no arguments (got %q); flags start with --", cmd, pos[0])
	}
	return nil
}

func warn(stderr io.Writer, c *app.Context) {
	for _, w := range c.Warnings() {
		fmt.Fprintf(stderr, "warning: %s\n", w)
	}
}

func first(pos []string) string {
	if len(pos) == 0 {
		return ""
	}
	return pos[0]
}

// --- init ---

func cmdInit(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	name := fs.String("name", "", "project name (default: directory name)")
	here := fs.Bool("here", false, "write the manifest in the current directory instead of the git top level (monorepos)")
	if err := noPositionals("init", fs, args); err != nil {
		return err
	}
	cwd, _ := os.Getwd()
	root := gitx.Toplevel(cwd)
	if root == "" || *here {
		root = cwd
	}
	path := filepath.Join(root, manifest.FileName)
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists", path)
	}
	if *name == "" {
		*name = strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
				return r
			}
			return '-'
		}, strings.ToLower(filepath.Base(root)))
	}
	if _, err := manifest.Parse(manifest.Skeleton(*name)); err != nil {
		return fmt.Errorf("project name %q is not valid: %w (pass --name)", *name, err)
	}
	if err := os.WriteFile(path, []byte(manifest.Skeleton(*name)), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "wrote %s\n", path)
	gi := filepath.Join(root, ".gitignore")
	b, _ := os.ReadFile(gi)
	alreadyIgnored := gitx.InRepo(root) && gitx.Ignored(root, ".env.local")
	if !alreadyIgnored && !strings.Contains(string(b), ".env.local") {
		content := string(b)
		if content != "" && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		content += "# port-keeper renders port numbers here; never commit them\n.env.local\n"
		if err := os.WriteFile(gi, []byte(content), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "added .env.local to %s\n", gi)
	}
	fmt.Fprintln(stdout, "next: edit the [[service]] entries, then `port-keeper slot new` and `port-keeper env`")
	return nil
}

// --- slot ---

func cmdSlot(ctx context.Context, a *app.App, cwd, slot string, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: port-keeper slot new|ls|rm")
	}
	switch args[0] {
	case "new":
		return cmdSlotNew(ctx, a, cwd, slot, args[1:], stdout)
	case "ls":
		return cmdSlotLs(ctx, a, cwd, slot, args[1:], stdout)
	case "rm":
		return cmdSlotRm(ctx, a, cwd, slot, args[1:], stdout)
	default:
		return fmt.Errorf("unknown slot subcommand %q (new|ls|rm)", args[0])
	}
}

func cmdSlotNew(ctx context.Context, a *app.App, cwd, slot string, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("slot new", flag.ContinueOnError)
	infra := fs.String("infra-from", "", "share tier=infra services from this slot")
	noBind := fs.Bool("no-bind", false, "do not bind this working copy to the new slot")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	c, err := a.Resolve(ctx, cwd, slot)
	if err != nil {
		return err
	}
	name := first(pos)
	if name == "" && slot != "" {
		name = slot
	}
	r, created, err := a.SlotNew(ctx, c, app.NewSlotOptions{Name: name, InfraFrom: *infra, BindRoot: !*noBind})
	if err != nil {
		return err
	}
	verb := "exists"
	if created {
		verb = "created"
	}
	fmt.Fprintf(stdout, "slot %s/%s %s (block base %d, %d services)\n", r.Project, r.Slot, verb, r.BlockBase, len(r.Ports))
	if r.InfraSlot != r.Slot {
		fmt.Fprintf(stdout, "infra services resolve from slot %s\n", r.InfraSlot)
	}
	if c.BoundTo != "" {
		fmt.Fprintf(stdout, "this working copy stays bound to slot %s; use `--slot %s` here, or run `port-keeper env` from the working copy that should own slot %s\n", c.BoundTo, r.Slot, r.Slot)
		fmt.Fprintf(stdout, "next: port-keeper --slot %s env\n", r.Slot)
		return nil
	}
	fmt.Fprintln(stdout, "next: port-keeper env")
	return nil
}

func cmdSlotLs(ctx context.Context, a *app.App, cwd, slot string, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("slot ls", flag.ContinueOnError)
	showPins := fs.Bool("pins", false, "list pinned ports with their reasons below the table")
	if err := noPositionals("slot ls", fs, args); err != nil {
		return err
	}
	c, err := a.Resolve(ctx, cwd, slot)
	if err != nil {
		return err
	}
	if c.Project == nil {
		fmt.Fprintf(stdout, "project %s has no slots yet (port-keeper slot new)\n", c.Manifest.Project.Name)
		return nil
	}
	slots, err := a.Ledger.ListSlots(ctx, c.Project.ID)
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SLOT\tBLOCKS\tINFRA FROM\tPINNED\tBOUND TO")
	var pinLines []string
	for _, s := range slots {
		blocks, _ := a.Ledger.Blocks(ctx, s.ID)
		var bs []string
		for _, b := range blocks {
			bs = append(bs, fmt.Sprintf("%d-%d", b.Base, b.Base+b.Size-1))
		}
		infra := "-"
		if s.InfraFrom != nil {
			if ref, _ := a.Ledger.SlotByID(ctx, *s.InfraFrom); ref != nil {
				infra = ref.Name
			}
		}
		leases, _ := a.Ledger.Leases(ctx, s.ID)
		pins := 0
		for _, l := range leases {
			if l.Pinned() {
				pins++
				pinLines = append(pinLines, fmt.Sprintf("  %s/%s\t%d\t%s\tsince %s", s.Name, l.Service, l.Port, l.PinReason, l.PinnedAt.Format("2006-01-02")))
			}
		}
		mark := ""
		if s.Name == c.SlotName {
			mark = " *"
		}
		pinCol := "-"
		if pins > 0 {
			pinCol = strconv.Itoa(pins)
		}
		fmt.Fprintf(tw, "%s%s\t%s\t%s\t%s\t%s\n", s.Name, mark, strings.Join(bs, ","), infra, pinCol, s.RootPath)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if len(pinLines) == 0 {
		return nil
	}
	if !*showPins {
		fmt.Fprintf(stdout, "%d pinned port(s); `port-keeper slot ls --pins` lists them\n", len(pinLines))
		return nil
	}
	fmt.Fprintln(stdout, "pinned ports (migration aid; unpin once references use `port-keeper url`):")
	tw2 := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	for _, l := range pinLines {
		fmt.Fprintln(tw2, l)
	}
	return tw2.Flush()
}

func cmdSlotRm(ctx context.Context, a *app.App, cwd, slot string, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("slot rm", flag.ContinueOnError)
	force := fs.Bool("force", false, "release even while services are listening")
	cascade := fs.Bool("cascade", false, "also release slots that share this slot's infra")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	name := first(pos)
	if name == "" {
		return errors.New("usage: port-keeper slot rm <name>")
	}
	c, err := a.Resolve(ctx, cwd, slot)
	if err != nil {
		return err
	}
	released, err := a.ReleaseSlot(ctx, c, name, *force, *cascade)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "released %s\n", strings.Join(released, ", "))
	return nil
}

// --- env ---

func cmdEnv(ctx context.Context, a *app.App, cwd, slot string, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("env", flag.ContinueOnError)
	format := fs.String("format", "dotenv", "dotenv|export|json|mise|direnv|claude-env")
	toStdout := fs.Bool("stdout", false, "with --format dotenv: print the block instead of writing the file")
	ifPresent := fs.Bool("if-present", false, "exit 0 silently when the directory has no manifest (for shell and agent hooks)")
	if err := noPositionals("env", fs, args); err != nil {
		return err
	}
	c, err := a.Resolve(ctx, cwd, slot)
	if err != nil {
		if *ifPresent && errors.Is(err, manifest.ErrNotFound) {
			return nil
		}
		return err
	}
	if err := c.RequireBound(); err != nil {
		return err
	}
	warn(stderr, c)
	if c.Slot == nil && c.SlotSource == "default" {
		// First use in a fresh checkout: create the default slot so `env` just works.
		if _, _, err := a.SlotNew(ctx, c, app.NewSlotOptions{Name: c.SlotName, BindRoot: true}); err != nil {
			return err
		}
	}
	text, r, err := a.EnvText(ctx, c, *format)
	if err != nil {
		return err
	}
	if *format != "dotenv" || *toStdout {
		_, err := io.WriteString(stdout, text)
		return err
	}
	path, changed, err := a.WriteDotenv(ctx, c, text)
	if err != nil {
		return err
	}
	state := "unchanged"
	if changed {
		state = "updated"
	}
	fmt.Fprintf(stderr, "%s %s (%s/%s, %d services)\n", state, path, r.Project, r.Slot, len(r.Ports))
	if gitx.InRepo(c.Manifest.Dir) && !gitx.Ignored(c.Manifest.Dir, c.Manifest.Render.DotenvPath) {
		fmt.Fprintf(stderr, "warning: %s is not gitignored; add it before committing\n", c.Manifest.Render.DotenvPath)
	}
	// Last look before the numbers are used: report leases someone else took.
	if rows, err := a.Status(ctx, c, false); err == nil {
		for _, row := range rows {
			if row.State == app.StateHijacked {
				fmt.Fprintf(stderr, "warning: %s (%d) is held by %s whose working directory is outside this working copy; check with `port-keeper status` before starting it\n", row.Service, row.Port, row.ListenerLabel())
			}
		}
	}
	return nil
}

// --- url ---

func cmdURL(ctx context.Context, a *app.App, cwd, slot string, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("url", flag.ContinueOnError)
	open := fs.Bool("open", false, "open in the default browser")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	ref := first(pos)
	if ref == "" {
		return errors.New("usage: port-keeper url <service> | <project>/<slot>/<service>")
	}
	var c *app.Context
	service := ref
	if parts := strings.Split(ref, "/"); len(parts) == 3 {
		c, err = a.LookupProject(ctx, parts[0], parts[1])
		service = parts[2]
	} else if len(parts) == 1 {
		c, err = a.Resolve(ctx, cwd, slot)
		if err == nil {
			err = c.RequireBound()
		}
	} else {
		return fmt.Errorf("reference %q must be <service> or <project>/<slot>/<service>", ref)
	}
	if err != nil {
		return err
	}
	r, err := a.Resolved(ctx, c)
	if err != nil {
		return err
	}
	u, port, err := app.ServiceURL(c.Manifest, r, service)
	if err != nil {
		return err
	}
	if u == "" {
		fmt.Fprintf(stdout, "%s:%d\n", r.Host, port)
		return nil
	}
	fmt.Fprintln(stdout, u)
	if *open {
		return openBrowser(u)
	}
	return nil
}

func openBrowser(u string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	return cmd.Start()
}

// --- status ---

func cmdStatus(ctx context.Context, a *app.App, cwd, slot string, args []string, stdout io.Writer) error {
	if err := noPositionals("status", flag.NewFlagSet("status", flag.ContinueOnError), args); err != nil {
		return err
	}
	c, err := a.Resolve(ctx, cwd, slot)
	if err != nil {
		return err
	}
	if err := c.RequireBound(); err != nil {
		return err
	}
	warn(stdout, c)
	rows, err := a.Status(ctx, c, true)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s/%s (slot from %s)\n", c.Manifest.Project.Name, c.SlotName, c.SlotSource)
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SERVICE\tPORT\tSTATE\tNOTE")
	for _, r := range rows {
		var notes []string
		if r.Shared {
			notes = append(notes, "shared")
		}
		if r.Pinned {
			notes = append(notes, "pinned")
		}
		if r.Owner.PID != 0 {
			notes = append(notes, r.ListenerLabel())
		}
		if r.State == app.StateHijacked {
			notes = append(notes, "listener's cwd "+r.Owner.Cwd+" is outside this working copy")
		}
		if r.State == app.StateStale && r.LastSeen != nil {
			notes = append(notes, "last seen "+r.LastSeen.Format("2006-01-02"))
		}
		fmt.Fprintf(tw, "%s\t%d\t%s\t%s\n", r.Service, r.Port, r.State, strings.Join(notes, ", "))
	}
	return tw.Flush()
}

// --- gc ---

func cmdGC(ctx context.Context, a *app.App, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("gc", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "release the listed slots")
	if err := noPositionals("gc", fs, args); err != nil {
		return err
	}
	stale, err := a.StaleSlots(ctx)
	if err != nil {
		return err
	}
	if len(stale) == 0 {
		fmt.Fprintf(stdout, "no slots idle for more than %d days\n", a.Cfg.StaleDays)
		return nil
	}
	for _, s := range stale {
		seen := "never seen listening"
		if s.LastSeen != nil {
			seen = "last seen " + s.LastSeen.Format("2006-01-02")
		}
		orphan := ""
		if s.Orphan {
			orphan = "\t(working copy gone)"
		}
		fmt.Fprintf(stdout, "%s/%s\t%s\tcreated %s%s\n", s.Project, s.Slot, seen, s.Created.Format("2006-01-02"), orphan)
	}
	if !*yes {
		fmt.Fprintf(stdout, "%d stale slot(s). Re-run with --yes to release them.\n", len(stale))
		return nil
	}
	for _, s := range stale {
		var err error
		if s.Orphan {
			err = a.ReleaseOrphan(ctx, s.Project, s.SlotID)
		} else if c, lerr := a.LookupProject(ctx, s.Project, s.Slot); lerr != nil {
			err = a.ReleaseOrphan(ctx, s.Project, s.SlotID) // manifest unreadable: release from the ledger alone
		} else {
			_, err = a.ReleaseSlot(ctx, c, s.Slot, false, false)
		}
		if err != nil {
			fmt.Fprintf(stdout, "skip %s/%s: %v\n", s.Project, s.Slot, err)
			continue
		}
		fmt.Fprintf(stdout, "released %s/%s\n", s.Project, s.Slot)
	}
	return nil
}

// --- doctor ---

func cmdDoctor(ctx context.Context, a *app.App, cwd string, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fix := fs.Bool("fix", false, "tighten permissions")
	if err := noPositionals("doctor", fs, args); err != nil {
		return err
	}
	problems := 0
	report := func(level, msg string) {
		if level != "ok" {
			problems++
		}
		fmt.Fprintf(stdout, "[%s] %s\n", level, msg)
	}
	dir := config.StateDir()
	if st, err := os.Stat(dir); err == nil {
		if st.Mode().Perm()&0o077 != 0 {
			if *fix {
				_ = os.Chmod(dir, 0o700)
				report("ok", "state dir permissions tightened to 0700")
			} else {
				report("warn", fmt.Sprintf("state dir %s is mode %04o; expected 0700 (run doctor --fix)", dir, st.Mode().Perm()))
			}
		} else {
			report("ok", "state dir is 0700")
		}
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		p := a.Ledger.Path() + suffix
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		if st.Mode().Perm()&0o077 != 0 {
			if *fix {
				_ = os.Chmod(p, 0o600)
				report("ok", filepath.Base(p)+" permissions tightened to 0600")
			} else {
				report("warn", fmt.Sprintf("%s is mode %04o; expected 0600 (run doctor --fix)", p, st.Mode().Perm()))
			}
		} else {
			report("ok", filepath.Base(p)+" is 0600")
		}
	}
	for _, w := range a.Cfg.Pool.Warnings() {
		report("warn", w)
	}
	report("ok", fmt.Sprintf("pool holds %d ports (%s)", a.Cfg.Pool.Capacity(), poolString(a.Cfg.Pool)))
	if c, err := a.Resolve(ctx, cwd, ""); err == nil {
		m := c.Manifest
		if gitx.InRepo(m.Dir) {
			switch {
			case gitx.Tracked(m.Dir, m.Render.DotenvPath):
				report("fail", m.Render.DotenvPath+" is tracked by git; port-keeper refuses to render into it. Add it to .gitignore and git rm --cached it")
			case !gitx.Ignored(m.Dir, m.Render.DotenvPath):
				report("warn", m.Render.DotenvPath+" is not gitignored; add it before committing")
			default:
				report("ok", m.Render.DotenvPath+" is gitignored")
			}
		}
		for _, s := range m.Services {
			if len(s.Label) > manifest.MaxLabel {
				report("warn", fmt.Sprintf("service %q label is longer than %d characters; it is cut for agents", s.Name, manifest.MaxLabel))
			}
		}
	}
	stale, _ := a.StaleSlots(ctx)
	if len(stale) > 0 {
		report("warn", fmt.Sprintf("%d slot(s) idle for more than %d days (port-keeper gc)", len(stale), a.Cfg.StaleDays))
	} else {
		report("ok", "no stale slots")
	}
	pins, _ := a.Ledger.ListPins(ctx)
	old := 0
	for _, p := range pins {
		if time.Since(p.PinnedAt) > 90*24*time.Hour {
			old++
		}
	}
	switch {
	case len(pins) == 0:
		report("ok", "no pinned ports")
	case old > 0:
		report("warn", fmt.Sprintf("%d pinned port(s), %d older than 90 days; pins are a migration aid, unpin once docs use `port-keeper url`", len(pins), old))
	default:
		report("ok", fmt.Sprintf("%d pinned port(s) (migration aid; unpin when possible)", len(pins)))
	}
	if ports, ok := probe.ListenersOfCommand(ctx, "port-keeper"); !ok {
		report("ok", "self-listener check skipped (lsof not available); port-keeper has no listening code path")
	} else if len(ports) > 0 {
		report("fail", fmt.Sprintf("a process named port-keeper is listening on %v; port-keeper must never listen. Stop it and report a bug", ports))
	} else {
		report("ok", "no process named port-keeper is listening")
	}
	if problems == 0 {
		fmt.Fprintln(stdout, "all green")
	}
	return nil
}

func poolString(p config.Pool) string {
	var parts []string
	for _, r := range p.Ranges {
		parts = append(parts, fmt.Sprintf("%d-%d", r[0], r[1]))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

// --- pin / unpin / reassign ---

func cmdPin(ctx context.Context, a *app.App, cwd, slot string, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("pin", flag.ContinueOnError)
	reason := fs.String("reason", "", "why these ports must stay fixed (required)")
	force := fs.Bool("force", false, "pin even if something is listening on the port")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	// Two forms: `pin <service> <port>` and `pin <service>=<port> [<service>=<port> ...]`.
	type pair struct {
		service string
		port    int
	}
	var pairs []pair
	switch {
	case len(pos) == 2 && !strings.Contains(pos[0], "="):
		port, err := strconv.Atoi(pos[1])
		if err != nil {
			return fmt.Errorf("port %q is not a number", pos[1])
		}
		pairs = append(pairs, pair{pos[0], port})
	case len(pos) >= 1:
		for _, p := range pos {
			svc, portText, ok := strings.Cut(p, "=")
			if !ok {
				return fmt.Errorf("%q: expected <service>=<port> (or the two-argument form `pin <service> <port>`)", p)
			}
			port, err := strconv.Atoi(portText)
			if err != nil {
				return fmt.Errorf("%q: port %q is not a number", p, portText)
			}
			pairs = append(pairs, pair{svc, port})
		}
	default:
		return errors.New("usage: port-keeper pin <service> <port> --reason <text>\n       port-keeper pin <service>=<port> [<service>=<port> ...] --reason <text>")
	}
	c, err := a.Resolve(ctx, cwd, slot)
	if err != nil {
		return err
	}
	failed := 0
	for _, p := range pairs {
		if err := a.Pin(ctx, c, p.service, p.port, *reason, *force); err != nil {
			failed++
			fmt.Fprintf(stdout, "skip %s: %v\n", p.service, err)
			continue
		}
		fmt.Fprintf(stdout, "pinned %s/%s/%s to %d\n", c.Manifest.Project.Name, c.SlotName, p.service, p.port)
	}
	if failed < len(pairs) {
		fmt.Fprintln(stdout, "Pins are a migration aid. Replace references to the numbers with `port-keeper url` or ${url.…} in the manifest, then `port-keeper unpin`. `doctor` will remind you.")
		fmt.Fprintln(stdout, "next: port-keeper env")
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d pin(s) skipped", failed, len(pairs))
	}
	return nil
}

func cmdUnpin(ctx context.Context, a *app.App, cwd, slot string, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("unpin", flag.ContinueOnError)
	all := fs.Bool("all", false, "unpin every pinned service of the slot")
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	c, err := a.Resolve(ctx, cwd, slot)
	if err != nil {
		return err
	}
	services := pos
	if *all {
		if c.Slot == nil {
			return fmt.Errorf("slot %q does not exist", c.SlotName)
		}
		leases, err := a.Ledger.Leases(ctx, c.Slot.ID)
		if err != nil {
			return err
		}
		services = nil
		for _, l := range leases {
			if l.Pinned() {
				services = append(services, l.Service)
			}
		}
		if len(services) == 0 {
			fmt.Fprintln(stdout, "nothing pinned")
			return nil
		}
	}
	if len(services) == 0 {
		return errors.New("usage: port-keeper unpin <service> [<service> ...] | --all")
	}
	for _, svc := range services {
		r, err := a.Unpin(ctx, c, svc)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "unpinned %s; now %d from the pool\n", svc, r.Ports[svc])
	}
	fmt.Fprintln(stdout, "next: port-keeper env")
	return nil
}

func cmdReassign(ctx context.Context, a *app.App, cwd, slot string, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("reassign", flag.ContinueOnError)
	pos, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("usage: port-keeper reassign <service>")
	}
	c, err := a.Resolve(ctx, cwd, slot)
	if err != nil {
		return err
	}
	old, port, err := a.Reassign(ctx, c, pos[0])
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s: %d -> %d\n", pos[0], old, port)
	fmt.Fprintln(stdout, "next: port-keeper env")
	return nil
}

// --- context (the client-agnostic contract for agents and shells) ---

// ContextInfo is what `port-keeper context --json` prints. It carries no port
// numbers; the environment comes from `port-keeper env`.
type ContextInfo struct {
	Project    string   `json:"project"`
	Slot       string   `json:"slot"`
	SlotSource string   `json:"slot_source"`
	Root       string   `json:"root"`
	Ready      bool     `json:"ready"`
	Unbound    bool     `json:"unbound,omitempty"`
	OwnedBy    string   `json:"slot_owned_by,omitempty"`
	Services   []string `json:"services"`
	Guidance   string   `json:"guidance"`
	Warnings   []string `json:"warnings,omitempty"`
}

func contextInfo(ctx context.Context, a *app.App, cwd, slot string) (ContextInfo, *app.Context, error) {
	c, err := a.Resolve(ctx, cwd, slot)
	if err != nil {
		return ContextInfo{}, nil, err
	}
	info := ContextInfo{Project: c.Manifest.Project.Name, Slot: c.SlotName, SlotSource: c.SlotSource, Root: c.Root, Services: c.Manifest.ServiceNames(), Warnings: c.Warnings()}
	switch {
	case c.UnboundRoot != "":
		info.Unbound, info.OwnedBy = true, c.UnboundRoot
		info.Guidance = fmt.Sprintf("this working copy (%s) has no slot of its own; slot %q belongs to %s. Run `port-keeper slot new` (or the slot_new tool) to lease this working copy's own ports before starting any server. Never pick port numbers yourself; resolve them with resolve_url.", c.Root, c.SlotName, c.UnboundRoot)
	case c.Slot == nil:
		info.Guidance = fmt.Sprintf("project %s has no slot %q yet. Run `port-keeper env` (or the slot_new tool) before starting servers. Never pick port numbers yourself; resolve them with resolve_url.", info.Project, info.Slot)
	default:
		info.Ready = true
		info.Guidance = fmt.Sprintf("this session is project %s, slot %s (services: %s). Local ports are managed by port-keeper. Never choose a port number yourself or start a server on an ad-hoc port. Use the resolve_url tool (or `port-keeper url <service>`) to find where a service runs, and refer to services by name (%s/%s/<service>) rather than by number.", info.Project, info.Slot, strings.Join(info.Services, ", "), info.Project, info.Slot)
	}
	return info, c, nil
}

func cmdContext(ctx context.Context, a *app.App, cwd, slot string, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("context", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "machine-readable output")
	ifPresent := fs.Bool("if-present", false, "exit 0 silently when the directory has no manifest")
	if err := noPositionals("context", fs, args); err != nil {
		return err
	}
	info, _, err := contextInfo(ctx, a, cwd, slot)
	if err != nil {
		if *ifPresent && errors.Is(err, manifest.ErrNotFound) {
			return nil
		}
		return err
	}
	if *asJSON {
		enc, err := json.Marshal(info)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, string(enc))
		return err
	}
	fmt.Fprintf(stdout, "%s/%s (slot from %s)\n%s\n", info.Project, info.Slot, info.SlotSource, info.Guidance)
	for _, w := range info.Warnings {
		fmt.Fprintf(stdout, "warning: %s\n", w)
	}
	return nil
}
