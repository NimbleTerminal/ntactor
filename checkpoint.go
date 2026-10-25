// Copyright (c) 2026 Neomantra Corp.

package ntactor

import (
	"encoding/json"
	"fmt"
	"slices"
)

type followPathTagNode struct {
	tag        string
	speed      float64
	maxForce   float64
	smooth     bool
	capRad     float64
	destroy    bool
	scoreKey   string
	intentKind string
	gridMap    *GridMap
	follow     *FollowPathBehavior
	goalID     ActorID
	goalPos    Vec3
}

func (n *followPathTagNode) Evaluate(dt float64, self *Actor, world *World) NodeStatus {
	target := nearestByTagWithin(self, world, n.tag, 1e9, false)
	if target == nil {
		return StatusFailure
	}
	dist := self.Position.Dist(target.Position)
	self.Blackboard().SetString("status", fmt.Sprintf("A*->%s#%d (%.1fm)", n.tag, target.ID, dist))
	if n.capRad > 0 && dist <= n.capRad {
		onCaptureTarget(self, world, target, n.destroy, n.scoreKey, n.intentKind)
		n.follow.SetPath(nil)
		n.goalID = InvalidActorID
		return StatusSuccess
	}
	if n.gridMap != nil {
		if n.goalID != target.ID || target.Position.Dist(n.goalPos) > n.gridMap.CellSize() || n.follow.Completed() || len(n.follow.Path) == 0 {
			path, ok := n.gridMap.FindWorldPath(self.Position, target.Position, n.smooth)
			if !ok || len(path) == 0 {
				path = []Vec3{target.Position}
			}
			n.follow.SetPath(path)
			n.goalID = target.ID
			n.goalPos = target.Position
		}
		n.follow.Tick(dt, self, world)
		return StatusRunning
	}
	desired := SteerArrive(self.Position, target.Position, n.speed, 2.5)
	ApplySteering(self, desired, n.maxForce, n.speed, dt)
	return StatusRunning
}

type patrolBoxNode struct {
	patrol *PatrolBehavior
}

func (n *patrolBoxNode) Evaluate(dt float64, self *Actor, world *World) NodeStatus {
	n.patrol.Tick(dt, self, world)
	self.Blackboard().SetString("status", fmt.Sprintf("patrol wp#%d", n.patrol.CurrentIndex+1))
	return StatusRunning
}

type investigateMemoryNode struct {
	tag           string
	speed         float64
	maxForce      float64
	leadSeconds   float64
	minConfidence float64
	arrivalRadius float64
	capRad        float64
	destroy       bool
	scoreKey      string
	intentKind    string
	gridMap       *GridMap
	follow        *FollowPathBehavior
	goalTargetID  ActorID
	goalPos       Vec3
}

func (n *investigateMemoryNode) Evaluate(dt float64, self *Actor, world *World) NodeStatus {
	mem := self.Memory()
	rec, ok := mem.BestByTag(self.Position, n.tag, n.minConfidence)
	if !ok {
		return StatusFailure
	}
	if target := world.ActorByID(rec.TargetID); target != nil && world.HasLineOfSight(self.Position, target.Position) {
		mem.Observe(target, SenseVision, 1.0, world.SimTime(), true)
		rec, _ = mem.Get(target.ID)
		if n.capRad > 0 && self.Position.Dist(target.Position) <= n.capRad {
			onCaptureTarget(self, world, target, n.destroy, n.scoreKey, n.intentKind)
			mem.Forget(target.ID)
			n.follow.SetPath(nil)
			n.goalTargetID = InvalidActorID
			return StatusSuccess
		}
	} else if self.Position.Dist(rec.LastKnownPosition) <= n.arrivalRadius {
		mem.Forget(rec.TargetID)
		n.follow.SetPath(nil)
		n.goalTargetID = InvalidActorID
		return StatusFailure
	}

	goal := rec.ExtrapolatedPosition(n.leadSeconds)
	if n.gridMap != nil {
		if cx, cy, inBounds := n.gridMap.WorldToCell(goal); !inBounds || n.gridMap.IsWall(cx, cy) {
			goal = rec.LastKnownPosition
		}
		if n.goalTargetID != rec.TargetID || goal.Dist(n.goalPos) > n.gridMap.CellSize() || n.follow.Completed() || len(n.follow.Path) == 0 {
			path, found := n.gridMap.FindWorldPath(self.Position, goal, true)
			if !found || len(path) == 0 {
				path = []Vec3{goal}
			}
			n.follow.SetPath(path)
			n.goalTargetID = rec.TargetID
			n.goalPos = goal
		}
		n.follow.Tick(dt, self, world)
		self.Blackboard().SetString("status", fmt.Sprintf("investigate %s#%d (conf=%.2f)", n.tag, rec.TargetID, rec.Confidence))
		return StatusRunning
	}

	desired := SteerArrive(self.Position, goal, n.speed, 2.5)
	ApplySteering(self, desired, n.maxForce, n.speed, dt)
	self.Blackboard().SetString("status", fmt.Sprintf("investigate %s#%d (conf=%.2f)", n.tag, rec.TargetID, rec.Confidence))
	return StatusRunning
}

// CloneableBehavior is an optional interface that custom stateful Behaviors can
// implement so World.Checkpoint, World.Restore, and World.Clone deep-copy their
// internal state across simulation branches.
type CloneableBehavior interface {
	Behavior
	CloneBehavior() Behavior
}

// CloneBehavior returns an independent deep copy of b when b is a built-in
// stateful Behavior (*BehaviorTree, *FSM, *UtilitySelectorNode, *GOAPNode,
// *FollowPathBehavior, *PatrolBehavior, etc.) or implements CloneableBehavior.
// Stateless behaviors and functions are returned as-is.
func CloneBehavior(b Behavior) Behavior {
	if b == nil {
		return nil
	}
	if cb, ok := b.(CloneableBehavior); ok {
		return cb.CloneBehavior()
	}
	switch v := b.(type) {
	case *BehaviorTree:
		if v == nil {
			return nil
		}
		out := &BehaviorTree{
			lastStatus: v.lastStatus,
			trace:      v.LastTrace(),
		}
		out.root = cloneBTNode(v.root, &out.trace)
		return out
	case *FSM:
		return cloneFSM(v, nil)
	case *UtilitySelectorNode:
		return cloneUtilitySelector(v, nil)
	case *GOAPNode:
		return cloneGOAPNode(v, nil)
	case *FollowPathBehavior:
		if v == nil {
			return nil
		}
		cp := *v
		cp.Path = slices.Clone(v.Path)
		return &cp
	case *PatrolBehavior:
		if v == nil {
			return nil
		}
		cp := *v
		cp.Waypoints = slices.Clone(v.Waypoints)
		return &cp
	case *FlockBehavior:
		if v == nil {
			return nil
		}
		cp := *v
		return &cp
	case *FollowLeaderBehavior:
		if v == nil {
			return nil
		}
		cp := *v
		return &cp
	case *WanderBehavior:
		if v == nil {
			return nil
		}
		cp := *v
		return &cp
	case *FlowFieldBehavior:
		if v == nil {
			return nil
		}
		cp := *v
		return &cp
	default:
		return b
	}
}

func cloneFSM(f *FSM, trace *ExecutionTrace) *FSM {
	if f == nil {
		return nil
	}
	states := make(map[StateID]FSMState, len(f.states))
	for id, st := range f.states {
		stCopy := st
		if st.Node != nil {
			stCopy.Node = cloneBTNode(st.Node, trace)
		}
		states[id] = stCopy
	}
	return &FSM{
		current:     f.current,
		started:     f.started,
		timeInState: f.timeInState,
		transitions: f.transitions,
		states:      states,
		edges:       slices.Clone(f.edges),
	}
}

func cloneUtilitySelector(u *UtilitySelectorNode, trace *ExecutionTrace) *UtilitySelectorNode {
	if u == nil {
		return nil
	}
	opts := make([]UtilityOption, len(u.options))
	for i, opt := range u.options {
		opts[i] = UtilityOption{
			Name:           opt.Name,
			Weight:         opt.Weight,
			Considerations: slices.Clone(opt.Considerations),
			Node:           cloneBTNode(opt.Node, trace),
		}
	}
	return &UtilitySelectorNode{
		options:       opts,
		selectedIndex: u.selectedIndex,
		selectedName:  u.selectedName,
		selectedScore: u.selectedScore,
		lastTrace:     u.LastTrace(),
	}
}

func cloneGOAPNode(g *GOAPNode, trace *ExecutionTrace) *GOAPNode {
	if g == nil {
		return nil
	}
	goals := make([]GOAPGoal, len(g.goals))
	for i, goal := range g.goals {
		goals[i] = GOAPGoal{
			Name:       goal.Name,
			Priority:   goal.Priority,
			PriorityFn: goal.PriorityFn,
			Desired:    slices.Clone(goal.Desired),
		}
	}
	actions := make([]GOAPAction, len(g.actions))
	actionByName := make(map[string]GOAPAction, len(g.actions))
	for i, act := range g.actions {
		clonedAct := GOAPAction{
			Name:            act.Name,
			Cost:            act.Cost,
			Preconditions:   slices.Clone(act.Preconditions),
			Effects:         slices.Clone(act.Effects),
			CheckProcedural: act.CheckProcedural,
			Node:            cloneBTNode(act.Node, trace),
		}
		actions[i] = clonedAct
		actionByName[act.Name] = clonedAct
	}
	var plan []GOAPAction
	if len(g.plan) > 0 {
		plan = make([]GOAPAction, len(g.plan))
		for i, p := range g.plan {
			if ca, ok := actionByName[p.Name]; ok {
				plan[i] = ca
			} else {
				plan[i] = p
			}
		}
	}
	return &GOAPNode{
		goals:      goals,
		actions:    actions,
		sensors:    slices.Clone(g.sensors),
		activeGoal: g.activeGoal,
		plan:       plan,
		planNames:  slices.Clone(g.planNames),
		stepIdx:    g.stepIdx,
		planCost:   g.planCost,
	}
}

func cloneBTNode(node BTNode, trace *ExecutionTrace) BTNode {
	if node == nil {
		return nil
	}
	switch n := node.(type) {
	case tracedBTNode:
		return tracedBTNode{
			path:  n.path,
			typ:   n.typ,
			name:  n.name,
			inner: cloneBTNode(n.inner, trace),
			trace: trace,
		}
	case sequenceNode:
		ch := make([]BTNode, len(n.children))
		for i, c := range n.children {
			ch[i] = cloneBTNode(c, trace)
		}
		return sequenceNode{children: ch}
	case *memSequenceNode:
		if n == nil {
			return nil
		}
		ch := make([]BTNode, len(n.children))
		for i, c := range n.children {
			ch[i] = cloneBTNode(c, trace)
		}
		return &memSequenceNode{children: ch, running: n.running}
	case selectorNode:
		ch := make([]BTNode, len(n.children))
		for i, c := range n.children {
			ch[i] = cloneBTNode(c, trace)
		}
		return selectorNode{children: ch}
	case *memSelectorNode:
		if n == nil {
			return nil
		}
		ch := make([]BTNode, len(n.children))
		for i, c := range n.children {
			ch[i] = cloneBTNode(c, trace)
		}
		return &memSelectorNode{children: ch, running: n.running}
	case parallelNode:
		ch := make([]BTNode, len(n.children))
		for i, c := range n.children {
			ch[i] = cloneBTNode(c, trace)
		}
		return parallelNode{required: n.required, children: ch}
	case invertNode:
		return invertNode{child: cloneBTNode(n.child, trace)}
	case *cooldownNode:
		if n == nil {
			return nil
		}
		return &cooldownNode{
			duration:  n.duration,
			remaining: n.remaining,
			child:     cloneBTNode(n.child, trace),
		}
	case *timeoutNode:
		if n == nil {
			return nil
		}
		return &timeoutNode{
			maxDuration: n.maxDuration,
			elapsed:     n.elapsed,
			child:       cloneBTNode(n.child, trace),
		}
	case *UtilitySelectorNode:
		return cloneUtilitySelector(n, trace)
	case *FSM:
		return cloneFSM(n, trace)
	case *GOAPNode:
		return cloneGOAPNode(n, trace)
	case *followPathTagNode:
		if n == nil {
			return nil
		}
		cp := *n
		if n.follow != nil {
			fcp := *n.follow
			fcp.Path = slices.Clone(n.follow.Path)
			cp.follow = &fcp
		}
		return &cp
	case *patrolBoxNode:
		if n == nil {
			return nil
		}
		cp := *n
		if n.patrol != nil {
			pcp := *n.patrol
			pcp.Waypoints = slices.Clone(n.patrol.Waypoints)
			cp.patrol = &pcp
		}
		return &cp
	case *investigateMemoryNode:
		if n == nil {
			return nil
		}
		cp := *n
		if n.follow != nil {
			fcp := *n.follow
			fcp.Path = slices.Clone(n.follow.Path)
			cp.follow = &fcp
		}
		return &cp
	default:
		return node
	}
}

// ActorRNGCheckpoint captures a single Actor's derived RNG stream state.
type ActorRNGCheckpoint struct {
	ID  ActorID       `json:"id"`
	RNG RNGCheckpoint `json:"rng"`
}

// ActorCheckpoint captures a single Actor's identity, kinematics, Blackboard,
// SensoryMemory, and in-memory Behavior/State for deterministic rollback and branching.
type ActorCheckpoint struct {
	ID         ActorID           `json:"id"`
	Tag        string            `json:"tag"`
	Position   Vec3              `json:"position"`
	Velocity   Vec3              `json:"velocity"`
	Radius     float64           `json:"radius,omitempty"`
	Blackboard []BlackboardEntry `json:"blackboard,omitempty"`
	Memory     []MemoryRecord    `json:"memory,omitempty"`
	behavior   Behavior
	state      any
}

// WorldCheckpoint captures the complete deterministic simulation state of a
// World, including its clock, RNG streams, global Blackboard, and all active
// Actors.
type WorldCheckpoint struct {
	Seed       uint64               `json:"seed"`
	SimTime    float64              `json:"sim_time"`
	DT         float64              `json:"dt"`
	TickCount  uint64               `json:"tick_count"`
	NextID     ActorID              `json:"next_id"`
	WorldHash  uint64               `json:"world_hash"`
	RNG        RNGCheckpoint        `json:"rng"`
	ActorRNGs  []ActorRNGCheckpoint `json:"actor_rngs,omitempty"`
	Blackboard []BlackboardEntry    `json:"blackboard,omitempty"`
	Actors     []ActorCheckpoint    `json:"actors"`
}

// Checkpoint captures a complete, restorable WorldCheckpoint of w at the
// current simulation tick.
func (w *World) Checkpoint() WorldCheckpoint {
	if w == nil {
		return WorldCheckpoint{}
	}
	actors := w.Actors()
	actorCPs := make([]ActorCheckpoint, 0, len(actors))
	for _, a := range actors {
		if a == nil || a.destroyed {
			continue
		}
		actorCPs = append(actorCPs, ActorCheckpoint{
			ID:         a.ID,
			Tag:        a.Tag,
			Position:   a.Position,
			Velocity:   a.Velocity,
			Radius:     a.Radius,
			Blackboard: a.bb.Entries(),
			Memory:     a.mem.Records(),
			behavior:   CloneBehavior(a.Behavior),
			state:      a.State,
		})
	}

	var rngIDs []ActorID
	for id := range w.actorRNGs {
		rngIDs = append(rngIDs, id)
	}
	slices.Sort(rngIDs)
	var actorRNGs []ActorRNGCheckpoint
	if len(rngIDs) > 0 {
		actorRNGs = make([]ActorRNGCheckpoint, 0, len(rngIDs))
		for _, id := range rngIDs {
			if r := w.actorRNGs[id]; r != nil {
				actorRNGs = append(actorRNGs, ActorRNGCheckpoint{
					ID:  id,
					RNG: r.Checkpoint(),
				})
			}
		}
	}

	return WorldCheckpoint{
		Seed:       w.seed,
		SimTime:    w.simTime,
		DT:         w.dt,
		TickCount:  w.tickCount,
		NextID:     w.nextID,
		WorldHash:  w.Hash(),
		RNG:        w.rng.Checkpoint(),
		ActorRNGs:  actorRNGs,
		Blackboard: w.bb.Entries(),
		Actors:     actorCPs,
	}
}

// Restore rewinds or sets w to the exact simulation state captured in cp.
// If cp was deserialized from JSON (so in-memory Behavior references are nil),
// Restore preserves the existing Behavior and State of any matching ActorID
// already present in w.
func (w *World) Restore(cp WorldCheckpoint) {
	if w == nil {
		return
	}
	existingBehaviors := make(map[ActorID]Behavior, len(w.actors))
	existingStates := make(map[ActorID]any, len(w.actors))
	for _, a := range w.actors {
		if a != nil && !a.destroyed {
			existingBehaviors[a.ID] = a.Behavior
			existingStates[a.ID] = a.State
		}
	}

	w.seed = cp.Seed
	w.simTime = cp.SimTime
	w.dt = cp.DT
	w.tickCount = cp.TickCount
	w.nextID = cp.NextID
	if w.nextID < 1 {
		w.nextID = 1
	}
	if w.rng == nil {
		w.rng = NewRNG(cp.Seed)
	}
	w.rng.Restore(cp.RNG)

	w.actorRNGs = make(map[ActorID]*RNG, len(cp.ActorRNGs))
	for _, ar := range cp.ActorRNGs {
		r := NewRNG(ar.RNG.Seed)
		r.Restore(ar.RNG)
		w.actorRNGs[ar.ID] = r
	}

	w.bb = NewBlackboard()
	for _, e := range cp.Blackboard {
		w.bb.Set(e.Key, e.Value)
	}

	w.actors = make([]*Actor, 0, len(cp.Actors))
	w.indexByID = make(map[ActorID]int, len(cp.Actors))
	for _, ac := range cp.Actors {
		beh := CloneBehavior(ac.behavior)
		if beh == nil {
			beh = CloneBehavior(existingBehaviors[ac.ID])
		}
		st := ac.state
		if st == nil {
			st = existingStates[ac.ID]
		}
		bb := NewBlackboard()
		for _, e := range ac.Blackboard {
			bb.Set(e.Key, e.Value)
		}
		var mem *SensoryMemory
		if len(ac.Memory) > 0 {
			mem = &SensoryMemory{records: slices.Clone(ac.Memory)}
		}
		a := &Actor{
			ID:       ac.ID,
			Tag:      ac.Tag,
			Position: ac.Position,
			Velocity: ac.Velocity,
			Radius:   ac.Radius,
			Behavior: beh,
			State:    st,
			bb:       bb,
			mem:      mem,
			world:    w,
		}
		if a.ID >= w.nextID {
			w.nextID = a.ID + 1
		}
		w.indexByID[a.ID] = len(w.actors)
		w.actors = append(w.actors, a)
	}

	w.ticking = false
	w.pendingPurge = false
	w.spatialDirty = true
	w.events = w.events[:0]
	w.intents = w.intents[:0]
	if w.perception != nil {
		w.perception.Rebuild(w.actors)
		w.spatialDirty = false
	}
}

// RestoreWithRegistry restores cp onto w and, for any Actor whose Behavior is
// nil and whose Blackboard contains a "behavior_spec" JSON string, compiles and
// attaches that BehaviorSpec using reg.
func (w *World) RestoreWithRegistry(cp WorldCheckpoint, reg *BehaviorRegistry) error {
	if w == nil {
		return nil
	}
	w.Restore(cp)
	if reg == nil {
		return nil
	}
	for _, a := range w.actors {
		if a.Behavior == nil {
			if specStr, ok := a.Blackboard().String("behavior_spec"); ok && specStr != "" {
				bt, err := reg.CompileJSON([]byte(specStr))
				if err != nil {
					return err
				}
				a.Behavior = bt
			}
		}
	}
	return nil
}

// Clone returns an independent deep copy of w at its current simulation tick,
// including its RNG streams, Blackboard, event handlers, and cloned Actors and
// Behaviors.
func (w *World) Clone() *World {
	if w == nil {
		return nil
	}
	var perception SpatialIndex
	if gi, ok := w.perception.(*GridIndex); ok && gi != nil {
		perception = NewGridIndex(gi.CellSize())
	} else {
		perception = w.perception
	}
	out := NewWorld(Config{
		Seed:         w.seed,
		Perception:   perception,
		LineOfSight:  w.lineOfSight,
		EventHandler: w.eventHandler,
	})
	if len(w.kindHandlers) > 0 {
		out.kindHandlers = make(map[string][]func(w *World, ev Event), len(w.kindHandlers))
		for k, handlers := range w.kindHandlers {
			out.kindHandlers[k] = slices.Clone(handlers)
		}
	}
	out.Restore(w.Checkpoint())
	return out
}

// EncodeCheckpointJSON serializes a WorldCheckpoint as indented JSON.
func EncodeCheckpointJSON(cp WorldCheckpoint) ([]byte, error) {
	return json.MarshalIndent(cp, "", "  ")
}

// DecodeCheckpointJSON deserializes a WorldCheckpoint from JSON bytes.
func DecodeCheckpointJSON(data []byte) (WorldCheckpoint, error) {
	var cp WorldCheckpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return WorldCheckpoint{}, err
	}
	return cp, nil
}
