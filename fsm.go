// Copyright (c) 2026 Neomantra Corp.

package ntactor

// StateID names a state within an FSM.
type StateID string

// WildcardState matches any source state when used as Transition.From.
const WildcardState StateID = "*"

// FSMState defines the lifecycle callbacks for a single state in an FSM.
type FSMState struct {
	OnEnter func(self *Actor, world *World)
	OnTick  func(dt float64, self *Actor, world *World)
	OnExit  func(self *Actor, world *World)
	Node    BTNode
}

// Transition defines a directed conditional edge between two states in an FSM.
type Transition struct {
	From         StateID
	To           StateID
	AfterSeconds float64
	Condition    func(self *Actor, world *World) bool
}

// FSM is a deterministic Finite State Machine that implements Behavior.
// Transitions are evaluated in registration order before the active state's
// OnTick callback runs.
type FSM struct {
	current     StateID
	started     bool
	timeInState float64
	transitions uint64
	states      map[StateID]FSMState
	edges       []Transition
}

// NewFSM constructs a Finite State Machine starting in initial.
func NewFSM(initial StateID) *FSM {
	return &FSM{
		current: initial,
		states:  make(map[StateID]FSMState),
	}
}

// AddState registers or replaces a state definition and returns the FSM for chaining.
func (f *FSM) AddState(id StateID, state FSMState) *FSM {
	f.states[id] = state
	return f
}

// AddBehaviorState registers a Behavior as a state's OnTick handler.
func (f *FSM) AddBehaviorState(id StateID, b Behavior) *FSM {
	var tickFn func(dt float64, self *Actor, world *World)
	if b != nil {
		tickFn = b.Tick
	}
	f.states[id] = FSMState{OnTick: tickFn}
	return f
}

// AddNodeState registers a BTNode as a state's evaluation handler.
func (f *FSM) AddNodeState(id StateID, node BTNode) *FSM {
	f.states[id] = FSMState{Node: node}
	return f
}

// AddTransition appends a conditional transition from -> to.
// Transitions are evaluated in the exact order they were added.
func (f *FSM) AddTransition(from, to StateID, cond func(self *Actor, world *World) bool) *FSM {
	f.edges = append(f.edges, Transition{
		From:      from,
		To:        to,
		Condition: cond,
	})
	return f
}

// AddTransitionAfter appends a transition from -> to that requires at least
// afterSeconds in the current state before evaluating cond (if cond is nil, the
// timer alone triggers the transition).
func (f *FSM) AddTransitionAfter(from, to StateID, afterSeconds float64, cond func(self *Actor, world *World) bool) *FSM {
	f.edges = append(f.edges, Transition{
		From:         from,
		To:           to,
		AfterSeconds: afterSeconds,
		Condition:    cond,
	})
	return f
}

// Current returns the active StateID.
func (f *FSM) Current() StateID {
	return f.current
}

// TimeInState returns the simulation time in seconds elapsed since entering
// the current state.
func (f *FSM) TimeInState() float64 {
	return f.timeInState
}

// TransitionCount returns the total number of state transitions executed.
func (f *FSM) TransitionCount() uint64 {
	return f.transitions
}

// SetState immediately transitions to next, invoking the current state's OnExit
// and the new state's OnEnter callbacks.
func (f *FSM) SetState(next StateID, self *Actor, world *World) {
	if f.started && f.current == next {
		return
	}
	if f.started {
		if prev, ok := f.states[f.current]; ok && prev.OnExit != nil {
			prev.OnExit(self, world)
		}
		f.transitions++
	}
	f.current = next
	f.timeInState = 0
	f.started = true
	if cur, ok := f.states[f.current]; ok && cur.OnEnter != nil {
		cur.OnEnter(self, world)
	}
}

// Evaluate evaluates transitions for the active state and runs the active state's
// Node or OnTick callback, adapting FSM to the BTNode interface.
func (f *FSM) Evaluate(dt float64, self *Actor, world *World) NodeStatus {
	if !f.started {
		f.started = true
		if cur, ok := f.states[f.current]; ok && cur.OnEnter != nil {
			cur.OnEnter(self, world)
		}
	}

	for _, edge := range f.edges {
		if edge.From != f.current && edge.From != WildcardState {
			continue
		}
		if edge.To == f.current {
			continue
		}
		if edge.AfterSeconds > 0 && f.timeInState < edge.AfterSeconds {
			continue
		}
		if (edge.Condition == nil && edge.AfterSeconds > 0) || (edge.Condition != nil && edge.Condition(self, world)) {
			f.SetState(edge.To, self, world)
			break
		}
	}

	f.timeInState += dt
	if cur, ok := f.states[f.current]; ok {
		if cur.OnTick != nil {
			cur.OnTick(dt, self, world)
		}
		if cur.Node != nil {
			return cur.Node.Evaluate(dt, self, world)
		}
	}
	return StatusRunning
}

// Tick evaluates transitions for the active state and runs the active state's
// OnTick or Node callback.
func (f *FSM) Tick(dt float64, self *Actor, world *World) {
	f.Evaluate(dt, self, world)
}
