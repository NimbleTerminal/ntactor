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

// TraceEntry records the evaluation of a single node during a BehaviorTree tick.
type TraceEntry struct {
	Path           string               `json:"path"`
	Type           string               `json:"type"`
	Name           string               `json:"name,omitempty"`
	Status         string               `json:"status"`
	FSMState       StateID              `json:"fsm_state,omitempty"`
	FSMTimeInState float64              `json:"fsm_time_in_state,omitempty"`
	UtilityOptions []UtilityOptionTrace `json:"utility_options,omitempty"`
}

// ExecutionTrace captures the per-tick evaluation trace of a BehaviorTree.
type ExecutionTrace struct {
	Tick       uint64       `json:"tick"`
	SimTime    float64      `json:"sim_time"`
	ActorID    ActorID      `json:"actor_id"`
	RootStatus string       `json:"root_status"`
	ActivePath []string     `json:"active_path,omitempty"`
	Entries    []TraceEntry `json:"entries,omitempty"`
}

// BehaviorTree wraps a root BTNode and adapts it to the Behavior interface.
type BehaviorTree struct {
	root       BTNode
	lastStatus NodeStatus
	trace      ExecutionTrace
}

// NewBehaviorTree constructs a BehaviorTree with the given root node.
func NewBehaviorTree(root BTNode) *BehaviorTree {
	return &BehaviorTree{root: root}
}

// LastStatus returns the NodeStatus produced by the most recent Tick.
func (t *BehaviorTree) LastStatus() NodeStatus {
	return t.lastStatus
}

// LastTrace returns a copy of the ExecutionTrace recorded on the most recent Tick.
func (t *BehaviorTree) LastTrace() ExecutionTrace {
	if t == nil {
		return ExecutionTrace{}
	}
	out := t.trace
	if len(t.trace.ActivePath) > 0 {
		out.ActivePath = make([]string, len(t.trace.ActivePath))
		copy(out.ActivePath, t.trace.ActivePath)
	}
	if len(t.trace.Entries) > 0 {
		out.Entries = make([]TraceEntry, len(t.trace.Entries))
		copy(out.Entries, t.trace.Entries)
	}
	return out
}

// TraceActor extracts the most recent ExecutionTrace from a's Behavior if a is
// driven by a *BehaviorTree.
func TraceActor(a *Actor) (ExecutionTrace, bool) {
	if a == nil || a.Behavior == nil {
		return ExecutionTrace{}, false
	}
	bt, ok := a.Behavior.(*BehaviorTree)
	if !ok || bt == nil {
		return ExecutionTrace{}, false
	}
	return bt.LastTrace(), true
}

// Tick evaluates the root node for the current simulation tick.
func (t *BehaviorTree) Tick(dt float64, self *Actor, world *World) {
	t.trace.Entries = t.trace.Entries[:0]
	t.trace.ActivePath = t.trace.ActivePath[:0]
	if world != nil {
		t.trace.Tick = world.TickCount()
		t.trace.SimTime = world.SimTime()
	}
	if self != nil {
		t.trace.ActorID = self.ID
	}
	if t.root == nil {
		t.lastStatus = StatusFailure
		t.trace.RootStatus = StatusFailure.String()
		return
	}
	t.lastStatus = t.root.Evaluate(dt, self, world)
	t.trace.RootStatus = t.lastStatus.String()
	if len(t.trace.Entries) == 0 {
		switch r := t.root.(type) {
		case *UtilitySelectorNode:
			t.trace.Entries = append(t.trace.Entries, TraceEntry{
				Path:           "root",
				Type:           "utility",
				Name:           r.SelectedName(),
				Status:         t.lastStatus.String(),
				UtilityOptions: r.LastTrace(),
			})
		case *FSM:
			t.trace.Entries = append(t.trace.Entries, TraceEntry{
				Path:           "root",
				Type:           "fsm",
				Status:         t.lastStatus.String(),
				FSMState:       r.Current(),
				FSMTimeInState: r.TimeInState(),
			})
		default:
			t.trace.Entries = append(t.trace.Entries, TraceEntry{
				Path:   "root",
				Type:   "bt",
				Status: t.lastStatus.String(),
			})
		}
	}
	for _, e := range t.trace.Entries {
		if e.Status == StatusFailure.String() {
			continue
		}
		switch {
		case e.Type == "utility" && e.Name != "":
			t.trace.ActivePath = append(t.trace.ActivePath, "utility->"+e.Name)
		case e.Type == "fsm" && e.FSMState != "":
			t.trace.ActivePath = append(t.trace.ActivePath, "fsm:"+string(e.FSMState))
		case e.Name != "":
			t.trace.ActivePath = append(t.trace.ActivePath, e.Type+":"+e.Name)
		default:
			t.trace.ActivePath = append(t.trace.ActivePath, e.Type)
		}
	}
}

type sequenceNode struct {
	children []BTNode
}

// Sequence constructs a composite BTNode that evaluates its children in order
// from index 0 on every tick. It returns StatusFailure or StatusRunning on the
// first child that does not succeed, and StatusSuccess if all children succeed.
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

type memSequenceNode struct {
	children []BTNode
	running  int
}

// MemSequence constructs a stateful sequence BTNode that remembers which child
// returned StatusRunning and resumes from that child on subsequent ticks.
// When the sequence completes with StatusSuccess or StatusFailure, the cursor
// resets to index 0.
func MemSequence(children ...BTNode) BTNode {
	return &memSequenceNode{children: children}
}

func (n *memSequenceNode) Evaluate(dt float64, self *Actor, world *World) NodeStatus {
	for i := n.running; i < len(n.children); i++ {
		child := n.children[i]
		if child == nil {
			continue
		}
		status := child.Evaluate(dt, self, world)
		if status == StatusRunning {
			n.running = i
			return StatusRunning
		}
		if status == StatusFailure {
			n.running = 0
			return StatusFailure
		}
	}
	n.running = 0
	return StatusSuccess
}

type selectorNode struct {
	children []BTNode
}

// Selector constructs a composite BTNode that evaluates its children in order
// from index 0 until one succeeds or is running. It returns StatusFailure only
// if all children return StatusFailure.
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

type memSelectorNode struct {
	children []BTNode
	running  int
}

// MemSelector constructs a stateful selector BTNode that remembers which child
// returned StatusRunning and resumes from that child on subsequent ticks.
// When a child returns StatusSuccess or all remaining children fail, the cursor
// resets to index 0.
func MemSelector(children ...BTNode) BTNode {
	return &memSelectorNode{children: children}
}

func (n *memSelectorNode) Evaluate(dt float64, self *Actor, world *World) NodeStatus {
	for i := n.running; i < len(n.children); i++ {
		child := n.children[i]
		if child == nil {
			continue
		}
		status := child.Evaluate(dt, self, world)
		if status == StatusRunning {
			n.running = i
			return StatusRunning
		}
		if status == StatusSuccess {
			n.running = 0
			return StatusSuccess
		}
	}
	n.running = 0
	return StatusFailure
}

type parallelNode struct {
	required int
	children []BTNode
}

// Parallel constructs a composite BTNode that evaluates all non-nil children on
// every tick. It returns StatusSuccess when at least requiredSuccesses children
// succeed, StatusFailure if too many children fail to reach requiredSuccesses,
// and StatusRunning otherwise.
func Parallel(requiredSuccesses int, children ...BTNode) BTNode {
	return parallelNode{
		required: requiredSuccesses,
		children: children,
	}
}

func (n parallelNode) Evaluate(dt float64, self *Actor, world *World) NodeStatus {
	valid := 0
	succeeded := 0
	failed := 0
	for _, child := range n.children {
		if child == nil {
			continue
		}
		valid++
		switch child.Evaluate(dt, self, world) {
		case StatusSuccess:
			succeeded++
		case StatusFailure:
			failed++
		}
	}
	req := n.required
	if req <= 0 {
		req = valid
	}
	if succeeded >= req {
		return StatusSuccess
	}
	if valid-failed < req {
		return StatusFailure
	}
	return StatusRunning
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

type cooldownNode struct {
	duration  float64
	remaining float64
	child     BTNode
}

// Cooldown constructs a stateful decorator BTNode that blocks re-evaluation of
// child (returning StatusFailure) for duration simulation seconds after child
// returns StatusSuccess.
func Cooldown(duration float64, child BTNode) BTNode {
	return &cooldownNode{
		duration: duration,
		child:    child,
	}
}

func (n *cooldownNode) Evaluate(dt float64, self *Actor, world *World) NodeStatus {
	if n.child == nil {
		return StatusFailure
	}
	if n.remaining > 0 {
		n.remaining -= dt
		if n.remaining > 0 {
			return StatusFailure
		}
		n.remaining = 0
	}
	status := n.child.Evaluate(dt, self, world)
	if status == StatusSuccess && n.duration > 0 {
		n.remaining = n.duration
	}
	return status
}

type timeoutNode struct {
	maxDuration float64
	elapsed     float64
	child       BTNode
}

// Timeout constructs a stateful decorator BTNode that fails child if it remains
// in StatusRunning for more than maxDuration simulation seconds.
func Timeout(maxDuration float64, child BTNode) BTNode {
	return &timeoutNode{
		maxDuration: maxDuration,
		child:       child,
	}
}

func (n *timeoutNode) Evaluate(dt float64, self *Actor, world *World) NodeStatus {
	if n.child == nil {
		return StatusFailure
	}
	status := n.child.Evaluate(dt, self, world)
	if status != StatusRunning {
		n.elapsed = 0
		return status
	}
	n.elapsed += dt
	if n.maxDuration > 0 && n.elapsed >= n.maxDuration {
		n.elapsed = 0
		return StatusFailure
	}
	return StatusRunning
}
