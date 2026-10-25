// Copyright (c) 2026 Neomantra Corp.

package ntactor

import "math"

// NodeID identifies a vertex in a NavGraph.
type NodeID int

// InvalidNodeID represents an out-of-bounds or non-existent graph node.
const InvalidNodeID NodeID = -1

// DefaultMaxExpansions is the default node-expansion cap for FindPathNodes when
// maxExpansions <= 0.
const DefaultMaxExpansions = 65536

// NavGraph defines an abstract navigation graph for deterministic A* search.
type NavGraph interface {
	// Neighbors appends reachable neighbor NodeIDs from node to dst and returns the slice.
	Neighbors(node NodeID, dst []NodeID) []NodeID
	// Cost returns the non-negative traversal cost of moving from a to b.
	Cost(a, b NodeID) float64
	// Heuristic returns the estimated remaining cost from node to goal.
	Heuristic(node, goal NodeID) float64
	// Position returns the abstract world-space coordinate of node.
	Position(node NodeID) Vec3
}

type astarEntry struct {
	node NodeID
	f    float64
	g    float64
}

// astarLess orders open-set entries deterministically:
// 1. Lowest f = g + h
// 2. Highest g (tie-break toward nodes further along the path)
// 3. Lowest NodeID (strictly deterministic tie-breaker)
func astarLess(a, b astarEntry) bool {
	diffF := a.f - b.f
	if math.Abs(diffF) > 1e-12 {
		return diffF < 0
	}
	diffG := a.g - b.g
	if math.Abs(diffG) > 1e-12 {
		return diffG > 0
	}
	return a.node < b.node
}

type astarHeap []astarEntry

func (h *astarHeap) push(e astarEntry) {
	*h = append(*h, e)
	i := len(*h) - 1
	for i > 0 {
		parent := (i - 1) / 2
		if !astarLess((*h)[i], (*h)[parent]) {
			break
		}
		(*h)[i], (*h)[parent] = (*h)[parent], (*h)[i]
		i = parent
	}
}

func (h *astarHeap) pop() astarEntry {
	old := *h
	n := len(old)
	top := old[0]
	last := old[n-1]
	*h = old[:n-1]
	if n-1 > 0 {
		(*h)[0] = last
		i := 0
		for {
			left := 2*i + 1
			if left >= len(*h) {
				break
			}
			smallest := left
			if right := left + 1; right < len(*h) && astarLess((*h)[right], (*h)[left]) {
				smallest = right
			}
			if !astarLess((*h)[smallest], (*h)[i]) {
				break
			}
			(*h)[i], (*h)[smallest] = (*h)[smallest], (*h)[i]
			i = smallest
		}
	}
	return top
}

// FindPathNodes computes a deterministic shortest path of NodeIDs from start to
// goal on g using A*. If maxExpansions <= 0, DefaultMaxExpansions is used.
// It returns (nil, false) if no path exists within maxExpansions.
func FindPathNodes(g NavGraph, start, goal NodeID, maxExpansions int) ([]NodeID, bool) {
	if g == nil || start < 0 || goal < 0 {
		return nil, false
	}
	if start == goal {
		return []NodeID{start}, true
	}
	if maxExpansions <= 0 {
		maxExpansions = DefaultMaxExpansions
	}

	gScore := map[NodeID]float64{start: 0}
	cameFrom := make(map[NodeID]NodeID)
	closed := make(map[NodeID]bool)

	var open astarHeap
	open.push(astarEntry{
		node: start,
		f:    g.Heuristic(start, goal),
		g:    0,
	})

	var neighbors []NodeID
	expansions := 0

	for len(open) > 0 {
		cur := open.pop()
		if closed[cur.node] {
			continue
		}
		if cur.node == goal {
			return reconstructNodePath(cameFrom, start, goal), true
		}
		closed[cur.node] = true
		expansions++
		if expansions > maxExpansions {
			return nil, false
		}

		neighbors = g.Neighbors(cur.node, neighbors[:0])
		for _, next := range neighbors {
			if next < 0 || closed[next] {
				continue
			}
			stepCost := g.Cost(cur.node, next)
			if stepCost < 0 || math.IsNaN(stepCost) || math.IsInf(stepCost, 0) {
				continue
			}
			tentativeG := cur.g + stepCost
			prevG, seen := gScore[next]
			if !seen || tentativeG+1e-12 < prevG {
				gScore[next] = tentativeG
				cameFrom[next] = cur.node
				h := g.Heuristic(next, goal)
				open.push(astarEntry{
					node: next,
					f:    tentativeG + h,
					g:    tentativeG,
				})
			}
		}
	}
	return nil, false
}

func reconstructNodePath(cameFrom map[NodeID]NodeID, start, goal NodeID) []NodeID {
	var rev []NodeID
	for cur := goal; ; {
		rev = append(rev, cur)
		if cur == start {
			break
		}
		prev, ok := cameFrom[cur]
		if !ok {
			break
		}
		cur = prev
	}
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev
}

// FindPath computes a deterministic path of world-space Vec3 waypoints from
// start to goal on g using A*.
func FindPath(g NavGraph, start, goal NodeID, maxExpansions int) ([]Vec3, bool) {
	nodes, ok := FindPathNodes(g, start, goal, maxExpansions)
	if !ok {
		return nil, false
	}
	waypoints := make([]Vec3, len(nodes))
	for i, id := range nodes {
		waypoints[i] = g.Position(id)
	}
	return waypoints, true
}

// GridMap is a 2D occupancy and weighted-cost navigation grid that implements
// NavGraph and provides world/cell coordinate mapping, line-of-sight checks,
// and string-pulling path smoothing.
type GridMap struct {
	width         int
	height        int
	cellSize      float64
	invCell       float64
	origin        Vec3
	AllowDiagonal bool
	blocked       []bool
	costs         []float64
}

// NewGridMap constructs a GridMap of width x height cells, where cell (0,0) has
// its minimum corner at origin and each cell has dimension cellSize x cellSize.
// By default all cells are passable with cost multiplier 1.0 and AllowDiagonal is true.
func NewGridMap(width, height int, cellSize float64, origin Vec3) *GridMap {
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
	costs := make([]float64, n)
	for i := range costs {
		costs[i] = 1.0
	}
	return &GridMap{
		width:         width,
		height:        height,
		cellSize:      cellSize,
		invCell:       1.0 / cellSize,
		origin:        origin,
		AllowDiagonal: true,
		blocked:       make([]bool, n),
		costs:         costs,
	}
}

// Width returns the number of grid columns.
func (m *GridMap) Width() int { return m.width }

// Height returns the number of grid rows.
func (m *GridMap) Height() int { return m.height }

// CellSize returns the world-space size of each grid cell.
func (m *GridMap) CellSize() float64 { return m.cellSize }

// Origin returns the minimum world-space corner (X, Y) of the grid.
func (m *GridMap) Origin() Vec3 { return m.origin }

// InBounds reports whether cell coordinates (cx, cy) lie inside the grid.
func (m *GridMap) InBounds(cx, cy int) bool {
	return cx >= 0 && cx < m.width && cy >= 0 && cy < m.height
}

// NodeID converts cell coordinates (cx, cy) into a NodeID, or returns
// InvalidNodeID if out of bounds.
func (m *GridMap) NodeID(cx, cy int) NodeID {
	if !m.InBounds(cx, cy) {
		return InvalidNodeID
	}
	return NodeID(cy*m.width + cx)
}

// Coords converts a NodeID back into cell coordinates (cx, cy).
func (m *GridMap) Coords(id NodeID) (cx, cy int) {
	if id < 0 || int(id) >= len(m.blocked) {
		return -1, -1
	}
	return int(id) % m.width, int(id) / m.width
}

// SetWall marks cell (cx, cy) as blocked or passable.
func (m *GridMap) SetWall(cx, cy int, blocked bool) {
	if m.InBounds(cx, cy) {
		m.blocked[cy*m.width+cx] = blocked
	}
}

// SetRectWall marks all cells in the inclusive rectangle [x0..x1, y0..y1] as
// blocked or passable.
func (m *GridMap) SetRectWall(x0, y0, x1, y1 int, blocked bool) {
	if x0 > x1 {
		x0, x1 = x1, x0
	}
	if y0 > y1 {
		y0, y1 = y1, y0
	}
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			m.SetWall(x, y, blocked)
		}
	}
}

// IsWall reports whether cell (cx, cy) is out of bounds or blocked.
func (m *GridMap) IsWall(cx, cy int) bool {
	if !m.InBounds(cx, cy) {
		return true
	}
	return m.blocked[cy*m.width+cx]
}

// SetCost sets the traversal cost multiplier (>= 1.0) for cell (cx, cy).
func (m *GridMap) SetCost(cx, cy int, costMultiplier float64) {
	if !m.InBounds(cx, cy) {
		return
	}
	if costMultiplier < 1.0 || math.IsNaN(costMultiplier) || math.IsInf(costMultiplier, 0) {
		costMultiplier = 1.0
	}
	m.costs[cy*m.width+cx] = costMultiplier
}

// CellCost returns the traversal cost multiplier of cell (cx, cy).
func (m *GridMap) CellCost(cx, cy int) float64 {
	if !m.InBounds(cx, cy) {
		return math.Inf(1)
	}
	return m.costs[cy*m.width+cx]
}

// WorldToCell maps a world-space Vec3 to cell coordinates (cx, cy).
// It returns ok == false if the position lies outside the grid bounds.
func (m *GridMap) WorldToCell(pos Vec3) (cx, cy int, ok bool) {
	cx = int(math.Floor((pos.X - m.origin.X) * m.invCell))
	cy = int(math.Floor((pos.Y - m.origin.Y) * m.invCell))
	return cx, cy, m.InBounds(cx, cy)
}

// CellToWorld returns the world-space center of cell (cx, cy).
func (m *GridMap) CellToWorld(cx, cy int) Vec3 {
	return Vec3{
		X: m.origin.X + (float64(cx)+0.5)*m.cellSize,
		Y: m.origin.Y + (float64(cy)+0.5)*m.cellSize,
		Z: m.origin.Z,
	}
}

// Position returns the world-space center of node.
func (m *GridMap) Position(node NodeID) Vec3 {
	cx, cy := m.Coords(node)
	return m.CellToWorld(cx, cy)
}

// Neighbors appends passable adjacent NodeIDs to dst in deterministic order.
// When AllowDiagonal is true, diagonal steps are permitted only when both
// orthogonal adjacent cells are also passable (preventing corner clipping).
func (m *GridMap) Neighbors(node NodeID, dst []NodeID) []NodeID {
	cx, cy := m.Coords(node)
	if !m.InBounds(cx, cy) || m.IsWall(cx, cy) {
		return dst
	}

	// Cardinal directions: N, E, S, W
	cardinal := [4][2]int{{0, -1}, {1, 0}, {0, 1}, {-1, 0}}
	for _, d := range cardinal {
		nx, ny := cx+d[0], cy+d[1]
		if !m.IsWall(nx, ny) {
			dst = append(dst, m.NodeID(nx, ny))
		}
	}

	if m.AllowDiagonal {
		diags := [4][2]int{{1, -1}, {1, 1}, {-1, 1}, {-1, -1}}
		for _, d := range diags {
			nx, ny := cx+d[0], cy+d[1]
			if !m.IsWall(nx, ny) && !m.IsWall(cx+d[0], cy) && !m.IsWall(cx, cy+d[1]) {
				dst = append(dst, m.NodeID(nx, ny))
			}
		}
	}
	return dst
}

// Cost returns the Euclidean world-space step distance weighted by the average
// cost multiplier of cells a and b.
func (m *GridMap) Cost(a, b NodeID) float64 {
	ax, ay := m.Coords(a)
	bx, by := m.Coords(b)
	if m.IsWall(ax, ay) || m.IsWall(bx, by) {
		return math.Inf(1)
	}
	dist := m.CellToWorld(ax, ay).Dist(m.CellToWorld(bx, by))
	weight := 0.5 * (m.costs[int(a)] + m.costs[int(b)])
	return dist * weight
}

// Heuristic returns the octile / Euclidean lower-bound distance between node and goal.
func (m *GridMap) Heuristic(node, goal NodeID) float64 {
	return m.Position(node).Dist(m.Position(goal))
}

// HasLineOfSight performs a supercover ray march across the grid between from
// and to, returning false if any touched cell is blocked or out of bounds.
// It can be assigned directly to Config.LineOfSight on a World.
func (m *GridMap) HasLineOfSight(from, to Vec3) bool {
	x0, y0, ok0 := m.WorldToCell(from)
	x1, y1, ok1 := m.WorldToCell(to)
	if !ok0 || !ok1 || m.IsWall(x0, y0) || m.IsWall(x1, y1) {
		return false
	}
	if x0 == x1 && y0 == y1 {
		return true
	}

	dx := to.X - from.X
	dy := to.Y - from.Y
	dist := math.Hypot(dx, dy)
	steps := int(math.Ceil(dist/(m.cellSize*0.25))) + 1
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		px := from.X + dx*t
		py := from.Y + dy*t
		cx, cy, ok := m.WorldToCell(Vec2(px, py))
		if !ok || m.IsWall(cx, cy) {
			return false
		}
		// Prevent grazing past diagonal wall corners.
		if i > 0 {
			prevT := float64(i-1) / float64(steps)
			pcx, pcy, _ := m.WorldToCell(Vec2(from.X+dx*prevT, from.Y+dy*prevT))
			if cx != pcx && cy != pcy {
				if m.IsWall(cx, pcy) || m.IsWall(pcx, cy) {
					return false
				}
			}
		}
	}
	return true
}

// SmoothPath simplifies a waypoint path using line-of-sight string-pulling,
// removing redundant intermediate waypoints when a direct unobstructed segment
// exists on the GridMap.
func (m *GridMap) SmoothPath(path []Vec3) []Vec3 {
	if len(path) <= 2 {
		return path
	}
	out := make([]Vec3, 0, len(path))
	anchor := 0
	out = append(out, path[anchor])

	for anchor < len(path)-1 {
		furthest := anchor + 1
		for probe := len(path) - 1; probe > anchor+1; probe-- {
			if m.HasLineOfSight(path[anchor], path[probe]) {
				furthest = probe
				break
			}
		}
		out = append(out, path[furthest])
		anchor = furthest
	}
	return out
}

// FindWorldPath plans a path between two world-space positions on the GridMap.
// If smooth is true, intermediate waypoints with clear line of sight are merged.
func (m *GridMap) FindWorldPath(from, to Vec3, smooth bool) ([]Vec3, bool) {
	sx, sy, okS := m.WorldToCell(from)
	gx, gy, okG := m.WorldToCell(to)
	if !okS || !okG {
		return nil, false
	}
	startID := m.NodeID(sx, sy)
	goalID := m.NodeID(gx, gy)
	if m.IsWall(sx, sy) || m.IsWall(gx, gy) {
		return nil, false
	}

	path, ok := FindPath(m, startID, goalID, DefaultMaxExpansions)
	if !ok || len(path) == 0 {
		return nil, false
	}
	path[len(path)-1] = Vec3{X: to.X, Y: to.Y, Z: m.origin.Z}
	if smooth && len(path) > 2 {
		path = m.SmoothPath(path)
	}
	return path, true
}

// FlowField stores a precomputed Dijkstra distance integration field and a
// normalized steering direction vector per cell on a GridMap, allowing large
// swarms of Actors to navigate around obstacles toward a shared goal in O(1)
// lookup time per Actor per tick.
type FlowField struct {
	grid  *GridMap
	goalX int
	goalY int
	valid bool
	dist  []float64
	dirs  []Vec3
}

// NewFlowField constructs a FlowField bound to grid.
func NewFlowField(grid *GridMap) *FlowField {
	if grid == nil {
		panic("ntactor: NewFlowField requires a non-nil GridMap")
	}
	n := grid.width * grid.height
	dist := make([]float64, n)
	for i := range dist {
		dist[i] = math.Inf(1)
	}
	return &FlowField{
		grid:  grid,
		goalX: -1,
		goalY: -1,
		dist:  dist,
		dirs:  make([]Vec3, n),
	}
}

// Grid returns the underlying GridMap.
func (f *FlowField) Grid() *GridMap { return f.grid }

// GoalCell returns the cell coordinates (cx, cy) of the most recently computed goal.
func (f *FlowField) GoalCell() (int, int) { return f.goalX, f.goalY }

// Valid reports whether the FlowField has been computed for a valid passable goal.
func (f *FlowField) Valid() bool { return f.valid }

// ComputeWorld builds the integration and direction fields toward goalPos.
func (f *FlowField) ComputeWorld(goalPos Vec3) bool {
	cx, cy, ok := f.grid.WorldToCell(goalPos)
	if !ok {
		return false
	}
	return f.Compute(cx, cy)
}

// Compute builds the integration and direction fields toward cell (goalX, goalY).
func (f *FlowField) Compute(goalX, goalY int) bool {
	g := f.grid
	for i := range f.dist {
		f.dist[i] = math.Inf(1)
		f.dirs[i] = Vec3{}
	}
	f.goalX = goalX
	f.goalY = goalY
	f.valid = false

	if !g.InBounds(goalX, goalY) || g.IsWall(goalX, goalY) {
		return false
	}

	goalID := g.NodeID(goalX, goalY)
	f.dist[int(goalID)] = 0

	var heap astarHeap
	heap.push(astarEntry{node: goalID, f: 0, g: 0})

	var neighbors []NodeID
	for len(heap) > 0 {
		cur := heap.pop()
		if cur.g > f.dist[int(cur.node)]+1e-12 {
			continue
		}
		neighbors = g.Neighbors(cur.node, neighbors[:0])
		for _, next := range neighbors {
			step := g.Cost(cur.node, next)
			cand := cur.g + step
			if cand+1e-12 < f.dist[int(next)] {
				f.dist[int(next)] = cand
				heap.push(astarEntry{node: next, f: cand, g: cand})
			}
		}
	}

	for cy := 0; cy < g.height; cy++ {
		for cx := 0; cx < g.width; cx++ {
			id := g.NodeID(cx, cy)
			if g.IsWall(cx, cy) || math.IsInf(f.dist[int(id)], 1) {
				continue
			}
			if cx == goalX && cy == goalY {
				continue
			}
			bestCost := f.dist[int(id)]
			var bestNeighbor NodeID = InvalidNodeID
			neighbors = g.Neighbors(id, neighbors[:0])
			for _, nb := range neighbors {
				if f.dist[int(nb)]+1e-12 < bestCost {
					bestCost = f.dist[int(nb)]
					bestNeighbor = nb
				}
			}
			if bestNeighbor != InvalidNodeID {
				fromPos := g.Position(id)
				toPos := g.Position(bestNeighbor)
				f.dirs[int(id)] = toPos.Sub(fromPos).Normalize()
			}
		}
	}

	f.valid = true
	return true
}

// DistanceAt returns the integrated path cost from world position pos to the
// goal, or +Inf if pos is out of bounds or unreachable.
func (f *FlowField) DistanceAt(pos Vec3) float64 {
	cx, cy, ok := f.grid.WorldToCell(pos)
	if !ok {
		return math.Inf(1)
	}
	return f.dist[cy*f.grid.width+cx]
}

// DirectionAt returns the normalized 2D flow direction vector at world position pos.
func (f *FlowField) DirectionAt(pos Vec3) Vec3 {
	if !f.valid {
		return Vec3{}
	}
	cx, cy, ok := f.grid.WorldToCell(pos)
	if !ok || f.grid.IsWall(cx, cy) {
		return Vec3{}
	}
	if cx == f.goalX && cy == f.goalY {
		goalCenter := f.grid.CellToWorld(f.goalX, f.goalY)
		diff := goalCenter.Sub(pos)
		if diff.LenSq() <= 1e-6 {
			return Vec3{}
		}
		return diff.Normalize()
	}
	return f.dirs[cy*f.grid.width+cx]
}

// FollowPathBehavior steers an Actor along an ordered slice of waypoints
// (such as one produced by FindPath or GridMap.FindWorldPath), decelerating smoothly
// at the final waypoint unless Loop is true.
type FollowPathBehavior struct {
	Path          []Vec3
	CurrentIndex  int
	Speed         float64
	MaxForce      float64
	ArrivalRadius float64
	SlowRadius    float64
	Loop          bool
}

// SetPath assigns a new waypoint path and resets CurrentIndex to 0.
func (p *FollowPathBehavior) SetPath(path []Vec3) {
	p.Path = path
	p.CurrentIndex = 0
}

// Completed reports whether the Actor has reached the final waypoint of a non-looping path.
func (p *FollowPathBehavior) Completed() bool {
	return !p.Loop && len(p.Path) > 0 && p.CurrentIndex >= len(p.Path)
}

// Tick advances the Actor along Path using SteerSeek for intermediate waypoints
// and SteerArrive for the final waypoint.
func (p *FollowPathBehavior) Tick(dt float64, self *Actor, _ *World) {
	if p == nil || self == nil || dt <= 0 || len(p.Path) == 0 {
		return
	}
	speed := p.Speed
	if speed <= 0 {
		speed = 5.0
	}
	maxForce := p.MaxForce
	if maxForce <= 0 {
		maxForce = speed * 4.0
	}
	arriveRad := p.ArrivalRadius
	if arriveRad <= 0 {
		arriveRad = 0.4
	}
	slowRad := p.SlowRadius
	if slowRad <= 0 {
		slowRad = arriveRad * 3.0
	}

	if p.CurrentIndex >= len(p.Path) {
		if p.Loop {
			p.CurrentIndex = 0
		} else {
			self.Velocity = Vec3{}
			return
		}
	}

	target := p.Path[p.CurrentIndex]
	dist := self.Position.Dist(target)
	if dist <= arriveRad {
		p.CurrentIndex++
		if p.CurrentIndex >= len(p.Path) {
			if p.Loop {
				p.CurrentIndex = 0
			} else {
				self.Velocity = Vec3{}
				return
			}
		}
		target = p.Path[p.CurrentIndex]
	}

	isLast := !p.Loop && p.CurrentIndex == len(p.Path)-1
	var desired Vec3
	if isLast {
		desired = SteerArrive(self.Position, target, speed, slowRad)
	} else {
		desired = SteerSeek(self.Position, target, speed)
	}
	ApplySteering(self, desired, maxForce, speed, dt)
}

// FlowFieldBehavior steers an Actor along a precomputed FlowField while
// optionally applying local neighbor separation so crowds flow around walls
// without overlapping.
type FlowFieldBehavior struct {
	Field            *FlowField
	MaxSpeed         float64
	MaxForce         float64
	SeparationRadius float64
	SeparationWeight float64
	ArrivalRadius    float64

	scratch []*Actor
}

// Tick samples Field.DirectionAt(self.Position), blends local separation, and
// integrates self.Position.
func (b *FlowFieldBehavior) Tick(dt float64, self *Actor, world *World) {
	if b == nil || b.Field == nil || !b.Field.Valid() || self == nil || dt <= 0 {
		return
	}
	maxSpeed := b.MaxSpeed
	if maxSpeed <= 0 {
		maxSpeed = 6.0
	}
	maxForce := b.MaxForce
	if maxForce <= 0 {
		maxForce = maxSpeed * 4.0
	}
	arriveRad := b.ArrivalRadius
	if arriveRad <= 0 {
		arriveRad = 0.5
	}

	gx, gy := b.Field.GoalCell()
	goalPos := b.Field.Grid().CellToWorld(gx, gy)
	if self.Position.Dist(goalPos) <= arriveRad {
		desired := SteerArrive(self.Position, goalPos, maxSpeed, arriveRad*2.5)
		ApplySteering(self, desired, maxForce, maxSpeed, dt)
		return
	}

	dir := b.Field.DirectionAt(self.Position)
	desired := dir.Scale(maxSpeed)

	if world != nil && b.SeparationRadius > 0 && b.SeparationWeight > 0 {
		b.scratch = world.NeighborsInto(self.ID, b.SeparationRadius, b.scratch[:0])
		sep := SteerSeparation(self, b.scratch, b.SeparationRadius, maxSpeed)
		// Only apply separation components that do not push the Actor into a wall cell.
		candidate := self.Position.Add(sep.Normalize().Scale(b.Field.Grid().CellSize() * 0.5))
		cx, cy, ok := b.Field.Grid().WorldToCell(candidate)
		if ok && !b.Field.Grid().IsWall(cx, cy) {
			desired = desired.Add(sep.Scale(b.SeparationWeight)).ClampLen(maxSpeed)
		}
	}

	ApplySteering(self, desired, maxForce, maxSpeed, dt)
}

// WaypointEdge represents a directed weighted edge in a WaypointGraph.
type WaypointEdge struct {
	To             NodeID
	CostMultiplier float64
}

// WaypointGraph is a 3D sparse navigation graph (suitable for multi-floor
// waypoint networks, jump/elevator links, and navmesh polygon centroids) that
// implements NavGraph.
type WaypointGraph struct {
	positions []Vec3
	edges     [][]WaypointEdge
}

// NewWaypointGraph constructs an empty WaypointGraph.
func NewWaypointGraph() *WaypointGraph {
	return &WaypointGraph{}
}

// NodeCount returns the total number of nodes in the graph.
func (g *WaypointGraph) NodeCount() int {
	if g == nil {
		return 0
	}
	return len(g.positions)
}

// AddNode appends a 3D waypoint position and returns its NodeID.
func (g *WaypointGraph) AddNode(pos Vec3) NodeID {
	id := NodeID(len(g.positions))
	g.positions = append(g.positions, pos)
	g.edges = append(g.edges, nil)
	return id
}

// AddEdge adds or updates a directed edge from -> to with costMultiplier (>= 1.0, default 1.0).
func (g *WaypointGraph) AddEdge(from, to NodeID, costMultiplier float64) {
	if g == nil || from < 0 || int(from) >= len(g.positions) || to < 0 || int(to) >= len(g.positions) || from == to {
		return
	}
	if costMultiplier <= 0 || math.IsNaN(costMultiplier) || math.IsInf(costMultiplier, 0) {
		costMultiplier = 1.0
	}
	adj := g.edges[int(from)]
	for i := range adj {
		if adj[i].To == to {
			adj[i].CostMultiplier = costMultiplier
			return
		}
	}
	g.edges[int(from)] = append(adj, WaypointEdge{To: to, CostMultiplier: costMultiplier})
}

// AddBiEdge adds or updates an undirected (bi-directional) edge between a and b.
func (g *WaypointGraph) AddBiEdge(a, b NodeID, costMultiplier float64) {
	g.AddEdge(a, b, costMultiplier)
	g.AddEdge(b, a, costMultiplier)
}

// ConnectWithinRadius automatically connects all pairs of nodes within maxDist
// (and passing optional los check if non-nil) with bi-directional unit-cost edges
// in deterministic NodeID order.
func (g *WaypointGraph) ConnectWithinRadius(maxDist float64, los func(from, to Vec3) bool) {
	if g == nil || maxDist <= 0 {
		return
	}
	maxSq := maxDist * maxDist
	n := len(g.positions)
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			pa, pb := g.positions[i], g.positions[j]
			if pa.DistSq(pb) <= maxSq {
				if los != nil && !los(pa, pb) {
					continue
				}
				g.AddBiEdge(NodeID(i), NodeID(j), 1.0)
			}
		}
	}
}

// NearestNode returns the NodeID of the waypoint closest to pos, or InvalidNodeID
// if the graph is empty.
func (g *WaypointGraph) NearestNode(pos Vec3) NodeID {
	if g == nil || len(g.positions) == 0 {
		return InvalidNodeID
	}
	bestID := NodeID(0)
	bestSq := pos.DistSq(g.positions[0])
	for i := 1; i < len(g.positions); i++ {
		dSq := pos.DistSq(g.positions[i])
		if dSq < bestSq {
			bestSq = dSq
			bestID = NodeID(i)
		}
	}
	return bestID
}

// Position returns the 3D world-space coordinate of node.
func (g *WaypointGraph) Position(node NodeID) Vec3 {
	if g == nil || node < 0 || int(node) >= len(g.positions) {
		return Vec3{}
	}
	return g.positions[int(node)]
}

// Neighbors appends outgoing neighbor NodeIDs from node to dst in registration order.
func (g *WaypointGraph) Neighbors(node NodeID, dst []NodeID) []NodeID {
	if g == nil || node < 0 || int(node) >= len(g.edges) {
		return dst
	}
	for _, e := range g.edges[int(node)] {
		dst = append(dst, e.To)
	}
	return dst
}

// Cost returns the Euclidean distance between a and b scaled by the edge's CostMultiplier.
func (g *WaypointGraph) Cost(a, b NodeID) float64 {
	if g == nil || a < 0 || int(a) >= len(g.edges) || b < 0 || int(b) >= len(g.positions) {
		return math.Inf(1)
	}
	for _, e := range g.edges[int(a)] {
		if e.To == b {
			return g.positions[int(a)].Dist(g.positions[int(b)]) * e.CostMultiplier
		}
	}
	return math.Inf(1)
}

// Heuristic returns the 3D Euclidean distance from node to goal.
func (g *WaypointGraph) Heuristic(node, goal NodeID) float64 {
	return g.Position(node).Dist(g.Position(goal))
}

// FindWorldPath plans a 3D path from from to to by snapping to the nearest
// start and goal waypoints in g and running A*.
func (g *WaypointGraph) FindWorldPath(from, to Vec3) ([]Vec3, bool) {
	start := g.NearestNode(from)
	goal := g.NearestNode(to)
	if start == InvalidNodeID || goal == InvalidNodeID {
		return nil, false
	}
	path, ok := FindPath(g, start, goal, DefaultMaxExpansions)
	if !ok || len(path) == 0 {
		return nil, false
	}
	if path[len(path)-1].DistSq(to) > 1e-6 {
		path = append(path, to)
	}
	return path, true
}

// VoxelGrid is a 3D volumetric occupancy and weighted-cost grid that implements
// NavGraph for 3D aerial, underwater, and multi-story routing.
type VoxelGrid struct {
	width         int
	height        int
	depth         int
	cellSize      float64
	invCell       float64
	origin        Vec3
	AllowDiagonal bool
	blocked       []bool
	costs         []float64
}

// NewVoxelGrid constructs a width x height x depth 3D VoxelGrid where voxel
// (0,0,0) has its minimum corner at origin and each voxel is a cube of side cellSize.
func NewVoxelGrid(width, height, depth int, cellSize float64, origin Vec3) *VoxelGrid {
	if width <= 0 {
		width = 1
	}
	if height <= 0 {
		height = 1
	}
	if depth <= 0 {
		depth = 1
	}
	if cellSize <= 0 || math.IsNaN(cellSize) || math.IsInf(cellSize, 0) {
		cellSize = 1.0
	}
	n := width * height * depth
	costs := make([]float64, n)
	for i := range costs {
		costs[i] = 1.0
	}
	return &VoxelGrid{
		width:         width,
		height:        height,
		depth:         depth,
		cellSize:      cellSize,
		invCell:       1.0 / cellSize,
		origin:        origin,
		AllowDiagonal: true,
		blocked:       make([]bool, n),
		costs:         costs,
	}
}

// Width returns the number of X columns.
func (v *VoxelGrid) Width() int { return v.width }

// Height returns the number of Y rows.
func (v *VoxelGrid) Height() int { return v.height }

// Depth returns the number of Z layers.
func (v *VoxelGrid) Depth() int { return v.depth }

// CellSize returns the world-space edge length of each voxel.
func (v *VoxelGrid) CellSize() float64 { return v.cellSize }

// Origin returns the minimum world-space corner (X, Y, Z) of the voxel grid.
func (v *VoxelGrid) Origin() Vec3 { return v.origin }

// InBounds reports whether voxel coordinates (cx, cy, cz) lie inside the grid.
func (v *VoxelGrid) InBounds(cx, cy, cz int) bool {
	return cx >= 0 && cx < v.width && cy >= 0 && cy < v.height && cz >= 0 && cz < v.depth
}

func (v *VoxelGrid) index(cx, cy, cz int) int {
	return (cz*v.height+cy)*v.width + cx
}

// NodeID converts voxel coordinates (cx, cy, cz) into a NodeID, or returns
// InvalidNodeID if out of bounds.
func (v *VoxelGrid) NodeID(cx, cy, cz int) NodeID {
	if !v.InBounds(cx, cy, cz) {
		return InvalidNodeID
	}
	return NodeID(v.index(cx, cy, cz))
}

// Coords converts a NodeID back into voxel coordinates (cx, cy, cz).
func (v *VoxelGrid) Coords(id NodeID) (cx, cy, cz int) {
	if id < 0 || int(id) >= len(v.blocked) {
		return -1, -1, -1
	}
	idx := int(id)
	cx = idx % v.width
	idx /= v.width
	cy = idx % v.height
	cz = idx / v.height
	return cx, cy, cz
}

// SetWall marks voxel (cx, cy, cz) as blocked or passable.
func (v *VoxelGrid) SetWall(cx, cy, cz int, blocked bool) {
	if v.InBounds(cx, cy, cz) {
		v.blocked[v.index(cx, cy, cz)] = blocked
	}
}

// SetBoxWall marks all voxels in the inclusive box [x0..x1, y0..y1, z0..z1] as
// blocked or passable.
func (v *VoxelGrid) SetBoxWall(x0, y0, z0, x1, y1, z1 int, blocked bool) {
	if x0 > x1 {
		x0, x1 = x1, x0
	}
	if y0 > y1 {
		y0, y1 = y1, y0
	}
	if z0 > z1 {
		z0, z1 = z1, z0
	}
	for z := z0; z <= z1; z++ {
		for y := y0; y <= y1; y++ {
			for x := x0; x <= x1; x++ {
				v.SetWall(x, y, z, blocked)
			}
		}
	}
}

// IsWall reports whether voxel (cx, cy, cz) is out of bounds or blocked.
func (v *VoxelGrid) IsWall(cx, cy, cz int) bool {
	if !v.InBounds(cx, cy, cz) {
		return true
	}
	return v.blocked[v.index(cx, cy, cz)]
}

// SetCost sets the traversal cost multiplier (>= 1.0) for voxel (cx, cy, cz).
func (v *VoxelGrid) SetCost(cx, cy, cz int, costMultiplier float64) {
	if !v.InBounds(cx, cy, cz) {
		return
	}
	if costMultiplier < 1.0 || math.IsNaN(costMultiplier) || math.IsInf(costMultiplier, 0) {
		costMultiplier = 1.0
	}
	v.costs[v.index(cx, cy, cz)] = costMultiplier
}

// WorldToCell maps a 3D world-space Vec3 to voxel coordinates (cx, cy, cz).
func (v *VoxelGrid) WorldToCell(pos Vec3) (cx, cy, cz int, ok bool) {
	cx = int(math.Floor((pos.X - v.origin.X) * v.invCell))
	cy = int(math.Floor((pos.Y - v.origin.Y) * v.invCell))
	cz = int(math.Floor((pos.Z - v.origin.Z) * v.invCell))
	return cx, cy, cz, v.InBounds(cx, cy, cz)
}

// CellToWorld returns the 3D world-space center of voxel (cx, cy, cz).
func (v *VoxelGrid) CellToWorld(cx, cy, cz int) Vec3 {
	return Vec3{
		X: v.origin.X + (float64(cx)+0.5)*v.cellSize,
		Y: v.origin.Y + (float64(cy)+0.5)*v.cellSize,
		Z: v.origin.Z + (float64(cz)+0.5)*v.cellSize,
	}
}

// Position returns the 3D world-space center of node.
func (v *VoxelGrid) Position(node NodeID) Vec3 {
	cx, cy, cz := v.Coords(node)
	return v.CellToWorld(cx, cy, cz)
}

// Neighbors appends passable adjacent 3D voxel NodeIDs to dst in deterministic order.
// When AllowDiagonal is true, 2D/3D diagonal steps are permitted only when all
// axis-aligned intermediate voxels are also passable (preventing corner clipping).
func (v *VoxelGrid) Neighbors(node NodeID, dst []NodeID) []NodeID {
	cx, cy, cz := v.Coords(node)
	if !v.InBounds(cx, cy, cz) || v.IsWall(cx, cy, cz) {
		return dst
	}

	// 6 cardinal directions: -X, +X, -Y, +Y, -Z, +Z
	cardinal := [6][3]int{{-1, 0, 0}, {1, 0, 0}, {0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}}
	for _, d := range cardinal {
		nx, ny, nz := cx+d[0], cy+d[1], cz+d[2]
		if !v.IsWall(nx, ny, nz) {
			dst = append(dst, v.NodeID(nx, ny, nz))
		}
	}

	if v.AllowDiagonal {
		for dz := -1; dz <= 1; dz++ {
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					nonZero := 0
					if dx != 0 {
						nonZero++
					}
					if dy != 0 {
						nonZero++
					}
					if dz != 0 {
						nonZero++
					}
					if nonZero <= 1 {
						continue
					}
					nx, ny, nz := cx+dx, cy+dy, cz+dz
					if v.IsWall(nx, ny, nz) {
						continue
					}
					// Require orthogonal component steps to be passable to prevent corner clipping.
					if dx != 0 && v.IsWall(cx+dx, cy, cz) {
						continue
					}
					if dy != 0 && v.IsWall(cx, cy+dy, cz) {
						continue
					}
					if dz != 0 && v.IsWall(cx, cy, cz+dz) {
						continue
					}
					dst = append(dst, v.NodeID(nx, ny, nz))
				}
			}
		}
	}
	return dst
}

// Cost returns the 3D Euclidean world-space step distance weighted by the average
// cost multiplier of voxels a and b.
func (v *VoxelGrid) Cost(a, b NodeID) float64 {
	ax, ay, az := v.Coords(a)
	bx, by, bz := v.Coords(b)
	if v.IsWall(ax, ay, az) || v.IsWall(bx, by, bz) {
		return math.Inf(1)
	}
	dist := v.CellToWorld(ax, ay, az).Dist(v.CellToWorld(bx, by, bz))
	weight := 0.5 * (v.costs[int(a)] + v.costs[int(b)])
	return dist * weight
}

// Heuristic returns the 3D Euclidean distance between node and goal.
func (v *VoxelGrid) Heuristic(node, goal NodeID) float64 {
	return v.Position(node).Dist(v.Position(goal))
}

// HasLineOfSight performs a 3D ray march across the VoxelGrid between from and
// to, returning false if any touched voxel is blocked or out of bounds.
func (v *VoxelGrid) HasLineOfSight(from, to Vec3) bool {
	x0, y0, z0, ok0 := v.WorldToCell(from)
	x1, y1, z1, ok1 := v.WorldToCell(to)
	if !ok0 || !ok1 || v.IsWall(x0, y0, z0) || v.IsWall(x1, y1, z1) {
		return false
	}
	if x0 == x1 && y0 == y1 && z0 == z1 {
		return true
	}

	diff := to.Sub(from)
	dist := diff.Len()
	steps := int(math.Ceil(dist/(v.cellSize*0.25))) + 1
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		p := from.Add(diff.Scale(t))
		cx, cy, cz, ok := v.WorldToCell(p)
		if !ok || v.IsWall(cx, cy, cz) {
			return false
		}
	}
	return true
}

// SmoothPath simplifies a 3D waypoint path using line-of-sight string-pulling.
func (v *VoxelGrid) SmoothPath(path []Vec3) []Vec3 {
	if len(path) <= 2 {
		return path
	}
	out := make([]Vec3, 0, len(path))
	anchor := 0
	out = append(out, path[anchor])

	for anchor < len(path)-1 {
		furthest := anchor + 1
		for probe := len(path) - 1; probe > anchor+1; probe-- {
			if v.HasLineOfSight(path[anchor], path[probe]) {
				furthest = probe
				break
			}
		}
		out = append(out, path[furthest])
		anchor = furthest
	}
	return out
}

// FindWorldPath plans a 3D path between two world-space positions on the VoxelGrid.
func (v *VoxelGrid) FindWorldPath(from, to Vec3, smooth bool) ([]Vec3, bool) {
	sx, sy, sz, okS := v.WorldToCell(from)
	gx, gy, gz, okG := v.WorldToCell(to)
	if !okS || !okG {
		return nil, false
	}
	if v.IsWall(sx, sy, sz) || v.IsWall(gx, gy, gz) {
		return nil, false
	}
	path, ok := FindPath(v, v.NodeID(sx, sy, sz), v.NodeID(gx, gy, gz), DefaultMaxExpansions)
	if !ok || len(path) == 0 {
		return nil, false
	}
	path[len(path)-1] = to
	if smooth && len(path) > 2 {
		path = v.SmoothPath(path)
	}
	return path, true
}
