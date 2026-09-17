package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gridhra/port-keeper-mcp/internal/manifest"
)

const sample = `
[project]
name = "shop"

[[service]]
name = "web"
env = "WEB_PORT"

[[service]]
name = "api"
env = "API_PORT"
proto = "http"

[[service]]
name = "db"
env = "DB_PORT"
proto = "tcp"
tier = "infra"

[[derive]]
env = "VITE_API_BASE"
value = "${url.api}/v1"

[[derive]]
env = "DB_CONTAINER"
value = "${project}-slot${slot}-db it's"
`

func resolved() (*manifest.Manifest, *Resolved) {
	m, err := manifest.Parse(sample)
	if err != nil {
		panic(err)
	}
	return m, &Resolved{Project: "shop", Slot: "2", InfraSlot: "1", BlockBase: 20032, Host: "localhost",
		Ports: map[string]int{"web": 20032, "api": 20033, "db": 20002}}
}

func TestFormatsGolden(t *testing.T) {
	m, r := resolved()
	vars, err := Vars(m, r)
	if err != nil {
		t.Fatal(err)
	}
	export := "export WEB_PORT=20032\nexport API_PORT=20033\nexport DB_PORT=20002\nexport VITE_API_BASE=http://localhost:20033/v1\nexport DB_CONTAINER='shop-slot2-db it'\\''s'\nexport PORT_KEEPER_PROJECT=shop\nexport PORT_KEEPER_SLOT=2\n"
	want := map[string]string{
		"direnv":     export,
		"claude-env": export,
		"json": `{
  "project": "shop",
  "slot": "2",
  "services": {
    "api": {
      "port": 20033,
      "url": "http://localhost:20033"
    },
    "db": {
      "port": 20002
    },
    "web": {
      "port": 20032,
      "url": "http://localhost:20032"
    }
  },
  "env": {
    "API_PORT": "20033",
    "DB_CONTAINER": "shop-slot2-db it's",
    "DB_PORT": "20002",
    "PORT_KEEPER_PROJECT": "shop",
    "PORT_KEEPER_SLOT": "2",
    "VITE_API_BASE": "http://localhost:20033/v1",
    "WEB_PORT": "20032"
  }
}
`,
		"dotenv": "WEB_PORT=20032\nAPI_PORT=20033\nDB_PORT=20002\nVITE_API_BASE=http://localhost:20033/v1\nDB_CONTAINER=\"shop-slot2-db it's\"\nPORT_KEEPER_PROJECT=shop\nPORT_KEEPER_SLOT=2\n",
		"export": "export WEB_PORT=20032\nexport API_PORT=20033\nexport DB_PORT=20002\nexport VITE_API_BASE=http://localhost:20033/v1\nexport DB_CONTAINER='shop-slot2-db it'\\''s'\nexport PORT_KEEPER_PROJECT=shop\nexport PORT_KEEPER_SLOT=2\n",
		"mise":   "[env]\nWEB_PORT = \"20032\"\nAPI_PORT = \"20033\"\nDB_PORT = \"20002\"\nVITE_API_BASE = \"http://localhost:20033/v1\"\nDB_CONTAINER = \"shop-slot2-db it's\"\nPORT_KEEPER_PROJECT = \"shop\"\nPORT_KEEPER_SLOT = \"2\"\n",
	}
	for format, w := range want {
		got, err := Format(format, m, r, vars)
		if err != nil {
			t.Fatal(err)
		}
		if got != w {
			t.Errorf("%s:\n got %q\nwant %q", format, got, w)
		}
	}
	js, err := Format("json", m, r, vars)
	if err != nil {
		t.Fatal(err)
	}
	for _, needle := range []string{`"url": "http://localhost:20033"`, `"port": 20002`, `"PORT_KEEPER_SLOT": "2"`} {
		if !strings.Contains(js, needle) {
			t.Errorf("json lacks %s:\n%s", needle, js)
		}
	}
	if strings.Contains(js, `"url": "tcp`) {
		t.Errorf("tcp service must have no url")
	}
	if _, err := Format("yaml", m, r, vars); err == nil {
		t.Error("unknown format accepted")
	}
}

func TestExpandErrors(t *testing.T) {
	m, r := resolved()
	for _, tmpl := range []string{"${port.nope}", "${url.db}", "${bogus}"} {
		if _, err := Expand(tmpl, m, r); err == nil {
			t.Errorf("%s: expected error", tmpl)
		}
	}
	got, err := Expand("${slot.infra}/${block.base}", m, r)
	if err != nil || got != "1/20032" {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestWriteDotenvIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env.local")
	if err := os.WriteFile(path, []byte("SECRET=keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := WriteDotenv(path, "port-keeper", "shop", "1", "A=1\n")
	if err != nil || !changed {
		t.Fatalf("first write: %v %v", changed, err)
	}
	changed, err = WriteDotenv(path, "port-keeper", "shop", "1", "A=1\n")
	if err != nil || changed {
		t.Fatalf("second write should be a no-op: %v %v", changed, err)
	}
	changed, err = WriteDotenv(path, "port-keeper", "shop", "2", "A=2\nB=3\n")
	if err != nil || !changed {
		t.Fatalf("third write: %v %v", changed, err)
	}
	b, _ := os.ReadFile(path)
	got := string(b)
	want := "SECRET=keep\n\n# >>> port-keeper: shop/2 >>>\nA=2\nB=3\n# <<< port-keeper <<<\n"
	if got != want {
		t.Errorf("\n got %q\nwant %q", got, want)
	}
	if strings.Count(got, "# >>> ") != 1 {
		t.Errorf("block duplicated:\n%s", got)
	}
	st, _ := os.Stat(path)
	if st.Mode().Perm() != 0o600 {
		t.Errorf("mode changed to %04o", st.Mode().Perm())
	}
}

func TestNewlinesNeverSplitLines(t *testing.T) {
	m, err := manifest.Parse(sample)
	if err != nil {
		t.Fatal(err)
	}
	r := &Resolved{Project: "shop", Slot: "1", InfraSlot: "1", Host: "localhost", Ports: map[string]int{"web": 20000, "api": 20001, "db": 20002}}
	vars := []KV{{"EVIL", "a\nINJECTED=1"}, {"OK", "x"}}
	for _, format := range []string{"dotenv", "export", "mise"} {
		out, err := Format(format, m, r, vars)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			if strings.HasPrefix(line, "INJECTED") {
				t.Fatalf("%s: injected line:\n%s", format, out)
			}
		}
	}
}
