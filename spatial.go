// Copyright (c) 2026 Neomantra Corp.

package ntactor

import (
	"math"
	"slices"
)

type cellKey struct {
	x, y, z int64
}

// GridIndex is a uniform 2D/3D spatial hash grid that implements SpatialIndex.
// It accelerates ActorsWithinRadius, Neighbors, and ActorsWithinCone queries
// for large actor counts while preserving deterministic ActorID ordering.
type GridIndex struct {
	cellSize float64
	invCell  float64
	cells    map[cellKey][]*Actor
}

// NewGridIndex constructs a uniform spatial hash grid with the given cellSize.
// If cellSize <= 0, a default cell size of 4.0 is used.
func NewGridIndex(cellSize float64) *GridIndex {
	if cellSize <= 0 || math.IsNaN(cellSize) || math.IsInf(cellSize, 0) {
		cellSize = 4.0
	}
	return &GridIndex{
		cellSize: cellSize,
		invCell:  1.0 / cellSize,
		cells:    make(map[cellKey][]*Actor),
	}
}

// CellSize returns the spatial grid's cell dimension.
func (g *GridIndex) CellSize() float64 {
	return g.cellSize
}

func (g *GridIndex) keyFor(p Vec3) cellKey {
	return cellKey{
		x: int64(math.Floor(p.X * g.invCell)),
		y: int64(math.Floor(p.Y * g.invCell)),
		z: int64(math.Floor(p.Z * g.invCell)),
	}
}

// Rebuild populates the spatial grid from the active actors slice.
func (g *GridIndex) Rebuild(actors []*Actor) {
	for k, bucket := range g.cells {
		if len(bucket) == 0 {
			delete(g.cells, k)
			continue
		}
		clear(bucket)
		g.cells[k] = bucket[:0]
	}
	for _, a := range actors {
		if a == nil || a.destroyed {
			continue
		}
		k := g.keyFor(a.Position)
		g.cells[k] = append(g.cells[k], a)
	}
}

// QueryRadius appends to dst all active Actors within radius (inclusive) of
// center, sorted in deterministic ascending ActorID order.
func (g *GridIndex) QueryRadius(center Vec3, radius float64, dst []*Actor) []*Actor {
	if radius < 0 || len(g.cells) == 0 {
		return dst
	}
	minX := int64(math.Floor((center.X - radius) * g.invCell))
	maxX := int64(math.Floor((center.X + radius) * g.invCell))
	minY := int64(math.Floor((center.Y - radius) * g.invCell))
	maxY := int64(math.Floor((center.Y + radius) * g.invCell))
	minZ := int64(math.Floor((center.Z - radius) * g.invCell))
	maxZ := int64(math.Floor((center.Z + radius) * g.invCell))

	startIdx := len(dst)
	rSq := radius * radius

	for cz := minZ; cz <= maxZ; cz++ {
		for cy := minY; cy <= maxY; cy++ {
			for cx := minX; cx <= maxX; cx++ {
				bucket := g.cells[cellKey{x: cx, y: cy, z: cz}]
				for _, a := range bucket {
					if !a.destroyed && a.Position.DistSq(center) <= rSq {
						dst = append(dst, a)
					}
				}
			}
		}
	}

	added := dst[startIdx:]
	if len(added) > 1 {
		slices.SortFunc(added, func(a, b *Actor) int {
			if a.ID < b.ID {
				return -1
			}
			if a.ID > b.ID {
				return 1
			}
			return 0
		})
	}
	return dst
}
