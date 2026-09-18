// Package render turns a resolved slot (ports per service) into the formats
// that reach other tools: dotenv blocks, shell exports, JSON, mise and direnv
// snippets, and Claude Code's CLAUDE_ENV_FILE.
package render

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/gridhra/port-keeper-mcp/internal/manifest"
)

// Resolved is a slot with every service's port decided.
type Resolved struct {
	Project   string
	Slot      string
	InfraSlot string // equals Slot unless --infra-from was used
	BlockBase int    // base of the first block, 0 when the slot has no pooled block
	Host      string
	Ports     map[string]int // service -> port
}

// KV is one environment variable.
type KV struct {
	Key   string
	Value string
}

// Formats lists the supported output formats.
var Formats = []string{"dotenv", "export", "json", "mise", "direnv", "claude-env"}

var placeholder = regexp.MustCompile(`\$\{([a-z]+)(?:\.([a-z0-9-]+))?\}`)

// URL builds the URL of a service, or ok=false for tcp services.
func URL(proto, host string, port int) (string, bool) {
	switch proto {
	case "http", "https":
		return fmt.Sprintf("%s://%s:%d", proto, host, port), true
	default:
		return "", false
	}
}

// Expand substitutes ${port.x} ${url.x} ${slot} ${slot.infra} ${project} ${block.base}.
func Expand(tmpl string, m *manifest.Manifest, r *Resolved) (string, error) {
	var firstErr error
	out := placeholder.ReplaceAllStringFunc(tmpl, func(match string) string {
		sub := placeholder.FindStringSubmatch(match)
		kind, arg := sub[1], sub[2]
		switch kind {
		case "port":
			p, ok := r.Ports[arg]
			if !ok {
				firstErr = fmt.Errorf("%s: unknown service %q (services: %s)", match, arg, strings.Join(m.ServiceNames(), ", "))
				return match
			}
			return strconv.Itoa(p)
		case "url":
			svc, ok := m.Service(arg)
			p, has := r.Ports[arg]
			if !ok || !has {
				firstErr = fmt.Errorf("%s: unknown service %q (services: %s)", match, arg, strings.Join(m.ServiceNames(), ", "))
				return match
			}
			u, isURL := URL(svc.Proto, r.Host, p)
			if !isURL {
				firstErr = fmt.Errorf("%s: service %q is proto=tcp and has no URL; use ${port.%s}", match, arg, arg)
				return match
			}
			return u
		case "slot":
			if arg == "infra" {
				return r.InfraSlot
			}
			if arg == "" {
				return r.Slot
			}
		case "project":
			if arg == "" {
				return r.Project
			}
		case "block":
			if arg == "base" {
				return strconv.Itoa(r.BlockBase)
			}
		}
		firstErr = fmt.Errorf("unknown template variable %s", match)
		return match
	})
	return out, firstErr
}

// Vars produces the ordered environment variables of a resolved slot:
// one per service in manifest order, then each derive, then the two
// PORT_KEEPER_* markers that let task runners know which slot they are in.
func Vars(m *manifest.Manifest, r *Resolved) ([]KV, error) {
	var out []KV
	for _, s := range m.Services {
		p, ok := r.Ports[s.Name]
		if !ok {
			return nil, fmt.Errorf("service %q has no port; run `port-keeper env` to lease one", s.Name)
		}
		out = append(out, KV{s.Env, strconv.Itoa(p)})
	}
	for _, d := range m.Derives {
		v, err := Expand(d.Value, m, r)
		if err != nil {
			return nil, fmt.Errorf("derive %s: %w", d.Env, err)
		}
		out = append(out, KV{d.Env, v})
	}
	out = append(out, KV{"PORT_KEEPER_PROJECT", r.Project}, KV{"PORT_KEEPER_SLOT", r.Slot})
	return out, nil
}

// Format renders vars in the named format. The dotenv format returns only the
// marker block body; WriteDotenv places it into the file.
func Format(format string, m *manifest.Manifest, r *Resolved, vars []KV) (string, error) {
	var b strings.Builder
	switch format {
	case "dotenv":
		for _, kv := range vars {
			fmt.Fprintf(&b, "%s=%s\n", kv.Key, dotenvQuote(kv.Value))
		}
	case "export", "direnv", "claude-env":
		for _, kv := range vars {
			fmt.Fprintf(&b, "export %s=%s\n", kv.Key, shellQuote(kv.Value))
		}
	case "mise":
		b.WriteString("[env]\n")
		for _, kv := range vars {
			fmt.Fprintf(&b, "%s = %s\n", kv.Key, strconv.Quote(kv.Value))
		}
	case "json":
		type svc struct {
			Port int    `json:"port"`
			URL  string `json:"url,omitempty"`
		}
		doc := struct {
			Project  string            `json:"project"`
			Slot     string            `json:"slot"`
			Services map[string]svc    `json:"services"`
			Env      map[string]string `json:"env"`
		}{Project: r.Project, Slot: r.Slot, Services: map[string]svc{}, Env: map[string]string{}}
		for _, s := range m.Services {
			p := r.Ports[s.Name]
			u, _ := URL(s.Proto, r.Host, p)
			doc.Services[s.Name] = svc{Port: p, URL: u}
		}
		for _, kv := range vars {
			doc.Env[kv.Key] = kv.Value
		}
		enc, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return "", err
		}
		b.Write(enc)
		b.WriteString("\n")
	default:
		return "", fmt.Errorf("unknown format %q (formats: %s)", format, strings.Join(Formats, ", "))
	}
	return b.String(), nil
}

func dotenvQuote(v string) string {
	// Newlines are quoted (escaped) so that a value from a manifest can never
	// smuggle a second KEY=VALUE line into the file.
	if v == "" || strings.ContainsAny(v, " \t#'\"$\\\n\r") {
		return strconv.Quote(v)
	}
	return v
}

func shellQuote(v string) string {
	if v == "" {
		return "''"
	}
	safe := true
	if strings.ContainsAny(v, "\n\r") {
		return strconv.Quote(v) // bash-compatible $'…' would be nicer; double quotes keep it on one logical value
	}
	for _, c := range v {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-_./:,=@%+", c)) {
			safe = false
			break
		}
	}
	if safe {
		return v
	}
	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}

// Markers returns the begin and end marker lines for a dotenv block.
func Markers(marker, project, slot string) (string, string) {
	return fmt.Sprintf("# >>> %s: %s/%s >>>", marker, project, slot), fmt.Sprintf("# <<< %s <<<", marker)
}

// blockRE matches the marker block; group 1 is the "project/slot" header and
// group 2 the body between the marker lines.
func blockRE(marker string) *regexp.Regexp {
	return regexp.MustCompile(`(?ms)^# >>> ` + regexp.QuoteMeta(marker) + `: ([^\n]*?) >>>\n(.*?)^# <<< ` + regexp.QuoteMeta(marker) + ` <<<\n?`)
}

// DotenvBlock finds the marker block in content and returns its "project/slot"
// header and body (what Format("dotenv") produced).
func DotenvBlock(content, marker string) (header, body string, found bool) {
	m := blockRE(marker).FindStringSubmatch(content)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// WriteDotenv replaces (or appends) the marker block in the file at path.
// The file is created with mode 0600 when missing. It is idempotent.
func WriteDotenv(path, marker, project, slot, body string) (changed bool, err error) {
	begin, end := Markers(marker, project, slot)
	block := begin + "\n" + body + end + "\n"
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	content := string(existing)
	re := blockRE(marker)
	var updated string
	if re.MatchString(content) {
		updated = re.ReplaceAllLiteralString(content, block)
	} else {
		if content != "" && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		if content != "" {
			content += "\n"
		}
		updated = content + block
	}
	if updated == content {
		return false, nil
	}
	mode := os.FileMode(0o600)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	// Write next to the target and rename, so an interrupted write never leaves
	// the user's other variables half-written.
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return false, err
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.WriteString(updated); err != nil {
		_ = tmp.Close()
		cleanup()
		return false, err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		cleanup()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return false, err
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return false, err
	}
	return true, nil
}

// SortedServices returns service names sorted, for stable listings.
func SortedServices(r *Resolved) []string {
	out := make([]string, 0, len(r.Ports))
	for k := range r.Ports {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
