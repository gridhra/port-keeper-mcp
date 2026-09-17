// Package config loads the per-user configuration of port-keeper and resolves
// the directories it uses. Nothing in here touches a project; it is all about
// the machine-wide pool and where the ledger lives.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/BurntSushi/toml"
)

// Config is the machine-wide configuration (~/.config/port-keeper/config.toml).
type Config struct {
	Pool      Pool `toml:"pool"`
	MCP       MCP  `toml:"mcp"`
	StaleDays int  `toml:"stale_days"`
}

// Pool describes where pooled ports may come from.
type Pool struct {
	Ranges    [][]int `toml:"ranges"`
	DenyPorts []int   `toml:"deny_ports"`
}

// MCP holds the opt-in switches for the MCP tools that are not registered by default.
type MCP struct {
	EnableRelease bool `toml:"enable_release"`
	EnableListAll bool `toml:"enable_list_all"`
}

// Default returns the built-in configuration: pool 20000-31999, which stays
// clear of the conventional development ports (3000, 5000, 5173, 8000, 8080,
// 9000, 9229, ...), of the ports macOS services take (5000/7000), and of the
// OS ephemeral ranges (macOS 49152+, Linux 32768+).
func Default() *Config {
	return &Config{
		Pool: Pool{
			Ranges:    [][]int{{20000, 31999}},
			DenyPorts: []int{27017, 28015, 29092},
		},
		StaleDays: 30,
	}
}

// ConfigDir returns the configuration directory (PORT_KEEPER_CONFIG_DIR,
// then $XDG_CONFIG_HOME/port-keeper, then ~/.config/port-keeper).
func ConfigDir() string {
	if d := os.Getenv("PORT_KEEPER_CONFIG_DIR"); d != "" {
		return d
	}
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "port-keeper")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "port-keeper")
}

// StateDir returns where the ledger lives (PORT_KEEPER_STATE_DIR,
// then $XDG_STATE_HOME/port-keeper, then ~/.local/state/port-keeper).
func StateDir() string {
	if d := os.Getenv("PORT_KEEPER_STATE_DIR"); d != "" {
		return d
	}
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, "port-keeper")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "port-keeper")
}

// Path returns the config file path.
func Path() string { return filepath.Join(ConfigDir(), "config.toml") }

// Load reads the config file if it exists and overlays it on the defaults.
func Load() (*Config, error) {
	cfg := Default()
	b, err := os.ReadFile(Path())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}
	// A file that sets [pool] replaces the default pool entirely.
	var file Config
	md, err := toml.Decode(string(b), &file)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", Path(), err)
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		// A silently ignored key (a typo, or stale_days placed under [mcp])
		// would make the user believe a setting is in force. Refuse instead.
		return nil, fmt.Errorf("%s: unknown keys %v (known: pool.ranges, pool.deny_ports, mcp.enable_release, mcp.enable_list_all, stale_days at top level)", Path(), undecoded)
	}
	if len(file.Pool.Ranges) > 0 {
		cfg.Pool.Ranges = file.Pool.Ranges
	}
	if file.Pool.DenyPorts != nil {
		cfg.Pool.DenyPorts = file.Pool.DenyPorts
	}
	cfg.MCP = file.MCP
	if file.StaleDays < 0 {
		return nil, fmt.Errorf("%s: stale_days must be >= 0 (0 disables stale detection)", Path())
	}
	if file.StaleDays > 0 || md.IsDefined("stale_days") {
		cfg.StaleDays = file.StaleDays
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", Path(), err)
	}
	return cfg, nil
}

// Validate checks the pool for shape errors.
func (c *Config) Validate() error {
	if len(c.Pool.Ranges) == 0 {
		return errors.New("pool.ranges is empty")
	}
	for _, r := range c.Pool.Ranges {
		if len(r) != 2 {
			return fmt.Errorf("pool.ranges entry %v must be [low, high]", r)
		}
		if r[0] < 1024 || r[1] > 65535 || r[0] > r[1] {
			return fmt.Errorf("pool.ranges entry %v must satisfy 1024 <= low <= high <= 65535", r)
		}
	}
	return nil
}

// Contains reports whether port lies inside one of the pool ranges.
func (p Pool) Contains(port int) bool {
	for _, r := range p.Ranges {
		if port >= r[0] && port <= r[1] {
			return true
		}
	}
	return false
}

// Denied reports whether port is on the deny list.
func (p Pool) Denied(port int) bool {
	for _, d := range p.DenyPorts {
		if d == port {
			return true
		}
	}
	return false
}

// Capacity returns how many ports the pool holds after denials.
func (p Pool) Capacity() int {
	n := 0
	for _, r := range p.Ranges {
		n += r[1] - r[0] + 1
	}
	for _, d := range p.DenyPorts {
		if p.Contains(d) {
			n--
		}
	}
	return n
}

// Warnings returns human-readable concerns about the pool that are not errors
// (overlap with ephemeral ranges or conventional ports).
func (p Pool) Warnings() []string {
	var w []string
	sorted := append([][]int(nil), p.Ranges...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i][0] < sorted[j][0] })
	for _, r := range sorted {
		if r[1] >= 32768 {
			w = append(w, fmt.Sprintf("pool range %d-%d overlaps the OS ephemeral port range (Linux 32768+, macOS 49152+)", r[0], r[1]))
		}
		if r[0] < 10000 {
			w = append(w, fmt.Sprintf("pool range %d-%d overlaps conventional development ports (3000, 5000, 5173, 8000, 8080, 9000, 9229)", r[0], r[1]))
		}
	}
	return w
}
