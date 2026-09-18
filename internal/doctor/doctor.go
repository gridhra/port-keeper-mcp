// Package doctor holds the self-checks that need no ledger: they read files
// on disk and return findings for the CLI to print. Findings name files and
// keys, never the values found in them, so that `doctor` output can be pasted
// into an issue without leaking a port number or a token.
package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/gridhra/port-keeper-mcp/internal/gitx"
	"github.com/gridhra/port-keeper-mcp/internal/manifest"
)

// Level is the severity of a finding, printed as `[level]` by the CLI.
type Level string

const (
	OK   Level = "ok"
	Warn Level = "warn"
	Fail Level = "fail"
)

// Finding is one line of doctor output.
type Finding struct {
	Level Level
	Msg   string
}

// mcpEntry is the part of an MCP server entry every client config shares.
type mcpEntry struct {
	Command string            `json:"command" toml:"command"`
	Args    []string          `json:"args" toml:"args"`
	Env     map[string]string `json:"env" toml:"env"`
}

// configFile is one MCP client configuration file and how to find the
// server entries in it.
type configFile struct {
	path string
	kind string // "json" (top-level key `key`) or "toml" (table `key`)
	key  string
	// claudeProjects walks `projects.<path>.mcpServers` too (Claude Code's user file).
	claudeProjects bool
}

var (
	portLikeRE  = regexp.MustCompile(`\d{4,5}`)
	secretKeyRE = regexp.MustCompile(`(?i)token|secret|key|password`)
)

// MCPConfigs inspects the MCP client configuration files under home and the
// repository root for port-keeper entries that carry a port-like number or a
// secret-looking environment variable. port-keeper needs neither, and a number
// in a config file is exactly the leak the ledger exists to prevent. Missing
// files are skipped; findings name the file and the entry, never a value.
func MCPConfigs(home, repoRoot string) []Finding {
	var files []configFile
	if home != "" {
		files = append(files,
			configFile{path: filepath.Join(home, ".claude.json"), kind: "json", key: "mcpServers", claudeProjects: true},
			configFile{path: filepath.Join(home, ".cursor", "mcp.json"), kind: "json", key: "mcpServers"},
			configFile{path: filepath.Join(home, ".codex", "config.toml"), kind: "toml", key: "mcp_servers"},
		)
		switch runtime.GOOS {
		case "darwin":
			files = append(files, configFile{path: filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json"), kind: "json", key: "mcpServers"})
		case "windows":
			if appdata := os.Getenv("APPDATA"); appdata != "" {
				files = append(files, configFile{path: filepath.Join(appdata, "Claude", "claude_desktop_config.json"), kind: "json", key: "mcpServers"})
			}
		default:
			files = append(files, configFile{path: filepath.Join(home, ".config", "Claude", "claude_desktop_config.json"), kind: "json", key: "mcpServers"})
		}
	}
	if repoRoot != "" {
		files = append(files,
			configFile{path: filepath.Join(repoRoot, ".mcp.json"), kind: "json", key: "mcpServers"},
			configFile{path: filepath.Join(repoRoot, ".cursor", "mcp.json"), kind: "json", key: "mcpServers"},
			configFile{path: filepath.Join(repoRoot, ".vscode", "mcp.json"), kind: "json", key: "servers"},
		)
	}
	var out []Finding
	checked, entries := 0, 0
	for _, f := range files {
		b, err := os.ReadFile(f.path)
		if err != nil {
			continue
		}
		checked++
		servers, err := f.entries(b)
		if err != nil {
			out = append(out, Finding{Warn, fmt.Sprintf("%s: cannot parse as %s (skipped)", f.path, strings.ToUpper(f.kind))})
			continue
		}
		for name, e := range servers {
			if !strings.Contains(strings.ToLower(name+" "+e.Command), "port-keeper") {
				continue
			}
			entries++
			out = append(out, inspectEntry(f.path, name, e)...)
		}
	}
	switch {
	case checked == 0:
		out = append(out, Finding{OK, "MCP client configs: none found"})
	default:
		out = append(out, Finding{OK, fmt.Sprintf("MCP client configs: %d file(s) checked, %d port-keeper %s", checked, entries, plural(entries, "entry", "entries"))})
	}
	return out
}

func (f configFile) entries(b []byte) (map[string]mcpEntry, error) {
	servers := map[string]mcpEntry{}
	switch f.kind {
	case "toml":
		var t struct {
			Servers map[string]mcpEntry `toml:"mcp_servers"`
		}
		if _, err := toml.Decode(string(b), &t); err != nil {
			return nil, err
		}
		for k, v := range t.Servers {
			servers[k] = v
		}
	default:
		var doc map[string]json.RawMessage
		if err := json.Unmarshal(b, &doc); err != nil {
			return nil, err
		}
		if raw, ok := doc[f.key]; ok {
			var m map[string]mcpEntry
			if err := json.Unmarshal(raw, &m); err == nil {
				for k, v := range m {
					servers[k] = v
				}
			}
		}
		if f.claudeProjects {
			var projects map[string]struct {
				Servers map[string]mcpEntry `json:"mcpServers"`
			}
			if raw, ok := doc["projects"]; ok && json.Unmarshal(raw, &projects) == nil {
				for p, pv := range projects {
					for k, v := range pv.Servers {
						servers[k+" (project "+p+")"] = v
					}
				}
			}
		}
	}
	return servers, nil
}

func inspectEntry(path, name string, e mcpEntry) []Finding {
	var out []Finding
	prefix := fmt.Sprintf("%s: port-keeper entry %q", path, name)
	for _, a := range e.Args {
		if hasPortLike(a) {
			out = append(out, Finding{Warn, prefix + " has a port-like number in args; port-keeper takes no port"})
			break
		}
	}
	secret, number := false, false
	for k, v := range e.Env {
		if secretKeyRE.MatchString(k) {
			secret = true
		}
		if hasPortLike(v) {
			number = true
		}
	}
	if secret {
		out = append(out, Finding{Warn, prefix + " sets a secret-looking env var; port-keeper needs no credentials and never reads them"})
	}
	if number {
		out = append(out, Finding{Warn, prefix + " has a port-like number in env; port-keeper takes no port"})
	}
	return out
}

// hasPortLike reports whether s contains a number that could be a port.
func hasPortLike(s string) bool {
	for _, m := range portLikeRE.FindAllString(s, -1) {
		if n, err := strconv.Atoi(m); err == nil && n >= 1024 && n <= 65535 {
			return true
		}
	}
	return false
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

var (
	dotenvSkipRE   = regexp.MustCompile(`\.(example|sample|template)$`)
	fixedValueRE   = regexp.MustCompile(`^\d+$`)
	hostPortLikeRE = regexp.MustCompile(`:\d{2,5}(\D|$)`)
)

// TrackedDotenv warns about git-tracked dotenv files (`.env`, `.env.*`) in the
// manifest directory and the repository root that set one of the variables
// port-keeper manages to a fixed number or a host:port. Which file a dotenv
// loader lets win differs per tool, so such a line can silently override the
// rendered value. Example and template files are skipped; the value itself is
// never printed.
func TrackedDotenv(m *manifest.Manifest) []Finding {
	managed := map[string]bool{}
	for _, s := range m.Services {
		managed[s.Env] = true
	}
	for _, d := range m.Derives {
		managed[d.Env] = true
	}
	dirs := []string{m.Dir}
	if root := gitx.Toplevel(m.Dir); root != "" && realPath(root) != realPath(m.Dir) {
		dirs = append(dirs, root)
	}
	seen := map[string]bool{realPath(m.DotenvAbs()): true}
	var candidates []string
	for _, dir := range dirs {
		for _, f := range gitx.TrackedFiles(dir, ".env", ".env.*") {
			abs := filepath.Join(dir, f)
			if seen[realPath(abs)] || dotenvSkipRE.MatchString(f) {
				continue
			}
			seen[realPath(abs)] = true
			candidates = append(candidates, abs)
		}
	}
	var out []Finding
	for _, abs := range candidates {
		b, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(m.Dir, abs)
		if err != nil {
			rel = abs
		}
		for _, key := range fixedManagedKeys(string(b), managed) {
			out = append(out, Finding{Warn, fmt.Sprintf("%s sets %s to a fixed value; which wins depends on your dotenv loader", rel, key)})
		}
	}
	if len(candidates) > 0 && len(out) == 0 {
		out = append(out, Finding{OK, "no tracked .env file fixes a managed variable"})
	}
	return out
}

// realPath resolves symlinks so that the same file reached through two paths
// (macOS's /var and /private/var, say) is seen once.
func realPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// fixedManagedKeys returns, in file order, the managed keys the dotenv text
// sets to a number or a host:port.
func fixedManagedKeys(text string, managed map[string]bool) []string {
	var keys []string
	reported := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "export "))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if !managed[key] || reported[key] {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if fixedValueRE.MatchString(value) || hostPortLikeRE.MatchString(value) {
			reported[key] = true
			keys = append(keys, key)
		}
	}
	return keys
}
