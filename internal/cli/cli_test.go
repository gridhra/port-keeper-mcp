package cli

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/gridhra/port-keeper-mcp/internal/app"
	"github.com/gridhra/port-keeper-mcp/internal/ledger"
)

// run executes the CLI in dir with an isolated ledger and returns stdout+stderr.
func cli(t *testing.T, dir string, args ...string) (string, int) {
	t.Helper()
	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	var out bytes.Buffer
	code := Main(args, &out, &out)
	return out.String(), code
}

func TestEndToEnd(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	if err := exec.Command("git", "-C", dir, "init", "-q").Run(); err != nil {
		t.Skip("git not available")
	}

	out, code := cli(t, dir, "init", "--name", "shop")
	if code != 0 || !strings.Contains(out, "wrote") {
		t.Fatalf("init: %d %s", code, out)
	}
	gi, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if !strings.Contains(string(gi), ".env.local") {
		t.Fatalf("init did not gitignore .env.local: %s", gi)
	}
	// env creates the default slot and writes the block.
	out, code = cli(t, dir, "env")
	if code != 0 || !strings.Contains(out, "updated") || strings.Contains(out, "warning") {
		t.Fatalf("env: %d %s", code, out)
	}
	env, _ := os.ReadFile(filepath.Join(dir, ".env.local"))
	if !strings.Contains(string(env), "WEB_PORT=20000") || !strings.Contains(string(env), "VITE_API_BASE=http://localhost:20001") {
		t.Fatalf(".env.local: %s", env)
	}
	// Flags after positionals are honoured.
	out, code = cli(t, dir, "slot", "new", "2", "--no-bind")
	if code != 0 || !strings.Contains(out, "slot shop/2 created") {
		t.Fatalf("slot new: %d %s", code, out)
	}
	out, _ = cli(t, dir, "slot", "ls")
	if !strings.Contains(out, "1 *") || !strings.Contains(out, "20032-20063") {
		t.Fatalf("slot ls: %s", out)
	}
	out, code = cli(t, dir, "url", "shop/2/api")
	if code != 0 || strings.TrimSpace(out) != "http://localhost:20033" {
		t.Fatalf("url: %d %s", code, out)
	}
	out, code = cli(t, dir, "--slot", "2", "url", "web")
	if code != 0 || strings.TrimSpace(out) != "http://localhost:20032" {
		t.Fatalf("--slot url: %d %s", code, out)
	}
	// Batch pin with one refusal (a reason is required).
	out, code = cli(t, dir, "pin", "web=3001", "api=3002")
	if code == 0 || !strings.Contains(out, "reason") {
		t.Fatalf("pin without reason: %d %s", code, out)
	}
	out, code = cli(t, dir, "pin", "web=3001", "api=3002", "--reason", "legacy")
	if code != 0 || strings.Count(out, "pinned shop/1/") != 2 {
		t.Fatalf("batch pin: %d %s", code, out)
	}
	out, _ = cli(t, dir, "slot", "ls", "--pins")
	if !strings.Contains(out, "1/web") || !strings.Contains(out, "3001") || !strings.Contains(out, "legacy") {
		t.Fatalf("slot ls --pins: %s", out)
	}
	out, code = cli(t, dir, "unpin", "--all")
	if code != 0 || strings.Count(out, "unpinned") != 2 {
		t.Fatalf("unpin --all: %d %s", code, out)
	}
	out, code = cli(t, dir, "reassign", "web")
	if code != 0 || !strings.Contains(out, "web: 200") {
		t.Fatalf("reassign: %d %s", code, out)
	}
	out, code = cli(t, dir, "doctor")
	if code != 0 || strings.Contains(out, "[fail]") {
		t.Fatalf("doctor: %d %s", code, out)
	}
	out, code = cli(t, dir, "slot", "rm", "2")
	if code != 0 || !strings.Contains(out, "released 2") {
		t.Fatalf("slot rm: %d %s", code, out)
	}
	// --if-present outside a project is silent.
	out, code = cli(t, t.TempDir(), "env", "--if-present")
	if code != 0 || out != "" {
		t.Fatalf("--if-present: %d %q", code, out)
	}
	out, code = cli(t, t.TempDir(), "env")
	if code == 0 || !strings.Contains(out, "port-keeper init") {
		t.Fatalf("env outside project: %d %s", code, out)
	}
}

func TestHookAndConfigErrors(t *testing.T) {
	state := t.TempDir()
	cfgDir := filepath.Join(state, "cfg")
	t.Setenv("PORT_KEEPER_STATE_DIR", state)
	t.Setenv("PORT_KEEPER_CONFIG_DIR", cfgDir)
	t.Setenv("PORT_KEEPER_SLOT", "")
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "port-keeper.toml"), []byte("[project]\nname = \"shop\"\n[[service]]\nname = \"web\"\nenv = \"WEB_PORT\"\n"), 0o644)

	// Outside a project: silent.
	out, code := cli(t, t.TempDir(), "hook", "claude-session-start")
	if code != 0 || out != "" {
		t.Fatalf("hook outside project: %d %q", code, out)
	}
	// Before any slot: context only, no env file, no slot created.
	envFile := filepath.Join(state, "claude.env")
	t.Setenv("CLAUDE_ENV_FILE", envFile)
	out, code = cli(t, dir, "hook", "claude-session-start")
	if code != 0 || !strings.Contains(out, "additionalContext") || !strings.Contains(out, "no slot") {
		t.Fatalf("hook before slot: %d %s", code, out)
	}
	if _, err := os.Stat(envFile); err == nil {
		t.Fatal("env file written before a slot exists")
	}
	if _, code := cli(t, dir, "env"); code != 0 {
		t.Fatal("env")
	}
	out, code = cli(t, dir, "hook", "claude-session-start")
	if code != 0 || !strings.Contains(out, `"hookEventName":"SessionStart"`) || !strings.Contains(out, "shop, slot 1") {
		t.Fatalf("hook: %d %s", code, out)
	}
	if strings.Contains(out, "20000") {
		t.Fatalf("hook context leaks a number: %s", out)
	}
	env, _ := os.ReadFile(envFile)
	if !strings.Contains(string(env), "export WEB_PORT=20000") || !strings.Contains(string(env), "export PORT_KEEPER_SLOT=1") {
		t.Fatalf("env file: %s", env)
	}
	// A misplaced config key is an error, not a silent no-op.
	os.MkdirAll(cfgDir, 0o700)
	os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte("[mcp]\nstale_days = 5\n"), 0o600)
	out, code = cli(t, dir, "status")
	if code == 0 || !strings.Contains(out, "unknown keys") {
		t.Fatalf("config typo: %d %s", code, out)
	}
}

// TestHelperProcess is re-executed by TestConcurrentProcesses as a real port-keeper.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("PK_HELPER") != "1" {
		return
	}
	args := strings.Split(os.Getenv("PK_ARGS"), " ")
	os.Exit(Main(args, os.Stdout, os.Stderr))
}

func TestConcurrentProcesses(t *testing.T) {
	state := t.TempDir()
	t.Setenv("PORT_KEEPER_STATE_DIR", state)
	t.Setenv("PORT_KEEPER_CONFIG_DIR", filepath.Join(state, "cfg"))
	t.Setenv("PORT_KEEPER_SLOT", "")
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "port-keeper.toml"), []byte("[project]\nname = \"shop\"\nblock_size = 4\n[[service]]\nname = \"web\"\nenv = \"WEB_PORT\"\n[[service]]\nname = \"api\"\nenv = \"API_PORT\"\n"), 0o644)
	const n = 8
	type result struct {
		out string
		err error
	}
	results := make(chan result, n*2)
	spawn := func(args string) {
		cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "PK_HELPER=1", "PK_ARGS="+args, "PORT_KEEPER_STATE_DIR="+state, "PORT_KEEPER_CONFIG_DIR="+filepath.Join(state, "cfg"), "PORT_KEEPER_SLOT=")
		out, err := cmd.CombinedOutput()
		results <- result{string(out), err}
	}
	// Eight processes create distinct slots while eight more pin and render slot 1 concurrently.
	for i := 0; i < n; i++ {
		go spawn("slot new s" + string(rune('a'+i)) + " --no-bind")
		go spawn("env --format export")
	}
	for i := 0; i < n*2; i++ {
		r := <-results
		if r.err != nil {
			t.Fatalf("process failed: %v\n%s", r.err, r.out)
		}
	}
	out, code := cli(t, dir, "slot", "ls")
	if code != 0 {
		t.Fatal(out)
	}
	// Every slot has its own block and nothing overlaps: count distinct ranges.
	seen := map[string]bool{}
	blockRE := regexp.MustCompile(`^\d+-\d+$`)
	for _, line := range strings.Split(out, "\n") {
		for _, f := range strings.Fields(line) {
			if !blockRE.MatchString(f) {
				continue
			}
			if seen[f] {
				t.Fatalf("block %s appears twice:\n%s", f, out)
			}
			seen[f] = true
		}
	}
	if len(seen) != n+1 {
		t.Fatalf("expected %d slots, got %d:\n%s", n+1, len(seen), out)
	}
}

func TestHookReadsStdinAndCwdChanged(t *testing.T) {
	state := t.TempDir()
	t.Setenv("PORT_KEEPER_STATE_DIR", state)
	t.Setenv("PORT_KEEPER_CONFIG_DIR", filepath.Join(state, "cfg"))
	t.Setenv("PORT_KEEPER_SLOT", "")
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "port-keeper.toml"), []byte("[project]\nname = \"shop\"\n[[service]]\nname = \"web\"\nenv = \"WEB_PORT\"\n"), 0o644)
	if _, code := cli(t, dir, "env"); code != 0 {
		t.Fatal("env")
	}
	envFile := filepath.Join(state, "claude.env")
	t.Setenv("CLAUDE_ENV_FILE", envFile)
	// Run from an unrelated directory; the hook must use the cwd from stdin JSON.
	elsewhere := t.TempDir()
	old, _ := os.Getwd()
	os.Chdir(elsewhere)
	defer os.Chdir(old)
	a, err := appForTest()
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var out bytes.Buffer
	stdin := strings.NewReader(`{"hook_event_name":"CwdChanged","cwd":"` + dir + `","old_cwd":"/x","new_cwd":"` + dir + `"}`)
	if err := cmdHook(context.Background(), a, elsewhere, "", []string{"claude"}, stdin, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"systemMessage"`) || strings.Contains(out.String(), "additionalContext") {
		t.Fatalf("CwdChanged output: %s", out.String())
	}
	env, _ := os.ReadFile(envFile)
	if !strings.Contains(string(env), "export WEB_PORT=") {
		t.Fatalf("env file: %s", env)
	}
	out.Reset()
	stdin = strings.NewReader(`{"hook_event_name":"SessionStart","cwd":"` + dir + `"}`)
	if err := cmdHook(context.Background(), a, elsewhere, "", []string{"claude"}, stdin, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"hookEventName":"SessionStart"`) || !strings.Contains(out.String(), "additionalContext") {
		t.Fatalf("SessionStart output: %s", out.String())
	}
}

func appForTest() (*app.App, error) { return app.New(app.ActorCLI) }

func TestContextCommand(t *testing.T) {
	state := t.TempDir()
	t.Setenv("PORT_KEEPER_STATE_DIR", state)
	t.Setenv("PORT_KEEPER_CONFIG_DIR", filepath.Join(state, "cfg"))
	t.Setenv("PORT_KEEPER_SLOT", "")
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "port-keeper.toml"), []byte("[project]\nname = \"shop\"\n[[service]]\nname = \"web\"\nenv = \"WEB_PORT\"\n"), 0o644)
	out, code := cli(t, dir, "context", "--json")
	if code != 0 || !strings.Contains(out, `"ready":false`) || !strings.Contains(out, "port-keeper env") {
		t.Fatalf("context before slot: %d %s", code, out)
	}
	cli(t, dir, "env")
	out, code = cli(t, dir, "context", "--json")
	if code != 0 || !strings.Contains(out, `"ready":true`) || !strings.Contains(out, `"slot_source":"root"`) || strings.Contains(out, "20000") {
		t.Fatalf("context after slot: %d %s", code, out)
	}
	out, code = cli(t, t.TempDir(), "context", "--json", "--if-present")
	if code != 0 || out != "" {
		t.Fatalf("context --if-present: %d %q", code, out)
	}
}

// A ledger upgraded by a newer binary stops the CLI with instructions, not a
// SQL error, and the ledger is left alone.
func TestNewerLedgerStopsCLI(t *testing.T) {
	state := t.TempDir()
	t.Setenv("PORT_KEEPER_STATE_DIR", state)
	t.Setenv("PORT_KEEPER_CONFIG_DIR", filepath.Join(state, "cfg"))
	t.Setenv("PORT_KEEPER_SLOT", "")
	dir := t.TempDir()
	if out, code := cli(t, dir, "init", "--name", "shop"); code != 0 {
		t.Fatalf("init: %d %s", code, out)
	}
	if out, code := cli(t, dir, "env"); code != 0 {
		t.Fatalf("env: %d %s", code, out)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.Join(state, ledger.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA user_version = " + strconv.Itoa(ledger.SchemaVersion+1)); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	out, code := cli(t, dir, "status")
	if code == 0 {
		t.Fatalf("status succeeded against a newer ledger: %s", out)
	}
	if !strings.Contains(out, "Update this binary") || strings.Contains(out, "migrate ledger") {
		t.Fatalf("unhelpful message: %s", out)
	}
	if regexp.MustCompile(`\b2[0-9]{4}\b`).MatchString(out) {
		t.Fatalf("message leaks a port number: %s", out)
	}
}

// isolate points the ledger, config and home at temp dirs so a test never
// reads the developer's real ~/.claude.json or ledger.
func isolate(t *testing.T) {
	t.Helper()
	state := t.TempDir()
	t.Setenv("PORT_KEEPER_STATE_DIR", state)
	t.Setenv("PORT_KEEPER_CONFIG_DIR", filepath.Join(state, "cfg"))
	t.Setenv("PORT_KEEPER_SLOT", "")
	t.Setenv("HOME", filepath.Join(state, "home"))
	t.Setenv("USERPROFILE", filepath.Join(state, "home"))
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	if err := exec.Command("git", "-C", dir, "init", "-q").Run(); err != nil {
		t.Skip("git not available")
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.invalid"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

const twoServices = "[project]\nname = \"shop\"\n[[service]]\nname = \"web\"\nenv = \"WEB_PORT\"\n[[service]]\nname = \"api\"\nenv = \"API_PORT\"\n"

func TestSlotNewFromBranch(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	gitInit(t, dir)
	git(t, dir, "checkout", "-q", "-b", "Feature/Login")
	if _, code := cli(t, dir, "init", "--name", "shop"); code != 0 {
		t.Fatal("init")
	}
	out, code := cli(t, dir, "slot", "new", "--from-branch")
	if code != 0 || !strings.Contains(out, `slot name "feature-login" derived from branch "Feature/Login"`) || !strings.Contains(out, "slot shop/feature-login created") {
		t.Fatalf("from-branch: %d %s", code, out)
	}
	out, code = cli(t, dir, "slot", "new", "x", "--from-branch")
	if code != 1 || !strings.Contains(out, "not both") {
		t.Fatalf("name and --from-branch: %d %s", code, out)
	}
	git(t, dir, "commit", "-q", "--allow-empty", "-m", "empty")
	git(t, dir, "checkout", "-q", "--detach")
	out, code = cli(t, dir, "slot", "new", "--from-branch")
	if code != 1 || !strings.Contains(out, "detached") {
		t.Fatalf("detached: %d %s", code, out)
	}
	out, code = cli(t, t.TempDir(), "slot", "new", "--from-branch")
	if code != 1 || !strings.Contains(out, "not inside a git repository") {
		t.Fatalf("outside git: %d %s", code, out)
	}
}

func TestStatusJSON(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "port-keeper.toml"), []byte(twoServices), 0o644)
	if _, code := cli(t, dir, "env"); code != 0 {
		t.Fatal("env")
	}
	out, code := cli(t, dir, "status", "--json")
	if code != 0 || strings.Count(strings.TrimSpace(out), "\n") != 0 {
		t.Fatalf("status --json: %d %q", code, out)
	}
	var got statusOut
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if got.Project != "shop" || got.Slot != "1" || got.SlotSource != "root" || len(got.Services) != 2 {
		t.Fatalf("fields: %+v", got)
	}
	if got.Services[0].Service != "web" || got.Services[0].Port < 20000 || got.Services[0].State != "leased" || got.Services[0].Proto != "http" {
		t.Fatalf("service row: %+v", got.Services[0])
	}
	if out, code := cli(t, dir, "status", "--json", "--slot", "nope"); code != 1 || strings.HasPrefix(out, "{") {
		t.Fatalf("unknown slot: %d %s", code, out)
	}
}

func TestContextReportsEnvDrift(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "port-keeper.toml"), []byte(twoServices), 0o644)
	cli(t, dir, "env")
	out, code := cli(t, dir, "context", "--json")
	if code != 0 || strings.Contains(out, "env_stale") {
		t.Fatalf("fresh env reported stale: %d %s", code, out)
	}
	os.WriteFile(filepath.Join(dir, "port-keeper.toml"), []byte(twoServices+"[[service]]\nname = \"mail\"\nenv = \"MAIL_PORT\"\n"), 0o644)
	out, code = cli(t, dir, "context", "--json")
	if code != 0 || !strings.Contains(out, `"env_stale":true`) || !strings.Contains(out, `"env_stale_reason":"service mail has no port yet"`) || !strings.Contains(out, "Run `port-keeper env` to refresh .env.local") || strings.Contains(out, "20000") {
		t.Fatalf("stale after manifest change: %d %s", code, out)
	}
	cli(t, dir, "env")
	if out, _ := cli(t, dir, "context", "--json"); strings.Contains(out, "env_stale") {
		t.Fatalf("still stale after env: %s", out)
	}
	os.Remove(filepath.Join(dir, ".env.local"))
	out, _ = cli(t, dir, "context")
	if !strings.Contains(out, "(.env.local does not exist)") {
		t.Fatalf("missing file not reported: %s", out)
	}
	// The Claude hook relays the same instruction, even in the one-sentence CwdChanged form.
	elsewhere := t.TempDir()
	old, _ := os.Getwd()
	os.Chdir(elsewhere)
	defer os.Chdir(old)
	a, err := appForTest()
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var hookOut bytes.Buffer
	stdin := strings.NewReader(`{"hook_event_name":"CwdChanged","cwd":"` + dir + `","new_cwd":"` + dir + `"}`)
	if err := cmdHook(context.Background(), a, elsewhere, "", []string{"claude"}, stdin, &hookOut); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(hookOut.String(), `"systemMessage"`) || !strings.Contains(hookOut.String(), "refresh .env.local (.env.local does not exist)") || strings.Contains(hookOut.String(), "20000") {
		t.Fatalf("CwdChanged: %s", hookOut.String())
	}
}

func TestDoctorChecksAndExitCode(t *testing.T) {
	isolate(t)
	home := os.Getenv("HOME")
	os.MkdirAll(home, 0o755)
	os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{"mcpServers":{"port-keeper":{"command":"port-keeper","args":["mcp"],"env":{"PORT_KEEPER_TOKEN":"x"}}}}`), 0o600)
	dir := t.TempDir()
	gitInit(t, dir)
	cli(t, dir, "init", "--name", "shop")
	os.WriteFile(filepath.Join(dir, ".env"), []byte("WEB_PORT=3000\n"), 0o644)
	git(t, dir, "add", ".env")
	cli(t, dir, "env")
	out, code := cli(t, dir, "doctor")
	if code != 0 {
		t.Fatalf("doctor with warnings only: %d %s", code, out)
	}
	for _, want := range []string{
		"[ok] MCP client configs: 1 file(s) checked, 1 port-keeper entry",
		"[warn] " + home + "/.claude.json: port-keeper entry \"port-keeper\" sets a secret-looking env var",
		"[warn] .env sets WEB_PORT to a fixed value",
		"[ok] .env.local matches the ledger",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("doctor lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "3000") || strings.Contains(out, "PORT_KEEPER_TOKEN") {
		t.Fatalf("doctor leaks a value:\n%s", out)
	}
	os.Remove(filepath.Join(dir, ".env.local"))
	if out, _ := cli(t, dir, "doctor"); !strings.Contains(out, "[warn] .env.local does not exist (port-keeper env)") {
		t.Fatalf("drift not reported:\n%s", out)
	}
	// A tracked render target is a failure and fails the command.
	cli(t, dir, "env")
	git(t, dir, "add", "-f", ".env.local")
	out, code = cli(t, dir, "doctor")
	if code != 1 || !strings.Contains(out, "[fail] .env.local is tracked") || !strings.Contains(out, "1 check(s) failed") {
		t.Fatalf("doctor with a failure: %d %s", code, out)
	}
	git(t, dir, "rm", "-q", "-f", "--cached", ".env.local")
	if _, code := cli(t, dir, "doctor"); code != 0 {
		t.Fatalf("doctor after fix: %d", code)
	}
}

func TestCompletionScripts(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	for shell, marker := range map[string]string{"zsh": "#compdef port-keeper", "bash": "complete -F _port_keeper port-keeper", "fish": "complete -c port-keeper"} {
		out, code := cli(t, dir, "completion", shell)
		if code != 0 || !strings.Contains(out, marker) || !strings.Contains(out, "port-keeper __complete") {
			t.Fatalf("completion %s: %d %s", shell, code, out)
		}
	}
	if out, code := cli(t, dir, "completion", "pwsh"); code != 1 || !strings.Contains(out, "unsupported shell") {
		t.Fatalf("completion pwsh: %d %s", code, out)
	}
}

func TestCompleteCandidates(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "port-keeper.toml"), []byte(twoServices), 0o644)
	numbers := regexp.MustCompile(`\d{4,}`)
	expect := func(want string, args ...string) {
		t.Helper()
		out, code := cli(t, dir, append([]string{"__complete"}, args...)...)
		if code != 0 || out != want {
			t.Fatalf("__complete %v: %d %q, want %q", args, code, out, want)
		}
		if numbers.MatchString(out) {
			t.Fatalf("__complete %v prints a number: %q", args, out)
		}
	}
	expect("web\napi\n", "service")
	expect("", "slot")
	cli(t, dir, "env")
	expect("1\n", "slot")
	expect("", "bogus")
	expect("", "service", "extra")
	out, _ := cli(t, dir, "__complete", "format")
	if !strings.Contains(out, "dotenv\n") || !strings.Contains(out, "claude-env\n") {
		t.Fatalf("formats: %q", out)
	}
	out, _ = cli(t, dir, "__complete", "command")
	if !strings.Contains(out, "doctor\n") || strings.Contains(out, "__complete") {
		t.Fatalf("commands: %q", out)
	}
	// Outside a project everything is silent and exits 0.
	dir = t.TempDir()
	for _, kind := range []string{"service", "slot"} {
		expect("", kind)
	}
}
