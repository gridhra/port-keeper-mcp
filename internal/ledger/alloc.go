package ledger

import (
	"context"
	"fmt"
	"sort"

	"github.com/gridhra/port-keeper-mcp/internal/config"
)

// Probe reports whether a port is currently taken by a process outside the ledger.
type Probe func(port int) bool

// AllocateBlock finds the first block of size ports that lies inside the pool,
// overlaps no existing block, contains no leased or denied port, and (when
// probe is non-nil) has no listener on any of its ports. It records the block.
func (t *Tx) AllocateBlock(ctx context.Context, slotID int64, size int, pool config.Pool, probe Probe) (*Block, error) {
	blocks, err := t.AllBlocks(ctx)
	if err != nil {
		return nil, err
	}
	leased, err := t.AllLeasedPorts(ctx)
	if err != nil {
		return nil, err
	}
	ranges := append([][]int(nil), pool.Ranges...)
	sort.Slice(ranges, func(i, j int) bool { return ranges[i][0] < ranges[j][0] })
	for _, r := range ranges {
		base := r[0]
		for base+size-1 <= r[1] {
			next, ok := candidate(base, size, blocks, leased, pool, probe)
			if ok {
				return t.InsertBlock(ctx, slotID, base, size)
			}
			base = next
		}
	}
	return nil, fmt.Errorf("no free block of %d ports in the pool (%d ports total); release stale slots with `port-keeper gc` or widen [pool] in %s", size, pool.Capacity(), config.Path())
}

// candidate checks [base, base+size). On rejection it returns the next base worth trying.
func candidate(base, size int, blocks []Block, leased map[int]bool, pool config.Pool, probe Probe) (int, bool) {
	end := base + size // exclusive
	for _, b := range blocks {
		bend := b.Base + b.Size
		if b.Base < end && base < bend {
			return bend, false
		}
	}
	for p := base; p < end; p++ {
		if leased[p] || pool.Denied(p) {
			return p + 1, false
		}
	}
	if probe != nil {
		for p := base; p < end; p++ {
			if probe(p) {
				return p + 1, false
			}
		}
	}
	return end, true
}

// FreeOffsets returns the ports of the slot's blocks that carry no lease, in order.
func (t *Tx) FreeOffsets(ctx context.Context, slotID int64) ([]int, error) {
	blocks, err := t.Blocks(ctx, slotID)
	if err != nil {
		return nil, err
	}
	leases, err := t.Leases(ctx, slotID)
	if err != nil {
		return nil, err
	}
	used := map[int]bool{}
	for _, l := range leases {
		used[l.Port] = true
	}
	var out []int
	for _, b := range blocks {
		for p := b.Base; p < b.Base+b.Size; p++ {
			if !used[p] {
				out = append(out, p)
			}
		}
	}
	return out, nil
}
