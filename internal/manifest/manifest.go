// Package manifest parses port-keeper.toml, the file a project commits.
// It carries names and templates only; port numbers never appear in it.
package manifest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

// FileName is the manifest file name at the repository root.
const FileName = "port-keeper.toml"

// ErrNotFound is returned by Find when no manifest exists up the tree.
var ErrNotFound = errors.New("no manifest")

// MaxLabel is the length at which labels are cut before they reach an agent.
const MaxLabel = 64

// DefaultBlockSize is the number of ports leased per slot when unspecified.
const DefaultBlockSize = 32

var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
var envRE = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,127}$`)

// Manifest is the parsed port-keeper.toml.
type Manifest struct {
	Project  Project   `toml:"project"`
	Services []Service `toml:"service"`
	Derives  []Derive  `toml:"derive"`
	Render   Render    `toml:"render"`

	// Dir is the directory containing the manifest (not part of the file).
	Dir string `toml:"-"`
}

// Project is the [project] table.
type Project struct {
	Name        string `toml:"name"`
	BlockSize   int    `toml:"block_size"`
	SlotDefault string `toml:"slot_default"`
}

// Service is one [[service]] entry: one port.
type Service struct {
	Name  string `toml:"name"`
	Env   string `toml:"env"`
	Proto string `toml:"proto"` // http | https | tcp
	Tier  string `toml:"tier"`  // app | infra
	Label string `toml:"label"`
}

// Derive is one [[derive]] entry: an env var built from ports.
type Derive struct {
	Env   string `toml:"env"`
	Value string `toml:"value"`
}

// Render is the [render] table.
type Render struct {
	DotenvPath   string `toml:"dotenv_path"`
	DotenvMarker string `toml:"dotenv_marker"`
	Host         string `toml:"host"`
}

// Find walks up from start looking for the manifest. It returns the manifest path.
func Find(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		p := filepath.Join(dir, FileName)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("%w: no %s found in %s or any parent directory (run `port-keeper init` at the repository root)", ErrNotFound, FileName, start)
		}
		dir = parent
	}
}

// Load parses and validates the manifest at path.
func Load(path string) (*Manifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	m, err := Parse(string(b))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	m.Dir = filepath.Dir(path)
	return m, nil
}

// Parse parses manifest text and applies defaults and validation.
func Parse(text string) (*Manifest, error) {
	var m Manifest
	md, err := toml.Decode(text, &m)
	if err != nil {
		return nil, err
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return nil, fmt.Errorf("unknown keys: %v", undecoded)
	}
	if err := m.applyDefaultsAndValidate(); err != nil {
		return nil, err
	}
	return &m, nil
}

func (m *Manifest) applyDefaultsAndValidate() error {
	if !nameRE.MatchString(m.Project.Name) {
		return fmt.Errorf("project.name %q must match %s", m.Project.Name, nameRE)
	}
	if m.Project.BlockSize == 0 {
		m.Project.BlockSize = DefaultBlockSize
	}
	if m.Project.BlockSize < 1 || m.Project.BlockSize > 1024 {
		return fmt.Errorf("project.block_size %d must be between 1 and 1024", m.Project.BlockSize)
	}
	if m.Project.SlotDefault == "" {
		m.Project.SlotDefault = "1"
	}
	if !nameRE.MatchString(m.Project.SlotDefault) {
		return fmt.Errorf("project.slot_default %q must match %s", m.Project.SlotDefault, nameRE)
	}
	if len(m.Services) == 0 {
		return errors.New("at least one [[service]] is required")
	}
	names := map[string]bool{}
	envs := map[string]bool{}
	for i := range m.Services {
		s := &m.Services[i]
		if !nameRE.MatchString(s.Name) {
			return fmt.Errorf("service[%d].name %q must match %s", i, s.Name, nameRE)
		}
		if names[s.Name] {
			return fmt.Errorf("service name %q is declared twice", s.Name)
		}
		names[s.Name] = true
		if s.Env == "" {
			return fmt.Errorf("service %q: env is required", s.Name)
		}
		if !envRE.MatchString(s.Env) {
			return fmt.Errorf("service %q: env %q must match %s", s.Name, s.Env, envRE)
		}
		if envs[s.Env] {
			return fmt.Errorf("env %q is used by two services", s.Env)
		}
		envs[s.Env] = true
		switch s.Proto {
		case "":
			s.Proto = "http"
		case "http", "https", "tcp":
		default:
			return fmt.Errorf("service %q: proto %q must be http, https or tcp", s.Name, s.Proto)
		}
		switch s.Tier {
		case "":
			s.Tier = "app"
		case "app", "infra":
		default:
			return fmt.Errorf("service %q: tier %q must be app or infra", s.Name, s.Tier)
		}
		if len(s.Label) > 256 {
			return fmt.Errorf("service %q: label longer than 256 characters", s.Name)
		}
	}
	for i, d := range m.Derives {
		if !envRE.MatchString(d.Env) {
			return fmt.Errorf("derive[%d].env %q must match %s", i, d.Env, envRE)
		}
		if envs[d.Env] {
			return fmt.Errorf("env %q is used twice", d.Env)
		}
		envs[d.Env] = true
		if d.Value == "" {
			return fmt.Errorf("derive %q: value is required", d.Env)
		}
	}
	if m.Render.DotenvPath == "" {
		m.Render.DotenvPath = ".env.local"
	}
	if filepath.IsAbs(m.Render.DotenvPath) {
		return errors.New("render.dotenv_path must be relative to the manifest directory")
	}
	if m.Render.DotenvMarker == "" {
		m.Render.DotenvMarker = "port-keeper"
	}
	if m.Render.Host == "" {
		m.Render.Host = "localhost"
	}
	if !LoopbackHost(m.Render.Host) {
		return fmt.Errorf("render.host %q must be a loopback name: localhost, 127.0.0.1, ::1 or *.localhost (port-keeper only manages local ports)", m.Render.Host)
	}
	for _, d := range m.Derives {
		if strings.ContainsAny(d.Value, "\n\r") {
			return fmt.Errorf("derive %q: value must be a single line", d.Env)
		}
	}
	for _, s := range m.Services {
		if strings.ContainsAny(s.Label, "\n\r") {
			return fmt.Errorf("service %q: label must be a single line", s.Name)
		}
	}
	return nil
}

// LoopbackHost reports whether host names the local machine.
func LoopbackHost(host string) bool {
	h := strings.ToLower(strings.Trim(host, "[]"))
	if h == "localhost" || h == "127.0.0.1" || h == "::1" {
		return true
	}
	return strings.HasSuffix(h, ".localhost") && !strings.ContainsAny(h, "/ \t\n\r@:?#")
}

// Service returns the named service.
func (m *Manifest) Service(name string) (*Service, bool) {
	for i := range m.Services {
		if m.Services[i].Name == name {
			return &m.Services[i], true
		}
	}
	return nil, false
}

// ServiceNames returns the service names in declaration order.
func (m *Manifest) ServiceNames() []string {
	out := make([]string, 0, len(m.Services))
	for _, s := range m.Services {
		out = append(out, s.Name)
	}
	return out
}

// DotenvAbs returns the absolute path of the dotenv render target.
func (m *Manifest) DotenvAbs() string {
	return filepath.Join(m.Dir, m.Render.DotenvPath)
}

// Skeleton returns the manifest text written by `port-keeper init`.
func Skeleton(project string) string {
	return fmt.Sprintf(`# port-keeper manifest. Commit this file. It holds names and templates only;
# the port numbers live in each developer's local ledger (~/.local/state/port-keeper/).

[project]
name = %q
block_size = 32          # ports leased per slot (a second block is added if exceeded)
slot_default = "1"

[[service]]
name = "web"
env = "WEB_PORT"
proto = "http"           # http | https | tcp (tcp services have no URL)
# tier = "infra"         # infra services can be shared: port-keeper slot new 2 --infra-from 1
# label = "Storefront"   # short, no secrets, no tenant names; cut to 64 chars for agents

[[service]]
name = "api"
env = "API_PORT"
proto = "http"

# Values built from ports. Variables: ${port.<service>} ${url.<service>} ${slot} ${slot.infra} ${project} ${block.base}
[[derive]]
env = "VITE_API_BASE"
value = "${url.api}"

[render]
dotenv_path = ".env.local"   # must be gitignored; port-keeper refuses to write into a tracked file
`, project)
}
