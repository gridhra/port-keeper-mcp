package manifest

import (
	"strings"
	"testing"
)

func TestSkeletonParses(t *testing.T) {
	m, err := Parse(Skeleton("shop"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Project.BlockSize != 32 || m.Project.SlotDefault != "1" || m.Render.DotenvPath != ".env.local" || m.Render.Host != "localhost" {
		t.Errorf("defaults not applied: %+v", m)
	}
	if strings.Contains(Skeleton("shop"), "2000") {
		t.Error("skeleton must not contain port numbers")
	}
}

func TestValidation(t *testing.T) {
	bad := map[string]string{
		"dup service":  "[project]\nname='a'\n[[service]]\nname='x'\nenv='X'\n[[service]]\nname='x'\nenv='Y'\n",
		"dup env":      "[project]\nname='a'\n[[service]]\nname='x'\nenv='X'\n[[service]]\nname='y'\nenv='X'\n",
		"bad proto":    "[project]\nname='a'\n[[service]]\nname='x'\nenv='X'\nproto='udp'\n",
		"bad name":     "[project]\nname='A B'\n[[service]]\nname='x'\nenv='X'\n",
		"no services":  "[project]\nname='a'\n",
		"unknown key":  "[project]\nname='a'\nport=3000\n[[service]]\nname='x'\nenv='X'\n",
		"abs dotenv":   "[project]\nname='a'\n[[service]]\nname='x'\nenv='X'\n[render]\ndotenv_path='/etc/env'\n",
		"derive clash": "[project]\nname='a'\n[[service]]\nname='x'\nenv='X'\n[[derive]]\nenv='X'\nvalue='1'\n",
		"remote host":  "[project]\nname='a'\n[[service]]\nname='x'\nenv='X'\n[render]\nhost='evil.example.com'\n",
		"host w/ path": "[project]\nname='a'\n[[service]]\nname='x'\nenv='X'\n[render]\nhost='x.localhost/evil'\n",
		"multiline":    "[project]\nname='a'\n[[service]]\nname='x'\nenv='X'\n[[derive]]\nenv='D'\nvalue=\"a\\nB=1\"\n",
	}
	for name, text := range bad {
		if _, err := Parse(text); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestLoopbackHosts(t *testing.T) {
	for _, ok := range []string{"localhost", "127.0.0.1", "::1", "[::1]", "shop.localhost", "Admin.Shop.LOCALHOST"} {
		if !LoopbackHost(ok) {
			t.Errorf("%s should be accepted", ok)
		}
	}
	for _, bad := range []string{"example.com", "localhost.example.com", "10.0.0.1", "a.localhost:80", ""} {
		if LoopbackHost(bad) {
			t.Errorf("%s should be rejected", bad)
		}
	}
}
