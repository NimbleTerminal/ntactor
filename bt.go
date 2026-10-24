// Copyright (c) 2026 Neomantra Corp.

package ntactor

// NodeStatus is the evaluation outcome of a Behavior Tree node on a given tick.
type NodeStatus uint8

const (
	// StatusFailure indicates the node failed its condition or task.
	StatusFailure NodeStatus = iota
	// StatusSuccess indicates the node completed its task or satisfied its condition.
	StatusSuccess
	// StatusRunning indicates the node is still in progress and should continue next tick.
	StatusRunning
)

// String returns a human-readable name for the NodeStatus.
func (s NodeStatus) String() string {
	switch s {
	case StatusFailure:
		return "failure"
	case StatusSuccess:
		return "success"
	case StatusRunning:
		return "running"
	default:
		return "unknown"
	}
}

// BTNode is a single node in a Behavior Tree.
type BTNode interface {
	Evaluate(dt float64, self *Actor, world *World) NodeStatus
}

// BehaviorTree wraps a root BTNode and adapts it to the Behavior interface.
type BehaviorTree struct {
	root       BTNode
	lastStatus NodeStatus
}

// NewBehaviorTree constructs a BehaviorTree with the given root node.
func NewBehaviorTree(root BTNode) *BehaviorTree {
	return &BehaviorTree{root: root}
}

// LastStatus returns the NodeStatus produced by the most recent Tick.
func (t *BehaviorTree) LastStatus() NodeStatus {
	return t.lastStatus
}

// Tick evaluates the root node for the current simulation tick.
func (t *BehaviorTree) Tick(dt float64, self *Actor, world *World) {
	if t.root == nil {
		t.lastStatus = StatusFailure
		return
	}
	t.lastStatus = t.root.Evaluate(dt, self, world)
}

type sequenceNode struct {
	children []BTNode
}

// Sequence constructs a composite BTNode that evaluates its children in order.
// It returns StatusFailure or StatusRunning on the first child that does not
// succeed, and StatusSuccess if all children return StatusSuccess.
func Sequence(children ...BTNode) BTNode {
	return sequenceNode{children: children}
}

func (n sequenceNode) Evaluate(dt float64, self *Actor, world *World) NodeStatus {
	for _, child := range n.children {
		if child == nil {
			continue
		}
		status := child.Evaluate(dt, self, world)
		if status != StatusSuccess {
			return status
		}
	}
	return StatusSuccess
}

type selectorNode struct {
	children []BTNode
}

// Selector constructs a composite BTNode that evaluates its children in order
// until one succeeds or is running. It returns StatusFailure only if all
// children return StatusFailure.
func Selector(children ...BTNode) BTNode {
	return selectorNode{children: children}
}

func (n selectorNode) Evaluate(dt float64, self *Actor, world *World) NodeStatus {
	for _, child := range n.children {
		if child == nil {
			continue
		}
		status := child.Evaluate(dt, self, world)
		if status != StatusFailure {
			return status
		}
	}
	return StatusFailure
}

type conditionNode struct {
	fn func(self *Actor, world *World) bool
}

// Condition constructs a leaf BTNode that returns StatusSuccess when fn returns
// true and StatusFailure when fn returns false.
func Condition(fn func(self *Actor, world *World) bool) BTNode {
	return conditionNode{fn: fn}
}

func (n conditionNode) Evaluate(_ float64, self *Actor, world *World) NodeStatus {
	if n.fn != nil && n.fn(self, world) {
		return StatusSuccess
	}
	return StatusFailure
}

type actionNode struct {
	fn func(dt float64, self *Actor, world *World) NodeStatus
}

// Action constructs a leaf BTNode that executes fn on each evaluation.
func Action(fn func(dt float64, self *Actor, world *World) NodeStatus) BTNode {
	return actionNode{fn: fn}
}

func (n actionNode) Evaluate(dt float64, self *Actor, world *World) NodeStatus {
	if n.fn == nil {
		return StatusFailure
	}
	return n.fn(dt, self, world)
}

type invertNode struct {
	child BTNode
}

// Invert constructs a decorator BTNode that swaps StatusSuccess and
// StatusFailure while leaving StatusRunning unchanged.
func Invert(child BTNode) BTNode {
	return invertNode{child: child}
}

func (n invertNode) Evaluate(dt float64, self *Actor, world *World) NodeStatus {
	if n.child == nil {
		return StatusFailure
	}
	switch n.child.Evaluate(dt, self, world) {
	case StatusSuccess:
		return StatusFailure
	case StatusFailure:
		return StatusSuccess
	default:
		return StatusRunning
	}
}
