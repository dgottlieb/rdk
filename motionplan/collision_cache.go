package motionplan

import (
	"sync"
	"sync/atomic"
)

// CollisionCache holds planner-level temporal-coherence state for collision
// queries within a single planning request. Owned by planContext and threaded
// down through the constraint checker. Holds two pieces of state:
//
//  1. Geometry-pair "last-violated" hints — one slot per constraint type. When
//     a constraint check finds (geomA, geomB) in collision, it stores that pair
//     so the next call to the same constraint tries that pair first.
//  2. Edge-result memoization for checkPath — RRT-Connect rewire and path
//     smoothing re-check the same interpolated edges repeatedly; the verdict
//     for collision-free edges is cached here.
//
// Per-mesh witness caches (the inner-loop temporal-coherence short-circuit)
// live on spatialmath.Mesh.state, not here. Threading a cache through the
// motionplan call chain was measurably slower than direct field access on
// the mesh.
type CollisionCache struct {
	// obstaclePairHint, selfPairHint, robotPairHint each cache the
	// most-recently-violated geometry-label pair for one constraint type.
	// Lock-free reads via atomic; stale reads are harmless.
	obstaclePairHint atomic.Pointer[[2]string]
	selfPairHint     atomic.Pointer[[2]string]
	robotPairHint    atomic.Pointer[[2]string]

	// edgeResults memoizes the outcome of CheckStateConstraintsAcrossSegmentFS
	// for an interpolated edge. Key is the canonical {hashA, hashB} pair —
	// uint64 fits inside sync.Map's interface{} slot without allocation.
	edgeResults sync.Map // edgeResultKey -> edgeResultValue

	edgeHits     atomic.Uint64
	edgeMisses   atomic.Uint64
	edgeStores   atomic.Uint64
}

// CollisionCacheStats is a snapshot of cache utilization for reporting.
type CollisionCacheStats struct {
	EdgeEntries  int
	EdgeHits     uint64
	EdgeMisses   uint64
	EdgeStores   uint64
	// EdgeBytesApprox is a rough estimate of bytes held by edgeResults entries
	// (key + value only, not sync.Map overhead).
	EdgeBytesApprox uint64
}

// HitRate returns EdgeHits / (EdgeHits + EdgeMisses), or 0 if no lookups.
func (s CollisionCacheStats) HitRate() float64 {
	total := s.EdgeHits + s.EdgeMisses
	if total == 0 {
		return 0
	}
	return float64(s.EdgeHits) / float64(total)
}

// Stats returns a snapshot of cache utilization. Walks the sync.Map to count
// entries — O(n) but only called for reporting.
func (c *CollisionCache) Stats() CollisionCacheStats {
	if c == nil {
		return CollisionCacheStats{}
	}
	entries := 0
	c.edgeResults.Range(func(_, _ any) bool {
		entries++
		return true
	})
	// edgeResultKey is 16 bytes (two uint64); edgeResultValue is 1 byte but
	// padded to 8. Add ~32 bytes for sync.Map's per-entry overhead.
	const bytesPerEntry = 16 + 8 + 32
	return CollisionCacheStats{
		EdgeEntries:     entries,
		EdgeHits:        c.edgeHits.Load(),
		EdgeMisses:      c.edgeMisses.Load(),
		EdgeStores:      c.edgeStores.Load(),
		EdgeBytesApprox: uint64(entries) * bytesPerEntry,
	}
}

// NewCollisionCache constructs an empty cache. Safe for concurrent use.
func NewCollisionCache() *CollisionCache {
	return &CollisionCache{}
}

// edgeResultKey identifies an interpolated edge by hashed-config endpoints.
// Symmetric (edges are bidirectional) — sorting the hashes canonicalizes the key.
type edgeResultKey struct {
	a, b uint64
}

// edgeResultValue records whether an edge was found collision-free. Only clear
// results are cached — failed-edge results would need to be keyed by the buffer
// and resolution used at the time, which varies across callers.
type edgeResultValue struct {
	isClear bool
}

// LookupEdgeResult returns whether the edge between two configurations has been
// previously verified collision-free. Returns (false, false) for "no cached result".
// Caller hashes must be deterministic for the same inputs across calls.
func (c *CollisionCache) LookupEdgeResult(hashA, hashB uint64) (isClear, ok bool) {
	if c == nil {
		return false, false
	}
	if hashA > hashB {
		hashA, hashB = hashB, hashA
	}
	v, ok := c.edgeResults.Load(edgeResultKey{a: hashA, b: hashB})
	if !ok {
		c.edgeMisses.Add(1)
		return false, false
	}
	c.edgeHits.Add(1)
	return v.(edgeResultValue).isClear, true
}

// StoreEdgeResult records that an edge was found collision-free.
func (c *CollisionCache) StoreEdgeResult(hashA, hashB uint64, isClear bool) {
	if c == nil {
		return
	}
	if hashA > hashB {
		hashA, hashB = hashB, hashA
	}
	c.edgeResults.Store(edgeResultKey{a: hashA, b: hashB}, edgeResultValue{isClear: isClear})
	c.edgeStores.Add(1)
}
