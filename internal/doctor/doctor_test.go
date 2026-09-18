package doctor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridhra/port-keeper-mcp/internal/manifest"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func joined(fs []Finding) string {
	var lines []string
	for _, f := range fs {
		lines = append(lines, "["+string(f.Level)+"] "+f.Msg)
	}
	return strings.Join(lines, "\n")
}

func count(fs []Finding, level Level) int {
	n := 0
	for _, f := range fs {
		if f.Level == level {
			n++
		}
	}
	return n
}

func TestMCPConfigsWarnsOnPortsAndSecrets(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	write(t, filepath.Join(home, ".claude.json"), `{"mcpServers":{"port-keeper":{"command":"port-keeper","args":["mcp","3000"],"env":{"API_TOKEN":"hunter2"}}},"projects":{"/x":{"mcpServers":{"pk-local":{"command":"/opt/port-keeper","args":["mcp"]}}}}}`)
	write(t, filepath.Join(home, ".codex", "config.toml"), "[mcp_servers.port-keeper]\ncommand = \"port-keeper\"\nargs = [\"mcp\"]\n")
	write(t, filepath.Join(root, ".vscode", "mcp.json"), `{"servers":{"other":{"command":"foo","args":["4000"],"env":{"SECRET":"x"}}}}`)
	fs := MCPConfigs(home, root)
	out := joined(fs)
	if count(fs, Warn) != 2 {
		t.Fatalf("want 2 warnings, got:\n%s", out)
	}
	if !strings.Contains(out, "port-like number in args") || !strings.Contains(out, "secret-looking env var") {
		t.Fatalf("unexpected findings:\n%s", out)
	}
	if !strings.Contains(out, "3 file(s) checked, 3 port-keeper entries") {
		t.Fatalf("summary line missing:\n%s", out)
	}
	for _, leak := range []string{"3000", "4000", "API_TOKEN", "hunter2"} {
		if strings.Contains(out, leak) {
			t.Fatalf("output leaks %q:\n%s", leak, out)
		}
	}
}

func TestMCPConfigsSkipsMissingAndWarnsUnparsable(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	fs := MCPConfigs(home, root)
	if len(fs) != 1 || fs[0].Level != OK || fs[0].Msg != "MCP client configs: none found" {
		t.Fatalf("empty home: %s", joined(fs))
	}
	write(t, filepath.Join(root, ".mcp.json"), "{")
	fs = MCPConfigs(home, root)
	out := joined(fs)
	if count(fs, Warn) != 1 || !strings.Contains(out, "cannot parse as JSON") || !strings.Contains(out, "1 file(s) checked, 0 port-keeper entries") {
		t.Fatalf("unparsable: %s", out)
	}
	fs = MCPConfigs("", "")
	if len(fs) != 1 || fs[0].Level != OK {
		t.Fatalf("no paths: %s", joined(fs))
	}
}

func TestTrackedDotenv(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	write(t, filepath.Join(dir, manifest.FileName), "[project]\nname = \"shop\"\n[[service]]\nname = \"web\"\nenv = \"WEB_PORT\"\n[[service]]\nname = \"api\"\nenv = \"API_PORT\"\n[[derive]]\nenv = \"API_BASE\"\nvalue = \"${url.api}\"\n")
	write(t, filepath.Join(dir, ".env"), "# comment\nexport WEB_PORT=3000\nAPI_BASE='http://localhost:4000'\nAPI_PORT=${WEB_PORT}\nOTHER=5\n")
	write(t, filepath.Join(dir, ".env.example"), "WEB_PORT=1\n")
	write(t, filepath.Join(dir, ".env.test"), "WEB_PORT=1\n") // never added: untracked
	write(t, filepath.Join(dir, ".env.local"), "WEB_PORT=20000\n")
	git("add", ".env", ".env.example", "port-keeper.toml")
	m, err := manifest.Load(filepath.Join(dir, manifest.FileName))
	if err != nil {
		t.Fatal(err)
	}
	fs := TrackedDotenv(m)
	out := joined(fs)
	if count(fs, Warn) != 2 || !strings.Contains(out, ".env sets WEB_PORT to a fixed value") || !strings.Contains(out, ".env sets API_BASE to a fixed value") {
		t.Fatalf("findings:\n%s", out)
	}
	for _, leak := range []string{"3000", "4000", ".env.example", ".env.test", "OTHER"} {
		if strings.Contains(out, leak) {
			t.Fatalf("output mentions %q:\n%s", leak, out)
		}
	}
	// A clean tracked file yields one ok line; no tracked file yields nothing.
	write(t, filepath.Join(dir, ".env"), "API_PORT=${WEB_PORT}\n")
	if fs := TrackedDotenv(m); len(fs) != 1 || fs[0].Level != OK {
		t.Fatalf("clean: %s", joined(fs))
	}
	git("rm", "-q", "-f", "--cached", ".env", ".env.example")
	if fs := TrackedDotenv(m); len(fs) != 0 {
		t.Fatalf("untracked: %s", joined(fs))
	}
}
