package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaultsAndErrors(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PORT_KEEPER_CONFIG_DIR", dir)
	cfg, err := Load()
	if err != nil || cfg.Pool.Ranges[0][0] != 20000 || cfg.StaleDays != 30 {
		t.Fatalf("defaults: %+v %v", cfg, err)
	}
	write := func(text string) { os.WriteFile(filepath.Join(dir, "config.toml"), []byte(text), 0o600) }
	write("stale_days = 0\n[pool]\nranges = [[21000, 21999]]\n")
	cfg, err = Load()
	if err != nil || cfg.StaleDays != 0 || cfg.Pool.Capacity() != 1000 {
		t.Fatalf("explicit zero / pool: %+v %v", cfg, err)
	}
	for _, bad := range []string{"stale_days = -1\n", "[pool]\nranges = [[80, 90]]\n", "[pool]\nranges = [[30000, 20000]]\n", "[mcp]\nstale_days = 5\n", "typo = 1\n"} {
		write(bad)
		if _, err := Load(); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestPoolHelpers(t *testing.T) {
	p := Pool{Ranges: [][]int{{20000, 20009}}, DenyPorts: []int{20005, 3000}}
	if p.Capacity() != 9 {
		t.Fatalf("capacity %d (a deny port outside the range must not count)", p.Capacity())
	}
	if !p.Contains(20000) || p.Contains(19999) || !p.Denied(20005) || p.Denied(20006) {
		t.Fatal("contains/denied")
	}
	if w := (Pool{Ranges: [][]int{{3000, 40000}}}).Warnings(); len(w) != 2 {
		t.Fatalf("warnings: %v", w)
	}
}
