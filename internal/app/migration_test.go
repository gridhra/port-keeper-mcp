package app

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// A project the size of a real mid-sized web app: 26 services (three web apps
// with front/edge/inspector each, an API, storybooks, and 12 infra services),
// migrated from a legacy layout where slot 1 keeps its historic numbers.
func bigManifest() string {
	var b strings.Builder
	b.WriteString("[project]\nname = \"crm\"\nblock_size = 64\n")
	app := []string{"a-front", "a-edge", "a-inspector", "b-front", "b-edge", "b-inspector", "c-front", "c-edge", "c-inspector", "media", "media-db", "api", "storybook", "storybook-2"}
	infra := []string{"python", "mysql", "auth", "auth-ui", "store", "store-ui", "smtp", "mail-ui", "trace-ui", "trace-collector", "queue", "queue-ui"}
	for _, s := range app {
		fmt.Fprintf(&b, "[[service]]\nname = %q\nenv = %q\n", s, strings.ToUpper(strings.ReplaceAll(s, "-", "_"))+"_PORT")
	}
	for _, s := range infra {
		fmt.Fprintf(&b, "[[service]]\nname = %q\nenv = %q\ntier = \"infra\"\n", s, strings.ToUpper(strings.ReplaceAll(s, "-", "_"))+"_PORT")
	}
	b.WriteString("[[derive]]\nenv = \"ALLOWED_ORIGINS\"\nvalue = \"${url.a-front},${url.a-edge},${url.b-front}\"\n")
	b.WriteString("[[derive]]\nenv = \"DB_CONTAINER\"\nvalue = \"slot${slot.infra}-db\"\n")
	return b.String()
}

func TestMigrationOfLargeProject(t *testing.T) {
	ctx := context.Background()
	a := newApp(t)
	a.Cfg.Pool.Ranges = [][]int{{20000, 20511}} // room for 8 slots of 64
	dir := project(t, bigManifest())

	// Slot 1 on the legacy layout: pin every service to its historic number.
	c, _ := a.Resolve(ctx, dir, "")
	if _, _, err := a.SlotNew(ctx, c, NewSlotOptions{BindRoot: true}); err != nil {
		t.Fatal(err)
	}
	legacy := map[string]int{"a-front": 5001, "a-edge": 5010, "a-inspector": 9229, "b-front": 5002, "b-edge": 5011, "b-inspector": 9230,
		"c-front": 5013, "c-edge": 5012, "c-inspector": 9231, "media": 5014, "media-db": 5150, "api": 5500, "storybook": 5100, "storybook-2": 5200,
		"python": 5250, "mysql": 3330, "auth": 9099, "auth-ui": 14000, "store": 9002, "store-ui": 9001, "smtp": 1025, "mail-ui": 8025,
		"trace-ui": 16686, "trace-collector": 14268, "queue": 9324, "queue-ui": 9325}
	for svc, port := range legacy {
		if err := a.Pin(ctx, c, svc, port, "legacy layout", false); err != nil {
			t.Fatalf("pin %s: %v", svc, err)
		}
	}
	r1, err := a.Resolved(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	for svc, port := range legacy {
		if r1.Ports[svc] != port {
			t.Fatalf("%s: %d != %d", svc, r1.Ports[svc], port)
		}
	}

	// Slots 2..6, some sharing infra: no collisions, all pooled, all beyond the old limit of 4.
	seen := map[int]string{}
	for svc, p := range r1.Ports {
		seen[p] = svc
	}
	// Ordered: slot 4 shares slot 3's infra, so 3 must exist first.
	plan := []struct{ name, infraFrom string }{{"2", "1"}, {"3", ""}, {"4", "3"}, {"5", ""}, {"6", "1"}}
	for _, step := range plan {
		i, infraFrom := step.name, step.infraFrom
		ci, _ := a.Resolve(ctx, dir, i)
		r, _, err := a.SlotNew(ctx, ci, NewSlotOptions{Name: i, InfraFrom: infraFrom})
		if err != nil {
			t.Fatalf("slot %s: %v", i, err)
		}
		for svc, p := range r.Ports {
			shared := infraFrom != "" && isInfra(svc)
			if owner, dup := seen[p]; dup && !shared {
				t.Fatalf("slot %s/%s got %d already used by %s", i, svc, p, owner)
			}
			if !shared && (p < 20000 || p > 20511) {
				t.Fatalf("slot %s/%s got %d outside the pool", i, svc, p)
			}
			if !shared {
				seen[p] = i + "/" + svc
			}
		}
		if infraFrom != "" && r.InfraSlot != infraFrom {
			t.Fatalf("slot %s infra from %s, want %s", i, r.InfraSlot, infraFrom)
		}
	}
	// Slot 2 shares slot 1's pinned mysql.
	c2, _ := a.Resolve(ctx, dir, "2")
	r2, _ := a.Resolved(ctx, c2)
	if r2.Ports["mysql"] != 3330 {
		t.Fatalf("slot 2 mysql = %d, want the pinned 3330", r2.Ports["mysql"])
	}
	// Another project cannot take a pinned number.
	other := project(t, "[project]\nname = \"other\"\n[[service]]\nname = \"web\"\nenv = \"WEB_PORT\"\n")
	co, _ := a.Resolve(ctx, other, "")
	if _, _, err := a.SlotNew(ctx, co, NewSlotOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := a.Pin(ctx, co, "web", 5010, "mine", false); err == nil || !strings.Contains(err.Error(), "crm/1/a-edge") {
		t.Fatalf("cross-project pin: %v", err)
	}
	// Migration done: unpin everything, slot 1 falls back to its pooled block.
	for svc := range legacy {
		if _, err := a.Unpin(ctx, c, svc); err != nil {
			t.Fatalf("unpin %s: %v", svc, err)
		}
	}
	r1, _ = a.Resolved(ctx, c)
	for svc, p := range r1.Ports {
		if p < 20000 || p > 20063 {
			t.Fatalf("after unpin %s = %d, expected slot 1's block", svc, p)
		}
	}
	if err := a.Pin(ctx, co, "web", 5010, "now free", false); err != nil {
		t.Fatalf("pin after unpin: %v", err)
	}
}

func isInfra(svc string) bool {
	switch svc {
	case "python", "mysql", "auth", "auth-ui", "store", "store-ui", "smtp", "mail-ui", "trace-ui", "trace-collector", "queue", "queue-ui":
		return true
	}
	return false
}
