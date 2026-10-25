// Copyright (c) 2026 Neomantra Corp.

package ntactor

import "math"

// InfluenceMap is a deterministic 2D spatial scalar field used for threat maps,
// tactical control maps, scent/sound propagation, and stigmergic pheromone trails.
//
// Like GridMap, cell (0,0) has its minimum corner at Origin and each cell spans
// CellSize x CellSize in the X-Y plane.
type InfluenceMap struct {
	width    int
	height   int
	cellSize float64
	invCell  float64
	origin   Vec3
	values   []float64
	scratch  []float64
}

// NewInfluenceMap constructs an InfluenceMap of width x height cells initialized to 0.
func NewInfluenceMap(width, height int, cellSize float64, origin Vec3) *InfluenceMap {
	if width <= 0 {
		width = 1
	}
	if height <= 0 {
		height = 1
	}
	if cellSize <= 0 || math.IsNaN(cellSize) || math.IsInf(cellSize, 0) {
		cellSize = 1.0
	}
	n := width * height
	return &InfluenceMap{
		width:    width,
		height:   height,
		cellSize: cellSize,
		invCell:  1.0 / cellSize,
		origin:   origin,
		values:   make([]float64, n),
		scratch:  make([]float64, n),
	}
}

// NewInfluenceMapFromGrid constructs an InfluenceMap matching the dimensions,
// cell size, and origin of grid.
func NewInfluenceMapFromGrid(grid *GridMap) *InfluenceMap {
	if grid == nil {
		return NewInfluenceMap(1, 1, 1.0, Vec3{})
	}
	return NewInfluenceMap(grid.Width(), grid.Height(), grid.CellSize(), grid.Origin())
}

// Width returns the number of grid columns.
func (m *InfluenceMap) Width() int { return m.width }

// Height returns the number of grid rows.
func (m *InfluenceMap) Height() int { return m.height }

// CellSize returns the world-space size of each cell.
func (m *InfluenceMap) CellSize() float64 { return m.cellSize }

// Origin returns the minimum world-space corner of the grid.
func (m *InfluenceMap) Origin() Vec3 { return m.origin }

// Values returns the underlying row-major slice of width*height cell values.
func (m *InfluenceMap) Values() []float64 { return m.values }

// InBounds reports whether cell coordinates (cx, cy) lie inside the grid.
func (m *InfluenceMap) InBounds(cx, cy int) bool {
	return m != nil && cx >= 0 && cx < m.width && cy >= 0 && cy < m.height
}

// WorldToCell maps a world-space Vec3 to cell coordinates (cx, cy).
// It returns ok == false if the position lies outside the grid bounds.
func (m *InfluenceMap) WorldToCell(pos Vec3) (cx, cy int, ok bool) {
	if m == nil {
		return 0, 0, false
	}
	cx = int(math.Floor((pos.X - m.origin.X) * m.invCell))
	cy = int(math.Floor((pos.Y - m.origin.Y) * m.invCell))
	return cx, cy, m.InBounds(cx, cy)
}

// CellToWorld returns the world-space center of cell (cx, cy).
func (m *InfluenceMap) CellToWorld(cx, cy int) Vec3 {
	if m == nil {
		return Vec3{}
	}
	return Vec3{
		X: m.origin.X + (float64(cx)+0.5)*m.cellSize,
		Y: m.origin.Y + (float64(cy)+0.5)*m.cellSize,
		Z: m.origin.Z,
	}
}

// Clear resets all cell values to 0.
func (m *InfluenceMap) Clear() {
	if m == nil {
		return
	}
	clear(m.values)
}

// At returns the scalar influence value at cell (cx, cy), or 0 if out of bounds.
func (m *InfluenceMap) At(cx, cy int) float64 {
	if !m.InBounds(cx, cy) {
		return 0
	}
	return m.values[cy*m.width+cx]
}

// Set sets the scalar influence value at cell (cx, cy) if in bounds and finite.
func (m *InfluenceMap) Set(cx, cy int, val float64) {
	if !m.InBounds(cx, cy) || math.IsNaN(val) || math.IsInf(val, 0) {
		return
	}
	m.values[cy*m.width+cx] = val
}

// Add adds delta to the scalar influence value at cell (cx, cy) if in bounds and finite.
func (m *InfluenceMap) Add(cx, cy int, delta float64) {
	if !m.InBounds(cx, cy) || math.IsNaN(delta) || math.IsInf(delta, 0) {
		return
	}
	m.values[cy*m.width+cx] += delta
}

// AddWorld adds delta to the cell containing world-space position pos.
func (m *InfluenceMap) AddWorld(pos Vec3, delta float64) {
	cx, cy, ok := m.WorldToCell(pos)
	if !ok {
		return
	}
	m.Add(cx, cy, delta)
}

// StampRadius adds a linear radial falloff peakDelta * (1 - dist/radius) to all
// cells whose centers lie within radius of center. If radius <= 0, peakDelta is
// added only to the cell containing center.
func (m *InfluenceMap) StampRadius(center Vec3, radius, peakDelta float64) {
	m.StampRadiusWithLOS(center, radius, peakDelta, nil)
}

// StampRadiusWithLOS adds a linear radial falloff peakDelta * (1 - dist/radius)
// to all cells within radius of center. When grid is non-nil, wall cells and
// cells without line-of-sight from center on grid are skipped.
func (m *InfluenceMap) StampRadiusWithLOS(center Vec3, radius, peakDelta float64, grid *GridMap) {
	if m == nil || math.IsNaN(peakDelta) || math.IsInf(peakDelta, 0) || peakDelta == 0 {
		return
	}
	if radius <= 0 {
		cx, cy, ok := m.WorldToCell(center)
		if ok && (grid == nil || !grid.IsWall(cx, cy)) {
			m.Add(cx, cy, peakDelta)
		}
		return
	}
	minX := int(math.Floor((center.X - radius - m.origin.X) * m.invCell))
	maxX := int(math.Floor((center.X + radius - m.origin.X) * m.invCell))
	minY := int(math.Floor((center.Y - radius - m.origin.Y) * m.invCell))
	maxY := int(math.Floor((center.Y + radius - m.origin.Y) * m.invCell))
	if minX < 0 {
		minX = 0
	}
	if maxX >= m.width {
		maxX = m.width - 1
	}
	if minY < 0 {
		minY = 0
	}
	if maxY >= m.height {
		maxY = m.height - 1
	}
	rSq := radius * radius
	c2D := Vec2(center.X, center.Y)
	for cy := minY; cy <= maxY; cy++ {
		for cx := minX; cx <= maxX; cx++ {
			if grid != nil && grid.IsWall(cx, cy) {
				continue
			}
			cellPos := m.CellToWorld(cx, cy)
			dSq := c2D.DistSq(Vec2(cellPos.X, cellPos.Y))
			if dSq > rSq {
				continue
			}
			if grid != nil && !grid.HasLineOfSight(center, cellPos) {
				continue
			}
			falloff := 1.0 - math.Sqrt(dSq)/radius
			if falloff > 0 {
				m.values[cy*m.width+cx] += peakDelta * falloff
			}
		}
	}
}

// Step advances decay and 4-neighbor spatial diffusion over dt seconds.
//
//   - decayRate (>= 0) is the per-second decay rate; values are multiplied by max(0, 1 - decayRate*dt).
//   - diffusionRate (>= 0) is the per-second neighbor blending rate; each cell blends toward
//     the mean of its passable orthogonal neighbors by clamp(diffusionRate*dt, 0, 1).
func (m *InfluenceMap) Step(dt, decayRate, diffusionRate float64) {
	m.StepWithWalls(dt, decayRate, diffusionRate, nil)
}

// StepWithWalls advances decay and 4-neighbor spatial diffusion over dt seconds,
// preventing influence from diffusing into or through wall cells of grid when
// grid is non-nil.
func (m *InfluenceMap) StepWithWalls(dt, decayRate, diffusionRate float64, grid *GridMap) {
	if m == nil || dt <= 0 || math.IsNaN(dt) || math.IsInf(dt, 0) {
		return
	}
	if decayRate < 0 || math.IsNaN(decayRate) {
		decayRate = 0
	}
	if diffusionRate < 0 || math.IsNaN(diffusionRate) {
		diffusionRate = 0
	}
	blend := diffusionRate * dt
	if blend > 1.0 {
		blend = 1.0
	}
	decay := 1.0 - decayRate*dt
	if decay < 0 {
		decay = 0
	}

	dirs := [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
	for cy := 0; cy < m.height; cy++ {
		row := cy * m.width
		for cx := 0; cx < m.width; cx++ {
			idx := row + cx
			if grid != nil && grid.IsWall(cx, cy) {
				m.scratch[idx] = 0
				continue
			}
			cur := m.values[idx]
			next := cur
			if blend > 0 {
				var sum float64
				var count int
				for _, d := range dirs {
					nx, ny := cx+d[0], cy+d[1]
					if !m.InBounds(nx, ny) {
						continue
					}
					if grid != nil && grid.IsWall(nx, ny) {
						continue
					}
					sum += m.values[ny*m.width+nx]
					count++
				}
				if count > 0 {
					avg := sum / float64(count)
					next = cur*(1.0-blend) + avg*blend
				}
			}
			next *= decay
			if math.Abs(next) < 1e-12 {
				next = 0
			}
			m.scratch[idx] = next
		}
	}
	m.values, m.scratch = m.scratch, m.values
}

// SampleWorld returns the influence value of the cell containing pos, or 0 if
// pos lies outside the grid.
func (m *InfluenceMap) SampleWorld(pos Vec3) float64 {
	cx, cy, ok := m.WorldToCell(pos)
	if !ok {
		return 0
	}
	return m.At(cx, cy)
}

// SampleBilinear returns a bilinearly interpolated influence value at world-space
// position pos across the four nearest cell centers, clamping to grid edges.
func (m *InfluenceMap) SampleBilinear(pos Vec3) float64 {
	if m == nil {
		return 0
	}
	u := (pos.X-m.origin.X)*m.invCell - 0.5
	v := (pos.Y-m.origin.Y)*m.invCell - 0.5
	x0 := int(math.Floor(u))
	y0 := int(math.Floor(v))
	tx := u - float64(x0)
	ty := v - float64(y0)

	clampX := func(x int) int {
		if x < 0 {
			return 0
		}
		if x >= m.width {
			return m.width - 1
		}
		return x
	}
	clampY := func(y int) int {
		if y < 0 {
			return 0
		}
		if y >= m.height {
			return m.height - 1
		}
		return y
	}

	v00 := m.At(clampX(x0), clampY(y0))
	v10 := m.At(clampX(x0+1), clampY(y0))
	v01 := m.At(clampX(x0), clampY(y0+1))
	v11 := m.At(clampX(x0+1), clampY(y0+1))

	top := v00*(1.0-tx) + v10*tx
	bot := v01*(1.0-tx) + v11*tx
	return top*(1.0-ty) + bot*ty
}

// GradientCell computes the 2D spatial gradient vector at cell (cx, cy) pointing
// toward increasing influence using weighted 8-neighbor finite differences.
func (m *InfluenceMap) GradientCell(cx, cy int) Vec3 {
	if !m.InBounds(cx, cy) {
		return Vec3{}
	}
	centerVal := m.At(cx, cy)
	var gx, gy float64
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if dx == 0 && dy == 0 {
				continue
			}
			nx, ny := cx+dx, cy+dy
			if !m.InBounds(nx, ny) {
				continue
			}
			delta := m.At(nx, ny) - centerVal
			weight := 1.0
			if dx != 0 && dy != 0 {
				weight = 0.5 / math.Sqrt2
			}
			gx += float64(dx) * delta * weight
			gy += float64(dy) * delta * weight
		}
	}
	return Vec3{X: gx * m.invCell, Y: gy * m.invCell, Z: 0}
}

// GradientWorld returns the 2D spatial gradient vector at world-space position
// pos pointing toward increasing influence.
func (m *InfluenceMap) GradientWorld(pos Vec3) Vec3 {
	cx, cy, ok := m.WorldToCell(pos)
	if !ok {
		return Vec3{}
	}
	return m.GradientCell(cx, cy)
}

// PeakInRadius scans all cells within radius of center in deterministic (cy, cx)
// order and returns the world-space cell center and value of the highest cell
// (when seekHigh is true) or lowest cell (when seekHigh is false).
func (m *InfluenceMap) PeakInRadius(center Vec3, radius float64, seekHigh bool) (bestPos Vec3, bestVal float64, ok bool) {
	if m == nil || radius <= 0 {
		return Vec3{}, 0, false
	}
	minX := int(math.Floor((center.X - radius - m.origin.X) * m.invCell))
	maxX := int(math.Floor((center.X + radius - m.origin.X) * m.invCell))
	minY := int(math.Floor((center.Y - radius - m.origin.Y) * m.invCell))
	maxY := int(math.Floor((center.Y + radius - m.origin.Y) * m.invCell))
	if minX < 0 {
		minX = 0
	}
	if maxX >= m.width {
		maxX = m.width - 1
	}
	if minY < 0 {
		minY = 0
	}
	if maxY >= m.height {
		maxY = m.height - 1
	}
	rSq := radius * radius
	c2D := Vec2(center.X, center.Y)
	for cy := minY; cy <= maxY; cy++ {
		for cx := minX; cx <= maxX; cx++ {
			cellPos := m.CellToWorld(cx, cy)
			if c2D.DistSq(Vec2(cellPos.X, cellPos.Y)) > rSq {
				continue
			}
			val := m.values[cy*m.width+cx]
			if !ok || (seekHigh && val > bestVal) || (!seekHigh && val < bestVal) {
				bestVal = val
				bestPos = cellPos
				ok = true
			}
		}
	}
	return bestPos, bestVal, ok
}

// SteerInfluence returns a desired velocity of magnitude maxSpeed that steers
// self up the influence gradient (when seekHigh is true, e.g. following a
// pheromone trail) or down the influence gradient (when seekHigh is false, e.g.
// fleeing a threat zone). It returns a zero Vec3 if the local field is flat.
func SteerInfluence(self *Actor, im *InfluenceMap, maxSpeed float64, seekHigh bool) Vec3 {
	if self == nil || im == nil || maxSpeed <= 0 {
		return Vec3{}
	}
	grad := im.GradientWorld(self.Position)
	if !seekHigh {
		grad = grad.Scale(-1)
	}
	if grad.LenSq() > 1e-12 {
		return grad.Normalize().Scale(maxSpeed)
	}
	// Fallback: check a 3-cell neighborhood for a strictly better cell, or flee
	// the current cell center if sitting on a symmetric threat peak.
	curVal := im.SampleWorld(self.Position)
	if peakPos, peakVal, ok := im.PeakInRadius(self.Position, im.CellSize()*3.0, seekHigh); ok {
		if (seekHigh && peakVal > curVal+1e-9) || (!seekHigh && peakVal < curVal-1e-9) {
			return SteerSeek(self.Position, peakPos, maxSpeed)
		}
	}
	if !seekHigh && curVal > 0 {
		if cx, cy, ok := im.WorldToCell(self.Position); ok {
			away := self.Position.Sub(im.CellToWorld(cx, cy))
			if away.LenSq() > 1e-12 {
				return away.Normalize().Scale(maxSpeed)
			}
			return Vec2(maxSpeed, 0)
		}
	}
	return Vec3{}
}

// ApplyCostToGridMap maps each cell's influence value into grid's traversal cost
// multiplier as:
//
//	cost = max(1.0, baseCost + weight * im.At(cx, cy))
//
// Use weight > 0 so GridMap A* (FindWorldPath) and FlowField Dijkstra routing
// automatically detour around high-threat danger zones, or weight < 0 with
// baseCost > 1.0 to make A* prefer high-pheromone corridors.
func (m *InfluenceMap) ApplyCostToGridMap(grid *GridMap, baseCost, weight float64) {
	if m == nil || grid == nil {
		return
	}
	if baseCost < 1.0 || math.IsNaN(baseCost) || math.IsInf(baseCost, 0) {
		baseCost = 1.0
	}
	w := m.width
	if grid.Width() < w {
		w = grid.Width()
	}
	h := m.height
	if grid.Height() < h {
		h = grid.Height()
	}
	for cy := 0; cy < h; cy++ {
		for cx := 0; cx < w; cx++ {
			val := m.values[cy*m.width+cx]
			cost := baseCost + weight*val
			if cost < 1.0 {
				cost = 1.0
			}
			grid.SetCost(cx, cy, cost)
		}
	}
}

// Hash returns a deterministic 64-bit FNV-1a digest of the InfluenceMap's
// geometry and cell values.
func (m *InfluenceMap) Hash() uint64 {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	var h uint64 = offset64
	if m == nil {
		return h
	}
	mixU := func(v uint64) {
		for i := 0; i < 8; i++ {
			h ^= uint64(byte(v >> (8 * i)))
			h *= prime64
		}
	}
	mixU(uint64(m.width))
	mixU(uint64(m.height))
	mixU(math.Float64bits(m.cellSize))
	mixU(math.Float64bits(m.origin.X))
	mixU(math.Float64bits(m.origin.Y))
	mixU(math.Float64bits(m.origin.Z))
	for _, v := range m.values {
		mixU(math.Float64bits(v))
	}
	return h
}
