// Copyright (c) 2026 Neomantra Corp.

package ntactor

import (
	"math"
	"slices"
	"strings"
)

// GOAPFact represents a single symbolic world-state precondition or effect on
// an Actor's Blackboard (e.g., Key: "has_beacon", Value: BoolValue(true)).
type GOAPFact struct {
	Key   string          `json:"key"`
	Value BlackboardValue `json:"value"`
}

// FactBool constructs a boolean GOAPFact.
func FactBool(key string, v bool) GOAPFact {
	return GOAPFact{Key: key, Value: BoolValue(v)}
}

// FactFloat constructs a float64 GOAPFact.
func FactFloat(key string, v float64) GOAPFact {
	return GOAPFact{Key: key, Value: FloatValue(v)}
}

// FactInt constructs an int64 GOAPFact.
func FactInt(key string, v int64) GOAPFact {
	return GOAPFact{Key: key, Value: IntValue(v)}
}

// FactString constructs a string GOAPFact.
func FactString(key, v string) GOAPFact {
	return GOAPFact{Key: key, Value: StringValue(v)}
}

func defaultForKind(k ValueKind) BlackboardValue {
	switch k {
	case KindFloat:
		return FloatValue(0)
	case KindInt:
		return IntValue(0)
	case KindString:
		return StringValue("")
	default:
		return BoolValue(false)
	}
}

// FactSatisfied reports whether fact holds in bb (treating absent keys as
// closed-world zero/false/empty defaults).
func FactSatisfied(bb *Blackboard, fact GOAPFact) bool {
	if fact.Key == "" {
		return true
	}
	val, ok := bb.Get(fact.Key)
	if !ok {
		val = defaultForKind(fact.Value.Kind())
	}
	return val.Equal(fact.Value)
}

// FactsSatisfied reports whether every fact in facts holds in bb.
func FactsSatisfied(bb *Blackboard, facts []GOAPFact) bool {
	for _, f := range facts {
		if !FactSatisfied(bb, f) {
			return false
		}
	}
	return true
}

// ApplyFacts writes all facts into bb in deterministic order.
func ApplyFacts(bb *Blackboard, facts []GOAPFact) {
	if bb == nil {
		return
	}
	for _, f := range facts {
		if f.Key != "" && f.Value.Kind() != 0 {
			bb.Set(f.Key, f.Value)
		}
	}
}

// GOAPAction defines a planning operator in Goal-Oriented Action Planning.
//
// An action is applicable in a state when all Preconditions hold and (when
// evaluated for a live Actor) CheckProcedural is nil or returns true. During
// A* planning, applying the action transitions the simulated Blackboard by
// writing Effects at traversal cost Cost (default 1.0 if <= 0).
//
// During execution, Node is evaluated each tick. When Node returns
// StatusSuccess, Effects are committed to self.Blackboard() and the plan
// advances to the next action.
type GOAPAction struct {
	Name            string
	Cost            float64
	Preconditions   []GOAPFact
	Effects         []GOAPFact
	CheckProcedural func(self *Actor, world *World) bool
	Node            BTNode
}

func (a GOAPAction) effectiveCost() float64 {
	if a.Cost <= 0 || math.IsNaN(a.Cost) || math.IsInf(a.Cost, 0) {
		return 1.0
	}
	return a.Cost
}

// GOAPGoal defines a desired target state on an Actor's Blackboard and a
// static Priority or dynamic PriorityFn used to rank competing goals.
type GOAPGoal struct {
	Name       string
	Priority   float64
	PriorityFn func(self *Actor, world *World) float64
	Desired    []GOAPFact
}

// EffectivePriority returns the goal's priority for self in world.
func (g GOAPGoal) EffectivePriority(self *Actor, world *World) float64 {
	if g.PriorityFn != nil {
		p := g.PriorityFn(self, world)
		if !math.IsNaN(p) && !math.IsInf(p, 0) {
			return p
		}
	}
	if g.Priority <= 0 {
		return 1.0
	}
	return g.Priority
}

type goapSearchNode struct {
	state   *Blackboard
	g       float64
	h       float64
	f       float64
	indices []int
}

func goapLess(a, b goapSearchNode) bool {
	if diffF := a.f - b.f; math.Abs(diffF) > 1e-12 {
		return diffF < 0
	}
	if diffH := a.h - b.h; math.Abs(diffH) > 1e-12 {
		return diffH < 0
	}
	minLen := len(a.indices)
	if len(b.indices) < minLen {
		minLen = len(b.indices)
	}
	for i := 0; i < minLen; i++ {
		if a.indices[i] != b.indices[i] {
			return a.indices[i] < b.indices[i]
		}
	}
	return len(a.indices) < len(b.indices)
}

type goapHeap []goapSearchNode

func (h *goapHeap) push(n goapSearchNode) {
	*h = append(*h, n)
	i := len(*h) - 1
	for i > 0 {
		parent := (i - 1) / 2
		if !goapLess((*h)[i], (*h)[parent]) {
			break
		}
		(*h)[i], (*h)[parent] = (*h)[parent], (*h)[i]
		i = parent
	}
}

func (h *goapHeap) pop() goapSearchNode {
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
			if right := left + 1; right < len(*h) && goapLess((*h)[right], (*h)[left]) {
				smallest = right
			}
			if !goapLess((*h)[smallest], (*h)[i]) {
				break
			}
			(*h)[i], (*h)[smallest] = (*h)[smallest], (*h)[i]
			i = smallest
		}
	}
	return top
}

func goapHeuristic(state *Blackboard, desired []GOAPFact, minStepCost float64) float64 {
	unsatisfied := 0
	for _, f := range desired {
		if !FactSatisfied(state, f) {
			unsatisfied++
		}
	}
	return float64(unsatisfied) * minStepCost
}

func blackboardsEqual(a, b *Blackboard) bool {
	if a.Len() != b.Len() {
		return false
	}
	for i := range a.entries {
		if a.entries[i].Key != b.entries[i].Key || !a.entries[i].Value.Equal(b.entries[i].Value) {
			return false
		}
	}
	return true
}

// PlanGOAP synthesizes a deterministic, lowest-cost sequence of GOAPActions to
// transform start into a Blackboard state satisfying goal.Desired using A*.
// If goal.Desired is already satisfied in start, it returns (nil, 0, true).
// If no valid plan exists within maxExpansions, it returns (nil, 0, false).
func PlanGOAP(self *Actor, world *World, start *Blackboard, goal GOAPGoal, actions []GOAPAction, maxExpansions int) ([]GOAPAction, float64, bool) {
	if len(goal.Desired) == 0 || FactsSatisfied(start, goal.Desired) {
		return nil, 0, true
	}
	if len(actions) == 0 {
		return nil, 0, false
	}
	if maxExpansions <= 0 {
		maxExpansions = 2048
	}

	// Collect all relevant keys and their types in deterministic sorted order.
	type keySpec struct {
		key  string
		kind ValueKind
	}
	var keys []keySpec
	addKey := func(k string, kind ValueKind) {
		if k == "" || kind == 0 {
			return
		}
		idx, found := slices.BinarySearchFunc(keys, k, func(e keySpec, target string) int {
			return strings.Compare(e.key, target)
		})
		if !found {
			keys = slices.Insert(keys, idx, keySpec{key: k, kind: kind})
		}
	}
	for _, f := range goal.Desired {
		addKey(f.Key, f.Value.Kind())
	}
	minCost := math.MaxFloat64
	validActions := make([]bool, len(actions))
	for i, act := range actions {
		if act.CheckProcedural != nil && !act.CheckProcedural(self, world) {
			continue
		}
		validActions[i] = true
		if c := act.effectiveCost(); c < minCost {
			minCost = c
		}
		for _, f := range act.Preconditions {
			addKey(f.Key, f.Value.Kind())
		}
		for _, f := range act.Effects {
			addKey(f.Key, f.Value.Kind())
		}
	}
	if minCost == math.MaxFloat64 {
		return nil, 0, false
	}
	// Scale heuristic slightly below minCost so shorter action chains tie-break cleanly.
	minStepCost := minCost * 0.5

	// Project start onto the canonical key set with closed-world defaults.
	rootState := NewBlackboard()
	for _, ks := range keys {
		if v, ok := start.Get(ks.key); ok {
			rootState.Set(ks.key, v)
		} else {
			rootState.Set(ks.key, defaultForKind(ks.kind))
		}
	}

	type visitedEntry struct {
		state *Blackboard
		g     float64
	}
	visited := make(map[uint64][]visitedEntry)
	rootH := goapHeuristic(rootState, goal.Desired, minStepCost)

	var open goapHeap
	open.push(goapSearchNode{
		state: rootState,
		g:     0,
		h:     rootH,
		f:     rootH,
	})
	visited[rootState.Hash()] = []visitedEntry{{state: rootState, g: 0}}

	expansions := 0
	for len(open) > 0 {
		cur := open.pop()
		if FactsSatisfied(cur.state, goal.Desired) {
			plan := make([]GOAPAction, len(cur.indices))
			for i, idx := range cur.indices {
				plan[i] = actions[idx]
			}
			return plan, cur.g, true
		}
		expansions++
		if expansions > maxExpansions {
			return nil, 0, false
		}

		for i, act := range actions {
			if !validActions[i] {
				continue
			}
			if !FactsSatisfied(cur.state, act.Preconditions) {
				continue
			}
			nextState := cur.state.Clone()
			ApplyFacts(nextState, act.Effects)
			if blackboardsEqual(cur.state, nextState) {
				continue
			}

			nextG := cur.g + act.effectiveCost()
			hDigest := nextState.Hash()
			bucket := visited[hDigest]
			dominated := false
			foundIdx := -1
			for bi, ve := range bucket {
				if blackboardsEqual(ve.state, nextState) {
					foundIdx = bi
					if ve.g <= nextG+1e-12 {
						dominated = true
					}
					break
				}
			}
			if dominated {
				continue
			}
			if foundIdx >= 0 {
				bucket[foundIdx].g = nextG
			} else {
				visited[hDigest] = append(bucket, visitedEntry{state: nextState, g: nextG})
			}

			nextIndices := make([]int, len(cur.indices)+1)
			copy(nextIndices, cur.indices)
			nextIndices[len(cur.indices)] = i
			nextH := goapHeuristic(nextState, goal.Desired, minStepCost)
			open.push(goapSearchNode{
				state:   nextState,
				g:       nextG,
				h:       nextH,
				f:       nextG + nextH,
				indices: nextIndices,
			})
		}
	}

	return nil, 0, false
}

// GOAPNode is a BTNode (and Behavior) that runs Goal-Oriented Action Planning
// over an Actor's Blackboard. Each tick it updates any registered Sensors,
// selects the highest-priority unsatisfied GOAPGoal for which a valid action
// plan exists, and evaluates the current GOAPAction's BTNode.
type GOAPNode struct {
	goals      []GOAPGoal
	actions    []GOAPAction
	sensors    []func(self *Actor, world *World)
	activeGoal string
	plan       []GOAPAction
	planNames  []string
	stepIdx    int
	planCost   float64
}

// NewGOAP constructs a GOAPNode with the given goals, actions, and optional
// per-tick Blackboard sensor callbacks.
func NewGOAP(goals []GOAPGoal, actions []GOAPAction, sensors ...func(self *Actor, world *World)) *GOAPNode {
	return &GOAPNode{
		goals:   goals,
		actions: actions,
		sensors: sensors,
	}
}

// ActiveGoal returns the name of the goal currently being pursued.
func (g *GOAPNode) ActiveGoal() string {
	if g == nil {
		return ""
	}
	return g.activeGoal
}

// CurrentPlan returns a copy of the action names in the active plan.
func (g *GOAPNode) CurrentPlan() []string {
	if g == nil || len(g.planNames) == 0 {
		return nil
	}
	return slices.Clone(g.planNames)
}

// CurrentAction returns the name of the action currently being executed.
func (g *GOAPNode) CurrentAction() string {
	if g == nil || g.stepIdx < 0 || g.stepIdx >= len(g.plan) {
		return ""
	}
	return g.plan[g.stepIdx].Name
}

// PlanCost returns the total A* cost of the active plan.
func (g *GOAPNode) PlanCost() float64 {
	if g == nil {
		return 0
	}
	return g.planCost
}

// Evaluate runs sensors, selects/replans for the highest-priority unsatisfied
// goal, and evaluates the active action step.
func (g *GOAPNode) Evaluate(dt float64, self *Actor, world *World) NodeStatus {
	if g == nil || self == nil || len(g.goals) == 0 || len(g.actions) == 0 {
		return StatusFailure
	}
	bb := self.Blackboard()
	for _, s := range g.sensors {
		if s != nil {
			s(self, world)
		}
	}

	// Rank unsatisfied goals by priority descending (tie-breaking by slice index).
	type rankedGoal struct {
		idx      int
		priority float64
		goal     GOAPGoal
	}
	var candidates []rankedGoal
	for i, goal := range g.goals {
		if FactsSatisfied(bb, goal.Desired) {
			continue
		}
		candidates = append(candidates, rankedGoal{
			idx:      i,
			priority: goal.EffectivePriority(self, world),
			goal:     goal,
		})
	}
	slices.SortFunc(candidates, func(a, b rankedGoal) int {
		if a.priority > b.priority {
			return -1
		}
		if a.priority < b.priority {
			return 1
		}
		return a.idx - b.idx
	})

	if len(candidates) == 0 {
		g.clearPlan(bb)
		return StatusSuccess
	}

	// Check if existing plan is still valid for the top unsatisfied goal.
	topGoal := candidates[0].goal
	needPlan := g.activeGoal != topGoal.Name || len(g.plan) == 0 || g.stepIdx >= len(g.plan)
	if !needPlan {
		curAct := g.plan[g.stepIdx]
		if !FactsSatisfied(bb, curAct.Preconditions) || (curAct.CheckProcedural != nil && !curAct.CheckProcedural(self, world)) {
			needPlan = true
		}
	}

	if needPlan {
		g.clearPlan(bb)
		for _, cand := range candidates {
			plan, cost, ok := PlanGOAP(self, world, bb, cand.goal, g.actions, DefaultMaxExpansions)
			if ok && len(plan) > 0 {
				g.activeGoal = cand.goal.Name
				g.plan = plan
				g.planCost = cost
				g.stepIdx = 0
				g.planNames = make([]string, len(plan))
				for i, a := range plan {
					g.planNames[i] = a.Name
				}
				break
			}
		}
	}

	if len(g.plan) == 0 || g.stepIdx >= len(g.plan) {
		return StatusFailure
	}

	curAct := g.plan[g.stepIdx]
	bb.SetString("goap_goal", g.activeGoal)
	bb.SetString("goap_plan", strings.Join(g.planNames, "->"))
	bb.SetString("goap_action", curAct.Name)

	if curAct.Node == nil {
		ApplyFacts(bb, curAct.Effects)
		g.stepIdx++
		return StatusSuccess
	}

	status := curAct.Node.Evaluate(dt, self, world)
	switch status {
	case StatusSuccess:
		ApplyFacts(bb, curAct.Effects)
		g.stepIdx++
		if g.stepIdx >= len(g.plan) {
			return StatusSuccess
		}
		return StatusRunning
	case StatusFailure:
		g.clearPlan(bb)
		return StatusFailure
	default:
		return StatusRunning
	}
}

func (g *GOAPNode) clearPlan(bb *Blackboard) {
	g.activeGoal = ""
	g.plan = nil
	g.planNames = nil
	g.stepIdx = 0
	g.planCost = 0
	if bb != nil {
		bb.Delete("goap_goal")
		bb.Delete("goap_plan")
		bb.Delete("goap_action")
	}
}

// Tick adapts GOAPNode directly to the Behavior interface.
func (g *GOAPNode) Tick(dt float64, self *Actor, world *World) {
	g.Evaluate(dt, self, world)
}
