package ledger

import (
	"context"
	"math/rand"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/gridhra/port-keeper-mcp/internal/config"
)

func openTest(t *testing.T) *Ledger {
	t.Helper()
	l, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l
}

func pool() config.Pool { return config.Pool{Ranges: [][]int{{20000, 20255}}, DenyPorts: []int{20010}} }

// TestAllocationInvariants: after any sequence of slot creations and releases,
// live blocks never overlap, never leave the pool, never contain a denied port,
// and a slot keeps its block until released.
func TestAllocationInvariants(t *testing.T) {
	ctx := context.Background()
	l := openTest(t)
	rng := rand.New(rand.NewSource(1))
	p := pool()
	type live struct {
		id   int64
		base int
	}
	var slots []live
	next := 0
	for step := 0; step < 300; step++ {
		tx, err := l.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(slots) == 0 || rng.Intn(3) != 0 {
			pr, _ := tx.EnsureProject(ctx, "p")
			next++
			s, err := tx.CreateSlot(ctx, pr.ID, "s"+strconv.Itoa(next), "", nil)
			if err != nil {
				t.Fatal(err)
			}
			size := 1 + rng.Intn(8)
			b, err := tx.AllocateBlock(ctx, s.ID, size, p, nil)
			if err != nil {
				// pool full is acceptable; release something next round
				tx.Rollback()
				if len(slots) == 0 {
					t.Fatal(err)
				}
				continue
			}
			if b.Base < 20000 || b.Base+b.Size-1 > 20255 {
				t.Fatalf("block %d+%d leaves the pool", b.Base, b.Size)
			}
			for q := b.Base; q < b.Base+b.Size; q++ {
				if p.Denied(q) {
					t.Fatalf("block %d+%d contains denied port %d", b.Base, b.Size, q)
				}
			}
			for _, o := range slots {
				ob, _ := tx.Blocks(ctx, o.id)
				for _, x := range ob {
					if x.Base < b.Base+b.Size && b.Base < x.Base+x.Size {
						t.Fatalf("overlap: new %d+%d vs %d+%d", b.Base, b.Size, x.Base, x.Size)
					}
				}
			}
			slots = append(slots, live{s.ID, b.Base})
		} else {
			i := rng.Intn(len(slots))
			bs, _ := tx.Blocks(ctx, slots[i].id)
			if len(bs) != 1 || bs[0].Base != slots[i].base {
				t.Fatalf("slot changed its block: want %d got %+v", slots[i].base, bs)
			}
			if err := tx.DeleteSlot(ctx, slots[i].id); err != nil {
				t.Fatal(err)
			}
			slots = append(slots[:i], slots[i+1:]...)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
}

// TestConcurrentAllocation: many goroutines with their own ledger handles on the
// same file never receive overlapping blocks.
func TestConcurrentAllocation(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	const n = 12
	var wg sync.WaitGroup
	results := make([]int, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			l, err := Open(dir)
			if err != nil {
				errs[i] = err
				return
			}
			defer l.Close()
			tx, err := l.Begin(ctx)
			if err != nil {
				errs[i] = err
				return
			}
			defer tx.Rollback()
			pr, _ := tx.EnsureProject(ctx, "p")
			s, err := tx.CreateSlot(ctx, pr.ID, "s"+strconv.Itoa(i), "", nil)
			if err != nil {
				errs[i] = err
				return
			}
			b, err := tx.AllocateBlock(ctx, s.ID, 4, pool(), nil)
			if err != nil {
				errs[i] = err
				return
			}
			if err := tx.InsertLease(ctx, s.ID, "svc", b.Base, "app", "http"); err != nil {
				errs[i] = err
				return
			}
			errs[i] = tx.Commit()
			results[i] = b.Base
		}(i)
	}
	wg.Wait()
	seen := map[int]bool{}
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: %v", i, errs[i])
		}
		for q := results[i]; q < results[i]+4; q++ {
			if seen[q] {
				t.Fatalf("port %d handed out twice", q)
			}
			seen[q] = true
		}
	}
}

// TestPortUnique: the UNIQUE constraint rejects a second lease of the same port,
// pooled or pinned, across projects.
func TestPortUnique(t *testing.T) {
	ctx := context.Background()
	l := openTest(t)
	tx, _ := l.Begin(ctx)
	a, _ := tx.EnsureProject(ctx, "a")
	b, _ := tx.EnsureProject(ctx, "b")
	sa, _ := tx.CreateSlot(ctx, a.ID, "1", "", nil)
	sb, _ := tx.CreateSlot(ctx, b.ID, "1", "", nil)
	if err := tx.InsertPinnedLease(ctx, sa.ID, "web", 3000, "app", "http", "legacy"); err != nil {
		t.Fatal(err)
	}
	if err := tx.InsertLease(ctx, sb.ID, "web", 3000, "app", "http"); err == nil {
		t.Fatal("second lease of port 3000 was accepted")
	}
	owner, err := tx.OwnerOfPort(ctx, 3000)
	if err != nil || owner == nil || owner.Project != "a" || !owner.Pinned {
		t.Fatalf("owner = %+v, %v", owner, err)
	}
	// A pinned port inside the pool is skipped by the allocator.
	tx2 := tx
	if err := tx2.InsertPinnedLease(ctx, sa.ID, "api", 20001, "app", "http", "legacy"); err != nil {
		t.Fatal(err)
	}
	blk, err := tx2.AllocateBlock(ctx, sb.ID, 4, pool(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if blk.Base <= 20001 && blk.Base+4 > 20001 {
		t.Fatalf("block %d+4 contains pinned 20001", blk.Base)
	}
	_ = tx.Commit()
}
