// Copyright (c) 2026 Neomantra Corp.

package ntactor

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
)

const (
	// MaxSpecBytes is the maximum JSON size (256 KiB) accepted by CompileJSON.
	MaxSpecBytes = 256 << 10
	// MaxSpecDepth is the maximum tree depth (32) permitted in a BehaviorSpec.
	MaxSpecDepth = 32
	// MaxSpecNodes is the maximum total node count (512) permitted in a BehaviorSpec.
	MaxSpecNodes = 512
)

// ErrInvalidSpec is returned when a BehaviorSpec fails schema or parameter validation.
var ErrInvalidSpec = errors.New("ntactor: invalid behavior spec")

// ParamSpec describes a single typed parameter accepted by a registered
// condition, action, or consideration primitive.
type ParamSpec struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"` // "float", "int", "bool", "string"
	Required    bool     `json:"required,omitempty"`
	Default     any      `json:"default,omitempty"`
	Min         *float64 `json:"min,omitempty"`
	Max         *float64 `json:"max,omitempty"`
	Description string   `json:"description,omitempty"`
}

// FloatBound returns a pointer to v for setting ParamSpec.Min or ParamSpec.Max.
func FloatBound(v float64) *float64 {
	return &v
}

// PrimitiveDef describes a registered condition, action, or consideration in a
// BehaviorRegistry so planners or editors can inspect the available vocabulary.
type PrimitiveDef struct {
	Name        string      `json:"name"`
	Kind        string      `json:"kind"` // "condition", "action", "consideration"
	Description string      `json:"description"`
	Params      []ParamSpec `json:"params,omitempty"`
}

// RegistryCatalog is the complete, deterministic schema of node types and
// registered primitives supported by a BehaviorRegistry.
type RegistryCatalog struct {
	NodeTypes      []string       `json:"node_types"`
	Conditions     []PrimitiveDef `json:"conditions"`
	Actions        []PrimitiveDef `json:"actions"`
	Considerations []PrimitiveDef `json:"considerations"`
}

// Params wraps validated parameter values passed to a primitive builder.
type Params struct {
	raw map[string]any
}

// Float returns the float64 parameter named key, or fallback if omitted.
func (p Params) Float(key string, fallback float64) float64 {
	v, ok := p.raw[key]
	if !ok || v == nil {
		return fallback
	}
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return fallback
	}
}

// Int returns the int64 parameter named key, or fallback if omitted.
func (p Params) Int(key string, fallback int64) int64 {
	v, ok := p.raw[key]
	if !ok || v == nil {
		return fallback
	}
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	default:
		return fallback
	}
}

// Bool returns the bool parameter named key, or fallback if omitted.
func (p Params) Bool(key string, fallback bool) bool {
	v, ok := p.raw[key]
	if !ok || v == nil {
		return fallback
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return fallback
}

// String returns the string parameter named key, or fallback if omitted.
func (p Params) String(key, fallback string) string {
	v, ok := p.raw[key]
	if !ok || v == nil {
		return fallback
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fallback
}

// ConditionBuilder constructs a Condition predicate from validated Params.
type ConditionBuilder func(p Params) (func(self *Actor, world *World) bool, error)

// ActionBuilder constructs a leaf BTNode from validated Params.
type ActionBuilder func(p Params) (BTNode, error)

// ConsiderationBuilder constructs a Consideration input function from validated Params.
type ConsiderationBuilder func(p Params) (func(self *Actor, world *World) float64, error)

type registeredCondition struct {
	def   PrimitiveDef
	build ConditionBuilder
}

type registeredAction struct {
	def   PrimitiveDef
	build ActionBuilder
}

type registeredConsideration struct {
	def   PrimitiveDef
	build ConsiderationBuilder
}

// BehaviorRegistry holds a deterministic catalog of named Conditions, Actions,
// and Considerations that can be compiled from a declarative BehaviorSpec.
type BehaviorRegistry struct {
	conditions     map[string]registeredCondition
	actions        map[string]registeredAction
	considerations map[string]registeredConsideration
}

// NewBehaviorRegistry constructs an empty BehaviorRegistry.
func NewBehaviorRegistry() *BehaviorRegistry {
	return &BehaviorRegistry{
		conditions:     make(map[string]registeredCondition),
		actions:        make(map[string]registeredAction),
		considerations: make(map[string]registeredConsideration),
	}
}

// RegisterCondition adds or replaces a named condition primitive in the registry.
func (r *BehaviorRegistry) RegisterCondition(def PrimitiveDef, build ConditionBuilder) {
	if r == nil || def.Name == "" || build == nil {
		return
	}
	def.Kind = "condition"
	r.conditions[def.Name] = registeredCondition{def: def, build: build}
}

// RegisterAction adds or replaces a named action primitive in the registry.
func (r *BehaviorRegistry) RegisterAction(def PrimitiveDef, build ActionBuilder) {
	if r == nil || def.Name == "" || build == nil {
		return
	}
	def.Kind = "action"
	r.actions[def.Name] = registeredAction{def: def, build: build}
}

// RegisterConsideration adds or replaces a named utility consideration input in the registry.
func (r *BehaviorRegistry) RegisterConsideration(def PrimitiveDef, build ConsiderationBuilder) {
	if r == nil || def.Name == "" || build == nil {
		return
	}
	def.Kind = "consideration"
	r.considerations[def.Name] = registeredConsideration{def: def, build: build}
}

// Catalog returns a deterministic RegistryCatalog with all primitives sorted by Name.
func (r *BehaviorRegistry) Catalog() RegistryCatalog {
	cat := RegistryCatalog{
		NodeTypes: []string{
			"sequence",
			"mem_sequence",
			"selector",
			"mem_selector",
			"parallel",
			"invert",
			"cooldown",
			"timeout",
			"condition",
			"action",
			"utility",
			"fsm",
			"goap",
		},
	}
	if r == nil {
		return cat
	}
	for _, c := range r.conditions {
		cat.Conditions = append(cat.Conditions, c.def)
	}
	slices.SortFunc(cat.Conditions, func(a, b PrimitiveDef) int {
		return strings.Compare(a.Name, b.Name)
	})
	for _, a := range r.actions {
		cat.Actions = append(cat.Actions, a.def)
	}
	slices.SortFunc(cat.Actions, func(a, b PrimitiveDef) int {
		return strings.Compare(a.Name, b.Name)
	})
	for _, c := range r.considerations {
		cat.Considerations = append(cat.Considerations, c.def)
	}
	slices.SortFunc(cat.Considerations, func(a, b PrimitiveDef) int {
		return strings.Compare(a.Name, b.Name)
	})
	return cat
}

// CatalogJSON serializes r.Catalog() as indented JSON.
func (r *BehaviorRegistry) CatalogJSON() ([]byte, error) {
	return json.MarshalIndent(r.Catalog(), "", "  ")
}

// ConsiderationSpec is the declarative JSON representation of a utility Consideration.
type ConsiderationSpec struct {
	Name   string         `json:"name"`
	Params map[string]any `json:"params,omitempty"`
	Min    float64        `json:"min"`
	Max    float64        `json:"max"`
	Curve  ResponseCurve  `json:"curve,omitempty"`
}

// UtilityOptionSpec is a candidate branch inside a "utility" BehaviorSpec node.
type UtilityOptionSpec struct {
	Name           string              `json:"name"`
	Weight         float64             `json:"weight,omitempty"`
	Considerations []ConsiderationSpec `json:"considerations,omitempty"`
	Child          *BehaviorSpec       `json:"child"`
}

// FSMStateSpec is a declarative state definition inside an "fsm" BehaviorSpec node.
type FSMStateSpec struct {
	ID    StateID       `json:"id"`
	Child *BehaviorSpec `json:"child"`
}

// FSMTransitionSpec is a declarative conditional edge inside an "fsm" BehaviorSpec node.
// A transition fires when Condition (if non-nil) evaluates to StatusSuccess and
// AfterSeconds (if > 0) has elapsed in the active state.
type FSMTransitionSpec struct {
	From         StateID       `json:"from"`
	To           StateID       `json:"to"`
	Condition    *BehaviorSpec `json:"condition,omitempty"`
	AfterSeconds float64       `json:"after_seconds,omitempty"`
}

// GOAPSensorSpec binds a Blackboard boolean key to a Condition BehaviorSpec
// evaluated before each GOAP tick.
type GOAPSensorSpec struct {
	Key       string        `json:"key"`
	Condition *BehaviorSpec `json:"condition"`
}

// GOAPGoalSpec is a declarative goal inside a "goap" BehaviorSpec node.
type GOAPGoalSpec struct {
	Name     string     `json:"name"`
	Priority float64    `json:"priority,omitempty"`
	Desired  []GOAPFact `json:"desired"`
}

// GOAPActionSpec is a declarative planning operator inside a "goap" BehaviorSpec node.
type GOAPActionSpec struct {
	Name          string        `json:"name"`
	Cost          float64       `json:"cost,omitempty"`
	Preconditions []GOAPFact    `json:"preconditions,omitempty"`
	Effects       []GOAPFact    `json:"effects"`
	Condition     *BehaviorSpec `json:"condition,omitempty"`
	Child         *BehaviorSpec `json:"child"`
}

// BehaviorSpec is a declarative, JSON-serializable AST describing a composite
// BehaviorTree, UtilitySelector, FSM, and/or GOAP planner built from registered primitives.
type BehaviorSpec struct {
	Type              string              `json:"type"`
	Name              string              `json:"name,omitempty"`
	Params            map[string]any      `json:"params,omitempty"`
	Seconds           float64             `json:"seconds,omitempty"`
	RequiredSuccesses int                 `json:"required_successes,omitempty"`
	Child             *BehaviorSpec       `json:"child,omitempty"`
	Children          []BehaviorSpec      `json:"children,omitempty"`
	Options           []UtilityOptionSpec `json:"options,omitempty"`
	InitialState      StateID             `json:"initial_state,omitempty"`
	States            []FSMStateSpec      `json:"states,omitempty"`
	Transitions       []FSMTransitionSpec `json:"transitions,omitempty"`
	Sensors           []GOAPSensorSpec    `json:"sensors,omitempty"`
	Goals             []GOAPGoalSpec      `json:"goals,omitempty"`
	Actions           []GOAPActionSpec    `json:"actions,omitempty"`
}

func validateParams(path string, def PrimitiveDef, raw map[string]any) (Params, error) {
	allowed := make(map[string]ParamSpec, len(def.Params))
	for _, ps := range def.Params {
		allowed[ps.Name] = ps
		if ps.Required {
			if _, ok := raw[ps.Name]; !ok {
				return Params{}, fmt.Errorf("%w at %s (%s %q): missing required param %q",
					ErrInvalidSpec, path, def.Kind, def.Name, ps.Name)
			}
		}
	}
	for k, val := range raw {
		ps, ok := allowed[k]
		if !ok {
			var validNames []string
			for _, p := range def.Params {
				validNames = append(validNames, p.Name)
			}
			return Params{}, fmt.Errorf("%w at %s (%s %q): unknown param %q (allowed: %v)",
				ErrInvalidSpec, path, def.Kind, def.Name, k, validNames)
		}
		switch ps.Type {
		case "float":
			var fv float64
			switch n := val.(type) {
			case float64:
				fv = n
			case int:
				fv = float64(n)
			case int64:
				fv = float64(n)
			default:
				return Params{}, fmt.Errorf("%w at %s: param %q must be a float", ErrInvalidSpec, path, k)
			}
			if math.IsNaN(fv) || math.IsInf(fv, 0) {
				return Params{}, fmt.Errorf("%w at %s: param %q must be finite", ErrInvalidSpec, path, k)
			}
			if ps.Min != nil && fv < *ps.Min {
				return Params{}, fmt.Errorf("%w at %s: param %q (%v) is below min %v", ErrInvalidSpec, path, k, fv, *ps.Min)
			}
			if ps.Max != nil && fv > *ps.Max {
				return Params{}, fmt.Errorf("%w at %s: param %q (%v) exceeds max %v", ErrInvalidSpec, path, k, fv, *ps.Max)
			}
		case "int":
			var iv float64
			switch n := val.(type) {
			case float64:
				iv = n
			case int:
				iv = float64(n)
			case int64:
				iv = float64(n)
			default:
				return Params{}, fmt.Errorf("%w at %s: param %q must be an int", ErrInvalidSpec, path, k)
			}
			if ps.Min != nil && iv < *ps.Min {
				return Params{}, fmt.Errorf("%w at %s: param %q (%v) is below min %v", ErrInvalidSpec, path, k, iv, *ps.Min)
			}
			if ps.Max != nil && iv > *ps.Max {
				return Params{}, fmt.Errorf("%w at %s: param %q (%v) exceeds max %v", ErrInvalidSpec, path, k, iv, *ps.Max)
			}
		case "bool":
			if _, ok := val.(bool); !ok {
				return Params{}, fmt.Errorf("%w at %s: param %q must be a bool", ErrInvalidSpec, path, k)
			}
		case "string":
			if _, ok := val.(string); !ok {
				return Params{}, fmt.Errorf("%w at %s: param %q must be a string", ErrInvalidSpec, path, k)
			}
		}
	}
	return Params{raw: raw}, nil
}

type tracedBTNode struct {
	path  string
	typ   string
	name  string
	inner BTNode
	trace *ExecutionTrace
}

func (n tracedBTNode) Evaluate(dt float64, self *Actor, world *World) NodeStatus {
	if n.trace == nil {
		return n.inner.Evaluate(dt, self, world)
	}
	idx := len(n.trace.Entries)
	n.trace.Entries = append(n.trace.Entries, TraceEntry{
		Path: n.path,
		Type: n.typ,
		Name: n.name,
	})
	st := n.inner.Evaluate(dt, self, world)
	if idx < len(n.trace.Entries) {
		e := &n.trace.Entries[idx]
		e.Status = st.String()
		switch inner := n.inner.(type) {
		case *UtilitySelectorNode:
			e.Name = inner.SelectedName()
			e.UtilityOptions = inner.LastTrace()
		case *FSM:
			e.FSMState = inner.Current()
			e.FSMTimeInState = inner.TimeInState()
			if self != nil {
				self.Blackboard().SetString("fsm_state", string(inner.Current()))
			}
		case *GOAPNode:
			e.Name = inner.CurrentAction()
			e.GOAPGoal = inner.ActiveGoal()
			e.GOAPPlan = inner.CurrentPlan()
		}
	}
	return st
}

// ValidateSpec checks that spec conforms to the registry schema, parameter
// types/ranges, and depth/node limits without allocating runtime nodes.
func (r *BehaviorRegistry) ValidateSpec(spec BehaviorSpec) error {
	_, err := r.Compile(spec)
	return err
}

// Compile validates spec against r and compiles it into a ready-to-run *BehaviorTree
// with per-tick ExecutionTrace recording enabled.
func (r *BehaviorRegistry) Compile(spec BehaviorSpec) (*BehaviorTree, error) {
	if r == nil {
		return nil, fmt.Errorf("%w: nil BehaviorRegistry", ErrInvalidSpec)
	}
	bt := &BehaviorTree{}
	nodeCount := 0
	root, err := r.compileNode("root", spec, 0, &nodeCount, &bt.trace)
	if err != nil {
		return nil, err
	}
	bt.root = root
	return bt, nil
}

// CompileJSON parses data as JSON (rejecting unknown fields and oversized inputs)
// and compiles it into a *BehaviorTree.
func (r *BehaviorRegistry) CompileJSON(data []byte) (*BehaviorTree, error) {
	if len(data) == 0 || len(data) > MaxSpecBytes {
		return nil, fmt.Errorf("%w: spec JSON size %d out of range 1..%d", ErrInvalidSpec, len(data), MaxSpecBytes)
	}
	var spec BehaviorSpec
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&spec); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSpec, err)
	}
	return r.Compile(spec)
}

func (r *BehaviorRegistry) compileNode(path string, spec BehaviorSpec, depth int, count *int, trace *ExecutionTrace) (BTNode, error) {
	if depth > MaxSpecDepth {
		return nil, fmt.Errorf("%w at %s: exceeds max depth %d", ErrInvalidSpec, path, MaxSpecDepth)
	}
	*count++
	if *count > MaxSpecNodes {
		return nil, fmt.Errorf("%w at %s: exceeds max nodes %d", ErrInvalidSpec, path, MaxSpecNodes)
	}

	wrap := func(inner BTNode) BTNode {
		if trace == nil {
			return inner
		}
		return tracedBTNode{
			path:  path,
			typ:   spec.Type,
			name:  spec.Name,
			inner: inner,
			trace: trace,
		}
	}

	switch spec.Type {
	case "sequence", "mem_sequence", "selector", "mem_selector", "parallel":
		if len(spec.Children) == 0 {
			return nil, fmt.Errorf("%w at %s: %q requires non-empty children", ErrInvalidSpec, path, spec.Type)
		}
		children := make([]BTNode, len(spec.Children))
		for i, ch := range spec.Children {
			n, err := r.compileNode(fmt.Sprintf("%s.children[%d]", path, i), ch, depth+1, count, trace)
			if err != nil {
				return nil, err
			}
			children[i] = n
		}
		switch spec.Type {
		case "sequence":
			return wrap(Sequence(children...)), nil
		case "mem_sequence":
			return wrap(MemSequence(children...)), nil
		case "selector":
			return wrap(Selector(children...)), nil
		case "mem_selector":
			return wrap(MemSelector(children...)), nil
		default:
			return wrap(Parallel(spec.RequiredSuccesses, children...)), nil
		}

	case "invert":
		if spec.Child == nil {
			return nil, fmt.Errorf("%w at %s: \"invert\" requires a child node", ErrInvalidSpec, path)
		}
		ch, err := r.compileNode(path+".child", *spec.Child, depth+1, count, trace)
		if err != nil {
			return nil, err
		}
		return wrap(Invert(ch)), nil

	case "cooldown", "timeout":
		if spec.Child == nil {
			return nil, fmt.Errorf("%w at %s: %q requires a child node", ErrInvalidSpec, path, spec.Type)
		}
		if spec.Seconds <= 0 || math.IsNaN(spec.Seconds) || math.IsInf(spec.Seconds, 0) {
			return nil, fmt.Errorf("%w at %s: %q requires positive seconds", ErrInvalidSpec, path, spec.Type)
		}
		ch, err := r.compileNode(path+".child", *spec.Child, depth+1, count, trace)
		if err != nil {
			return nil, err
		}
		if spec.Type == "cooldown" {
			return wrap(Cooldown(spec.Seconds, ch)), nil
		}
		return wrap(Timeout(spec.Seconds, ch)), nil

	case "condition":
		reg, ok := r.conditions[spec.Name]
		if !ok {
			var names []string
			for _, c := range r.Catalog().Conditions {
				names = append(names, c.Name)
			}
			return nil, fmt.Errorf("%w at %s: unknown condition %q (available: %v)", ErrInvalidSpec, path, spec.Name, names)
		}
		params, err := validateParams(path, reg.def, spec.Params)
		if err != nil {
			return nil, err
		}
		fn, err := reg.build(params)
		if err != nil {
			return nil, fmt.Errorf("%w at %s: %v", ErrInvalidSpec, path, err)
		}
		return wrap(Condition(fn)), nil

	case "action":
		reg, ok := r.actions[spec.Name]
		if !ok {
			var names []string
			for _, a := range r.Catalog().Actions {
				names = append(names, a.Name)
			}
			return nil, fmt.Errorf("%w at %s: unknown action %q (available: %v)", ErrInvalidSpec, path, spec.Name, names)
		}
		params, err := validateParams(path, reg.def, spec.Params)
		if err != nil {
			return nil, err
		}
		node, err := reg.build(params)
		if err != nil {
			return nil, fmt.Errorf("%w at %s: %v", ErrInvalidSpec, path, err)
		}
		return wrap(node), nil

	case "utility":
		if len(spec.Options) == 0 {
			return nil, fmt.Errorf("%w at %s: \"utility\" requires non-empty options", ErrInvalidSpec, path)
		}
		opts := make([]UtilityOption, len(spec.Options))
		for i, ospec := range spec.Options {
			optPath := fmt.Sprintf("%s.options[%d]", path, i)
			if ospec.Name == "" {
				return nil, fmt.Errorf("%w at %s: utility option requires a name", ErrInvalidSpec, optPath)
			}
			if ospec.Child == nil {
				return nil, fmt.Errorf("%w at %s: utility option %q requires a child node", ErrInvalidSpec, optPath, ospec.Name)
			}
			ch, err := r.compileNode(optPath+".child", *ospec.Child, depth+1, count, trace)
			if err != nil {
				return nil, err
			}
			cons := make([]Consideration, len(ospec.Considerations))
			for j, cs := range ospec.Considerations {
				cPath := fmt.Sprintf("%s.considerations[%d]", optPath, j)
				reg, ok := r.considerations[cs.Name]
				if !ok {
					var names []string
					for _, c := range r.Catalog().Considerations {
						names = append(names, c.Name)
					}
					return nil, fmt.Errorf("%w at %s: unknown consideration %q (available: %v)", ErrInvalidSpec, cPath, cs.Name, names)
				}
				params, err := validateParams(cPath, reg.def, cs.Params)
				if err != nil {
					return nil, err
				}
				inFn, err := reg.build(params)
				if err != nil {
					return nil, fmt.Errorf("%w at %s: %v", ErrInvalidSpec, cPath, err)
				}
				cons[j] = Consideration{
					Name:  cs.Name,
					Input: inFn,
					Min:   cs.Min,
					Max:   cs.Max,
					Curve: cs.Curve,
				}
			}
			opts[i] = UtilityOption{
				Name:           ospec.Name,
				Weight:         ospec.Weight,
				Considerations: cons,
				Node:           ch,
			}
		}
		return wrap(UtilitySelector(opts...)), nil

	case "fsm":
		if len(spec.States) == 0 {
			return nil, fmt.Errorf("%w at %s: \"fsm\" requires non-empty states", ErrInvalidSpec, path)
		}
		initial := spec.InitialState
		if initial == "" {
			initial = spec.States[0].ID
		}
		fsm := NewFSM(initial)
		stateSet := make(map[StateID]bool, len(spec.States))
		for i, st := range spec.States {
			stPath := fmt.Sprintf("%s.states[%d]", path, i)
			if st.ID == "" || st.ID == WildcardState {
				return nil, fmt.Errorf("%w at %s: state requires a valid non-wildcard id", ErrInvalidSpec, stPath)
			}
			if stateSet[st.ID] {
				return nil, fmt.Errorf("%w at %s: duplicate state id %q", ErrInvalidSpec, stPath, st.ID)
			}
			if st.Child == nil {
				return nil, fmt.Errorf("%w at %s: state %q requires a child node", ErrInvalidSpec, stPath, st.ID)
			}
			ch, err := r.compileNode(fmt.Sprintf("%s.states[%s].child", path, st.ID), *st.Child, depth+1, count, trace)
			if err != nil {
				return nil, err
			}
			stateSet[st.ID] = true
			fsm.AddNodeState(st.ID, ch)
		}
		if !stateSet[initial] {
			return nil, fmt.Errorf("%w at %s: initial_state %q not found in states", ErrInvalidSpec, path, initial)
		}
		for i, tr := range spec.Transitions {
			trPath := fmt.Sprintf("%s.transitions[%d]", path, i)
			if tr.From != WildcardState && !stateSet[tr.From] {
				return nil, fmt.Errorf("%w at %s: unknown transition from state %q", ErrInvalidSpec, trPath, tr.From)
			}
			if !stateSet[tr.To] {
				return nil, fmt.Errorf("%w at %s: unknown transition to state %q", ErrInvalidSpec, trPath, tr.To)
			}
			if tr.Condition == nil && tr.AfterSeconds <= 0 {
				return nil, fmt.Errorf("%w at %s: transition requires condition or positive after_seconds", ErrInvalidSpec, trPath)
			}
			if math.IsNaN(tr.AfterSeconds) || math.IsInf(tr.AfterSeconds, 0) || tr.AfterSeconds < 0 {
				return nil, fmt.Errorf("%w at %s: after_seconds must be non-negative finite", ErrInvalidSpec, trPath)
			}
			var condNode BTNode
			if tr.Condition != nil {
				var err error
				condNode, err = r.compileNode(trPath+".condition", *tr.Condition, depth+1, count, nil)
				if err != nil {
					return nil, err
				}
			}
			afterSec := tr.AfterSeconds
			fsm.AddTransition(tr.From, tr.To, func(self *Actor, world *World) bool {
				if afterSec > 0 && fsm.TimeInState() < afterSec {
					return false
				}
				if condNode != nil {
					return condNode.Evaluate(0, self, world) == StatusSuccess
				}
				return true
			})
		}
		return wrap(fsm), nil

	case "goap":
		if len(spec.Goals) == 0 {
			return nil, fmt.Errorf("%w at %s: \"goap\" requires non-empty goals", ErrInvalidSpec, path)
		}
		if len(spec.Actions) == 0 {
			return nil, fmt.Errorf("%w at %s: \"goap\" requires non-empty actions", ErrInvalidSpec, path)
		}
		sensors := make([]func(self *Actor, world *World), len(spec.Sensors))
		for i, sspec := range spec.Sensors {
			sPath := fmt.Sprintf("%s.sensors[%d]", path, i)
			if sspec.Key == "" {
				return nil, fmt.Errorf("%w at %s: sensor requires a key", ErrInvalidSpec, sPath)
			}
			if sspec.Condition == nil {
				return nil, fmt.Errorf("%w at %s: sensor %q requires a condition node", ErrInvalidSpec, sPath, sspec.Key)
			}
			condNode, err := r.compileNode(sPath+".condition", *sspec.Condition, depth+1, count, nil)
			if err != nil {
				return nil, err
			}
			key := sspec.Key
			sensors[i] = func(self *Actor, world *World) {
				res := condNode.Evaluate(0, self, world) == StatusSuccess
				self.Blackboard().SetBool(key, res)
			}
		}
		goals := make([]GOAPGoal, len(spec.Goals))
		for i, gspec := range spec.Goals {
			gPath := fmt.Sprintf("%s.goals[%d]", path, i)
			if gspec.Name == "" {
				return nil, fmt.Errorf("%w at %s: goal requires a name", ErrInvalidSpec, gPath)
			}
			if len(gspec.Desired) == 0 {
				return nil, fmt.Errorf("%w at %s: goal %q requires desired facts", ErrInvalidSpec, gPath, gspec.Name)
			}
			goals[i] = GOAPGoal{
				Name:     gspec.Name,
				Priority: gspec.Priority,
				Desired:  slices.Clone(gspec.Desired),
			}
		}
		actions := make([]GOAPAction, len(spec.Actions))
		for i, aspec := range spec.Actions {
			aPath := fmt.Sprintf("%s.actions[%d]", path, i)
			if aspec.Name == "" {
				return nil, fmt.Errorf("%w at %s: goap action requires a name", ErrInvalidSpec, aPath)
			}
			if len(aspec.Effects) == 0 {
				return nil, fmt.Errorf("%w at %s: goap action %q requires effects", ErrInvalidSpec, aPath, aspec.Name)
			}
			if aspec.Child == nil {
				return nil, fmt.Errorf("%w at %s: goap action %q requires a child node", ErrInvalidSpec, aPath, aspec.Name)
			}
			ch, err := r.compileNode(fmt.Sprintf("%s.actions[%s].child", path, aspec.Name), *aspec.Child, depth+1, count, trace)
			if err != nil {
				return nil, err
			}
			var procFn func(self *Actor, world *World) bool
			if aspec.Condition != nil {
				condNode, err := r.compileNode(aPath+".condition", *aspec.Condition, depth+1, count, nil)
				if err != nil {
					return nil, err
				}
				procFn = func(self *Actor, world *World) bool {
					return condNode.Evaluate(0, self, world) == StatusSuccess
				}
			}
			actions[i] = GOAPAction{
				Name:            aspec.Name,
				Cost:            aspec.Cost,
				Preconditions:   slices.Clone(aspec.Preconditions),
				Effects:         slices.Clone(aspec.Effects),
				CheckProcedural: procFn,
				Node:            ch,
			}
		}
		return wrap(NewGOAP(goals, actions, sensors...)), nil

	default:
		return nil, fmt.Errorf("%w at %s: unknown node type %q", ErrInvalidSpec, path, spec.Type)
	}
}

// StandardRegistryConfig configures optional world/map helpers used by
// NewStandardRegistry's built-in navigation and obstacle avoidance actions.
type StandardRegistryConfig struct {
	GridMap   *GridMap
	FlowField *FlowField
	Obstacles []CircleObstacle
}

// NewStandardRegistry constructs a BehaviorRegistry populated with ntactor's
// built-in perception, blackboard, steering, flocking, and navigation primitives.
func NewStandardRegistry(cfg StandardRegistryConfig) *BehaviorRegistry {
	r := NewBehaviorRegistry()

	// --- Built-in Conditions ---
	r.RegisterCondition(PrimitiveDef{
		Name:        "tag_within_radius",
		Description: "Succeeds when at least one active Actor with the given tag is within radius of self.",
		Params: []ParamSpec{
			{Name: "tag", Type: "string", Required: true, Description: "Target Actor tag"},
			{Name: "radius", Type: "float", Required: true, Min: FloatBound(0.01), Max: FloatBound(10000), Description: "Search radius"},
			{Name: "store_key", Type: "string", Description: "Optional Blackboard key to store the nearest matching ActorID"},
		},
	}, func(p Params) (func(self *Actor, world *World) bool, error) {
		tag := p.String("tag", "")
		radius := p.Float("radius", 10.0)
		storeKey := p.String("store_key", "")
		return func(self *Actor, world *World) bool {
			nearest := nearestByTagWithin(self, world, tag, radius, false)
			if nearest == nil {
				return false
			}
			if storeKey != "" {
				self.Blackboard().SetActorID(storeKey, nearest.ID)
			}
			return true
		}, nil
	})

	r.RegisterCondition(PrimitiveDef{
		Name:        "tag_within_cone",
		Description: "Succeeds when an Actor with tag is inside self's forward vision cone and has line of sight.",
		Params: []ParamSpec{
			{Name: "tag", Type: "string", Required: true, Description: "Target Actor tag"},
			{Name: "radius", Type: "float", Required: true, Min: FloatBound(0.01), Max: FloatBound(10000), Description: "Vision distance"},
			{Name: "half_angle_deg", Type: "float", Default: 45.0, Min: FloatBound(1), Max: FloatBound(180), Description: "Half-angle of vision cone in degrees"},
			{Name: "store_key", Type: "string", Description: "Optional Blackboard key to store matching ActorID"},
		},
	}, func(p Params) (func(self *Actor, world *World) bool, error) {
		tag := p.String("tag", "")
		radius := p.Float("radius", 12.0)
		halfRad := p.Float("half_angle_deg", 45.0) * math.Pi / 180.0
		storeKey := p.String("store_key", "")
		return func(self *Actor, world *World) bool {
			fwd := self.Velocity
			if fwd.LenSq() == 0 {
				fwd = Vec2(1, 0)
			}
			for _, other := range world.ActorsWithinCone(self.Position, fwd, radius, halfRad) {
				if other.ID != self.ID && other.Tag == tag {
					if storeKey != "" {
						self.Blackboard().SetActorID(storeKey, other.ID)
					}
					return true
				}
			}
			return false
		}, nil
	})

	r.RegisterCondition(PrimitiveDef{
		Name:        "has_los_to_tag",
		Description: "Succeeds when an Actor with tag is within radius and unobstructed by World.HasLineOfSight.",
		Params: []ParamSpec{
			{Name: "tag", Type: "string", Required: true, Description: "Target Actor tag"},
			{Name: "radius", Type: "float", Default: 100.0, Min: FloatBound(0.01), Max: FloatBound(10000), Description: "Max search radius"},
		},
	}, func(p Params) (func(self *Actor, world *World) bool, error) {
		tag := p.String("tag", "")
		radius := p.Float("radius", 100.0)
		return func(self *Actor, world *World) bool {
			return nearestByTagWithin(self, world, tag, radius, true) != nil
		}, nil
	})

	r.RegisterCondition(PrimitiveDef{
		Name:        "blackboard_compare",
		Description: "Compares a numeric Blackboard value against value using op (eq, ne, lt, le, gt, ge).",
		Params: []ParamSpec{
			{Name: "key", Type: "string", Required: true, Description: "Blackboard key"},
			{Name: "op", Type: "string", Default: "ge", Description: "Comparison operator: eq, ne, lt, le, gt, ge"},
			{Name: "value", Type: "float", Required: true, Description: "Right-hand comparison value"},
			{Name: "scope", Type: "string", Default: "actor", Description: "'actor' or 'world'"},
		},
	}, func(p Params) (func(self *Actor, world *World) bool, error) {
		key := p.String("key", "")
		op := p.String("op", "ge")
		rhs := p.Float("value", 0)
		scope := p.String("scope", "actor")
		switch op {
		case "eq", "ne", "lt", "le", "gt", "ge":
		default:
			return nil, fmt.Errorf("unsupported op %q (use eq, ne, lt, le, gt, ge)", op)
		}
		return func(self *Actor, world *World) bool {
			bb := self.Blackboard()
			if scope == "world" {
				bb = world.Blackboard()
			}
			lhs := bb.FloatOr(key, 0)
			switch op {
			case "eq":
				return math.Abs(lhs-rhs) <= 1e-9
			case "ne":
				return math.Abs(lhs-rhs) > 1e-9
			case "lt":
				return lhs < rhs
			case "le":
				return lhs <= rhs
			case "gt":
				return lhs > rhs
			default:
				return lhs >= rhs
			}
		}, nil
	})

	r.RegisterCondition(PrimitiveDef{
		Name:        "random_chance",
		Description: "Succeeds with the given probability in [0, 1] using World.ActorRNG(self.ID).",
		Params: []ParamSpec{
			{Name: "probability", Type: "float", Required: true, Min: FloatBound(0), Max: FloatBound(1), Description: "Success probability in [0, 1]"},
		},
	}, func(p Params) (func(self *Actor, world *World) bool, error) {
		prob := p.Float("probability", 0.5)
		return func(self *Actor, world *World) bool {
			return world.ActorRNG(self.ID).Float64() < prob
		}, nil
	})

	// --- Built-in Actions ---
	r.RegisterAction(PrimitiveDef{
		Name:        "arrive_tag",
		Description: "Steers toward the nearest Actor with tag using SteerArrive, optionally capturing/destroying it within capture_radius.",
		Params: []ParamSpec{
			{Name: "tag", Type: "string", Required: true, Description: "Target Actor tag"},
			{Name: "speed", Type: "float", Default: 8.0, Min: FloatBound(0.1), Max: FloatBound(500), Description: "Max speed"},
			{Name: "slow_radius", Type: "float", Default: 3.0, Min: FloatBound(0.01), Max: FloatBound(500), Description: "Deceleration radius"},
			{Name: "max_force", Type: "float", Default: 20.0, Min: FloatBound(0.1), Max: FloatBound(2000), Description: "Max steering force"},
			{Name: "capture_radius", Type: "float", Default: 1.2, Min: FloatBound(0), Max: FloatBound(100), Description: "Distance at which action succeeds"},
			{Name: "destroy_target", Type: "bool", Default: false, Description: "Destroy target Actor on capture"},
			{Name: "score_key", Type: "string", Description: "Optional Actor Blackboard key incremented on capture"},
			{Name: "intent", Type: "string", Description: "Optional Intent Kind emitted on capture"},
		},
	}, func(p Params) (BTNode, error) {
		tag := p.String("tag", "")
		speed := p.Float("speed", 8.0)
		slowRadius := p.Float("slow_radius", 3.0)
		maxForce := p.Float("max_force", 20.0)
		capRad := p.Float("capture_radius", 1.2)
		destroy := p.Bool("destroy_target", false)
		scoreKey := p.String("score_key", "")
		intentKind := p.String("intent", "")
		return Action(func(dt float64, self *Actor, world *World) NodeStatus {
			target := nearestByTagWithin(self, world, tag, 1e9, false)
			if target == nil {
				return StatusFailure
			}
			dist := self.Position.Dist(target.Position)
			self.Blackboard().SetString("status", fmt.Sprintf("arrive %s#%d (%.1fm)", tag, target.ID, dist))
			if capRad > 0 && dist <= capRad {
				onCaptureTarget(self, world, target, destroy, scoreKey, intentKind)
				return StatusSuccess
			}
			desired := SteerArrive(self.Position, target.Position, speed, slowRadius)
			if len(cfg.Obstacles) > 0 {
				desired = desired.Add(SteerAvoidCircles(self, cfg.Obstacles, 2.5, speed)).ClampLen(speed)
			}
			ApplySteering(self, desired, maxForce, speed, dt)
			return StatusRunning
		}), nil
	})

	r.RegisterAction(PrimitiveDef{
		Name:        "pursue_tag",
		Description: "Intercepts the nearest moving Actor with tag using SteerPursue lead prediction.",
		Params: []ParamSpec{
			{Name: "tag", Type: "string", Required: true, Description: "Target Actor tag"},
			{Name: "speed", Type: "float", Default: 10.0, Min: FloatBound(0.1), Max: FloatBound(500), Description: "Pursuit speed"},
			{Name: "max_force", Type: "float", Default: 25.0, Min: FloatBound(0.1), Max: FloatBound(2000), Description: "Max steering force"},
			{Name: "max_prediction", Type: "float", Default: 0.8, Min: FloatBound(0), Max: FloatBound(30), Description: "Max lead prediction time in seconds"},
			{Name: "capture_radius", Type: "float", Default: 1.4, Min: FloatBound(0), Max: FloatBound(100), Description: "Capture radius"},
			{Name: "destroy_target", Type: "bool", Default: false, Description: "Destroy target Actor on capture"},
			{Name: "score_key", Type: "string", Description: "Optional Actor Blackboard key incremented on capture"},
			{Name: "intent", Type: "string", Description: "Optional Intent Kind emitted on capture"},
		},
	}, func(p Params) (BTNode, error) {
		tag := p.String("tag", "")
		speed := p.Float("speed", 10.0)
		maxForce := p.Float("max_force", 25.0)
		maxPred := p.Float("max_prediction", 0.8)
		capRad := p.Float("capture_radius", 1.4)
		destroy := p.Bool("destroy_target", false)
		scoreKey := p.String("score_key", "")
		intentKind := p.String("intent", "")
		return Action(func(dt float64, self *Actor, world *World) NodeStatus {
			target := nearestByTagWithin(self, world, tag, 1e9, false)
			if target == nil {
				return StatusFailure
			}
			dist := self.Position.Dist(target.Position)
			self.Blackboard().SetString("status", fmt.Sprintf("pursue %s#%d (%.1fm)", tag, target.ID, dist))
			if capRad > 0 && dist <= capRad {
				onCaptureTarget(self, world, target, destroy, scoreKey, intentKind)
				return StatusSuccess
			}
			desired := SteerPursue(self, target, maxPred, speed)
			if len(cfg.Obstacles) > 0 {
				desired = desired.Add(SteerAvoidCircles(self, cfg.Obstacles, 2.5, speed)).ClampLen(speed)
			}
			ApplySteering(self, desired, maxForce, speed, dt)
			return StatusRunning
		}), nil
	})

	r.RegisterAction(PrimitiveDef{
		Name:        "evade_tag",
		Description: "Flees predicted trajectory of nearest Actor with tag within radius using SteerEvade.",
		Params: []ParamSpec{
			{Name: "tag", Type: "string", Required: true, Description: "Threat Actor tag"},
			{Name: "radius", Type: "float", Default: 10.0, Min: FloatBound(0.1), Max: FloatBound(10000), Description: "Threat detection radius"},
			{Name: "speed", Type: "float", Default: 9.0, Min: FloatBound(0.1), Max: FloatBound(500), Description: "Evasion speed"},
			{Name: "max_force", Type: "float", Default: 24.0, Min: FloatBound(0.1), Max: FloatBound(2000), Description: "Max steering force"},
			{Name: "max_prediction", Type: "float", Default: 0.7, Min: FloatBound(0), Max: FloatBound(30), Description: "Max prediction horizon"},
		},
	}, func(p Params) (BTNode, error) {
		tag := p.String("tag", "")
		radius := p.Float("radius", 10.0)
		speed := p.Float("speed", 9.0)
		maxForce := p.Float("max_force", 24.0)
		maxPred := p.Float("max_prediction", 0.7)
		return Action(func(dt float64, self *Actor, world *World) NodeStatus {
			threat := nearestByTagWithin(self, world, tag, radius, false)
			if threat == nil {
				return StatusFailure
			}
			self.Blackboard().SetString("status", fmt.Sprintf("evade %s#%d", tag, threat.ID))
			desired := SteerEvade(self, threat, maxPred, speed)
			if len(cfg.Obstacles) > 0 {
				desired = desired.Add(SteerAvoidCircles(self, cfg.Obstacles, 2.5, speed)).ClampLen(speed)
			}
			ApplySteering(self, desired, maxForce, speed, dt)
			return StatusRunning
		}), nil
	})

	r.RegisterAction(PrimitiveDef{
		Name:        "follow_path_to_tag",
		Description: "Plans an A* path over GridMap (if configured) toward the nearest Actor with tag and follows it.",
		Params: []ParamSpec{
			{Name: "tag", Type: "string", Required: true, Description: "Target Actor tag"},
			{Name: "speed", Type: "float", Default: 7.5, Min: FloatBound(0.1), Max: FloatBound(500), Description: "Path-following speed"},
			{Name: "max_force", Type: "float", Default: 28.0, Min: FloatBound(0.1), Max: FloatBound(2000), Description: "Max steering force"},
			{Name: "smooth", Type: "bool", Default: true, Description: "Use line-of-sight string pulling"},
			{Name: "capture_radius", Type: "float", Default: 1.1, Min: FloatBound(0), Max: FloatBound(100), Description: "Capture radius"},
			{Name: "destroy_target", Type: "bool", Default: false, Description: "Destroy target Actor on arrival"},
			{Name: "score_key", Type: "string", Description: "Optional Actor Blackboard key incremented on arrival"},
			{Name: "intent", Type: "string", Description: "Optional Intent Kind emitted on arrival"},
		},
	}, func(p Params) (BTNode, error) {
		tag := p.String("tag", "")
		speed := p.Float("speed", 7.5)
		maxForce := p.Float("max_force", 28.0)
		smooth := p.Bool("smooth", true)
		capRad := p.Float("capture_radius", 1.1)
		destroy := p.Bool("destroy_target", false)
		scoreKey := p.String("score_key", "")
		intentKind := p.String("intent", "")

		follow := &FollowPathBehavior{
			Speed:         speed,
			MaxForce:      maxForce,
			ArrivalRadius: math.Max(0.4, capRad*0.5),
		}
		var goalID ActorID
		var goalPos Vec3
		return Action(func(dt float64, self *Actor, world *World) NodeStatus {
			target := nearestByTagWithin(self, world, tag, 1e9, false)
			if target == nil {
				return StatusFailure
			}
			dist := self.Position.Dist(target.Position)
			self.Blackboard().SetString("status", fmt.Sprintf("A*->%s#%d (%.1fm)", tag, target.ID, dist))
			if capRad > 0 && dist <= capRad {
				onCaptureTarget(self, world, target, destroy, scoreKey, intentKind)
				follow.SetPath(nil)
				goalID = InvalidActorID
				return StatusSuccess
			}
			if cfg.GridMap != nil {
				if goalID != target.ID || target.Position.Dist(goalPos) > cfg.GridMap.CellSize() || follow.Completed() || len(follow.Path) == 0 {
					path, ok := cfg.GridMap.FindWorldPath(self.Position, target.Position, smooth)
					if !ok || len(path) == 0 {
						path = []Vec3{target.Position}
					}
					follow.SetPath(path)
					goalID = target.ID
					goalPos = target.Position
				}
				follow.Tick(dt, self, world)
				return StatusRunning
			}
			desired := SteerArrive(self.Position, target.Position, speed, 2.5)
			ApplySteering(self, desired, maxForce, speed, dt)
			return StatusRunning
		}), nil
	})

	r.RegisterAction(PrimitiveDef{
		Name:        "wander",
		Description: "Wanders deterministically with separation from nearby neighbors and soft boundary return.",
		Params: []ParamSpec{
			{Name: "speed", Type: "float", Default: 6.0, Min: FloatBound(0.1), Max: FloatBound(500), Description: "Wander speed"},
			{Name: "jitter", Type: "float", Default: 0.4, Min: FloatBound(0), Max: FloatBound(50), Description: "Angular wander jitter"},
			{Name: "max_radius", Type: "float", Default: 12.0, Min: FloatBound(0), Max: FloatBound(10000), Description: "Soft containment radius around origin"},
			{Name: "separation_radius", Type: "float", Default: 2.5, Min: FloatBound(0), Max: FloatBound(1000), Description: "Separation radius"},
		},
	}, func(p Params) (BTNode, error) {
		speed := p.Float("speed", 6.0)
		jitter := p.Float("jitter", 0.4)
		maxRad := p.Float("max_radius", 12.0)
		sepRad := p.Float("separation_radius", 2.5)
		return Action(func(dt float64, self *Actor, world *World) NodeStatus {
			rng := world.ActorRNG(self.ID)
			desired := SteerWander(self.Velocity, rng, jitter, speed)
			if sepRad > 0 {
				neighbors := world.Neighbors(self.ID, sepRad)
				sep := SteerSeparation(self, neighbors, sepRad, speed)
				desired = desired.Add(sep.Scale(0.7)).ClampLen(speed)
			}
			if maxRad > 0 && self.Position.Len() > maxRad {
				toCenter := SteerSeek(self.Position, Vec3{}, speed)
				desired = desired.Lerp(toCenter, 0.5)
			}
			if len(cfg.Obstacles) > 0 {
				desired = desired.Add(SteerAvoidCircles(self, cfg.Obstacles, 2.5, speed)).ClampLen(speed)
			}
			ApplySteering(self, desired, speed*3.0, speed, dt)
			self.Blackboard().SetString("status", "wandering")
			return StatusRunning
		}), nil
	})

	r.RegisterAction(PrimitiveDef{
		Name:        "patrol_box",
		Description: "Patrols a rectangular perimeter loop around the origin.",
		Params: []ParamSpec{
			{Name: "half_width", Type: "float", Default: 16.0, Min: FloatBound(0.5), Max: FloatBound(5000), Description: "Half-width (X) of patrol rectangle"},
			{Name: "half_height", Type: "float", Default: 5.0, Min: FloatBound(0.5), Max: FloatBound(5000), Description: "Half-height (Y) of patrol rectangle"},
			{Name: "speed", Type: "float", Default: 7.5, Min: FloatBound(0.1), Max: FloatBound(500), Description: "Patrol speed"},
			{Name: "arrival_radius", Type: "float", Default: 1.0, Min: FloatBound(0.05), Max: FloatBound(100), Description: "Waypoint arrival radius"},
		},
	}, func(p Params) (BTNode, error) {
		hw := p.Float("half_width", 16.0)
		hh := p.Float("half_height", 5.0)
		speed := p.Float("speed", 7.5)
		arr := p.Float("arrival_radius", 1.0)
		patrol := &PatrolBehavior{
			Waypoints: []Vec3{
				Vec2(-hw, -hh),
				Vec2(hw, -hh),
				Vec2(hw, hh),
				Vec2(-hw, hh),
			},
			Speed:         speed,
			ArrivalRadius: arr,
		}
		return Action(func(dt float64, self *Actor, world *World) NodeStatus {
			patrol.Tick(dt, self, world)
			self.Blackboard().SetString("status", fmt.Sprintf("patrol wp#%d", patrol.CurrentIndex+1))
			return StatusRunning
		}), nil
	})

	r.RegisterAction(PrimitiveDef{
		Name:        "flock",
		Description: "Applies Reynolds Boids flocking (Separation + Alignment + Cohesion) with optional attraction/repulsion.",
		Params: []ParamSpec{
			{Name: "neighbor_radius", Type: "float", Default: 5.0, Min: FloatBound(0.1), Max: FloatBound(1000)},
			{Name: "separation_radius", Type: "float", Default: 2.0, Min: FloatBound(0.05), Max: FloatBound(1000)},
			{Name: "separation_weight", Type: "float", Default: 1.7, Min: FloatBound(0), Max: FloatBound(50)},
			{Name: "alignment_weight", Type: "float", Default: 1.1, Min: FloatBound(0), Max: FloatBound(50)},
			{Name: "cohesion_weight", Type: "float", Default: 1.0, Min: FloatBound(0), Max: FloatBound(50)},
			{Name: "max_speed", Type: "float", Default: 7.5, Min: FloatBound(0.1), Max: FloatBound(500)},
			{Name: "max_force", Type: "float", Default: 20.0, Min: FloatBound(0.1), Max: FloatBound(2000)},
			{Name: "attract_tag", Type: "string", Description: "Optional tag to steer toward"},
			{Name: "repel_tag", Type: "string", Description: "Optional tag to flee from"},
			{Name: "bounds_radius", Type: "float", Default: 10.0, Min: FloatBound(0), Max: FloatBound(10000)},
		},
	}, func(p Params) (BTNode, error) {
		fb := &FlockBehavior{
			NeighborRadius:   p.Float("neighbor_radius", 5.0),
			SeparationRadius: p.Float("separation_radius", 2.0),
			SeparationWeight: p.Float("separation_weight", 1.7),
			AlignmentWeight:  p.Float("alignment_weight", 1.1),
			CohesionWeight:   p.Float("cohesion_weight", 1.0),
			WanderJitter:     0.2,
			MaxSpeed:         p.Float("max_speed", 7.5),
			MaxForce:         p.Float("max_force", 20.0),
			SameTagOnly:      true,
			AttractTag:       p.String("attract_tag", ""),
			AttractWeight:    1.3,
			RepelTag:         p.String("repel_tag", ""),
			RepelRadius:      7.0,
			RepelWeight:      3.0,
			BoundsRadius:     p.Float("bounds_radius", 10.0),
		}
		return Action(func(dt float64, self *Actor, world *World) NodeStatus {
			fb.Tick(dt, self, world)
			self.Blackboard().SetString("status", "flocking")
			return StatusRunning
		}), nil
	})

	r.RegisterAction(PrimitiveDef{
		Name:        "follow_leader",
		Description: "Follows a leader Actor in a rotated formation slot (offset_x forward, offset_y lateral).",
		Params: []ParamSpec{
			{Name: "leader_tag", Type: "string", Required: true, Description: "Tag of the leader Actor"},
			{Name: "offset_x", Type: "float", Default: -2.5, Min: FloatBound(-200), Max: FloatBound(200), Description: "Forward slot offset relative to leader heading"},
			{Name: "offset_y", Type: "float", Default: 1.5, Min: FloatBound(-200), Max: FloatBound(200), Description: "Lateral slot offset relative to leader heading"},
			{Name: "speed", Type: "float", Default: 8.5, Min: FloatBound(0.1), Max: FloatBound(500), Description: "Max follower speed"},
			{Name: "max_force", Type: "float", Default: 28.0, Min: FloatBound(0.1), Max: FloatBound(2000), Description: "Max steering force"},
			{Name: "slow_radius", Type: "float", Default: 2.5, Min: FloatBound(0.05), Max: FloatBound(200), Description: "Arrival deceleration radius"},
			{Name: "separation_radius", Type: "float", Default: 2.0, Min: FloatBound(0.05), Max: FloatBound(200), Description: "Squadmate separation radius"},
		},
	}, func(p Params) (BTNode, error) {
		fl := &FollowLeaderBehavior{
			LeaderTag:        p.String("leader_tag", ""),
			SlotOffset:       Vec2(p.Float("offset_x", -2.5), p.Float("offset_y", 1.5)),
			MaxSpeed:         p.Float("speed", 8.5),
			MaxForce:         p.Float("max_force", 28.0),
			SlowRadius:       p.Float("slow_radius", 2.5),
			SeparationRadius: p.Float("separation_radius", 2.0),
		}
		return Action(func(dt float64, self *Actor, world *World) NodeStatus {
			fl.Tick(dt, self, world)
			self.Blackboard().SetString("status", "formation")
			return StatusRunning
		}), nil
	})

	r.RegisterAction(PrimitiveDef{
		Name:        "set_blackboard",
		Description: "Sets a numeric or string value on the Actor or World Blackboard and returns StatusSuccess.",
		Params: []ParamSpec{
			{Name: "key", Type: "string", Required: true, Description: "Blackboard key"},
			{Name: "value", Type: "float", Default: 0.0, Description: "Float value to set"},
			{Name: "text", Type: "string", Description: "Optional string value to set instead of float"},
			{Name: "scope", Type: "string", Default: "actor", Description: "'actor' or 'world'"},
		},
	}, func(p Params) (BTNode, error) {
		key := p.String("key", "")
		val := p.Float("value", 0)
		txt := p.String("text", "")
		scope := p.String("scope", "actor")
		return Action(func(_ float64, self *Actor, world *World) NodeStatus {
			bb := self.Blackboard()
			if scope == "world" {
				bb = world.Blackboard()
			}
			if txt != "" {
				bb.SetString(key, txt)
			} else {
				bb.SetFloat(key, val)
			}
			return StatusSuccess
		}), nil
	})

	r.RegisterAction(PrimitiveDef{
		Name:        "emit_intent",
		Description: "Emits an Intent with kind and optional numeric value and returns StatusSuccess.",
		Params: []ParamSpec{
			{Name: "kind", Type: "string", Required: true, Description: "Intent Kind"},
			{Name: "value", Type: "float", Default: 0.0, Description: "Intent Value"},
		},
	}, func(p Params) (BTNode, error) {
		kind := p.String("kind", "")
		val := p.Float("value", 0)
		return Action(func(_ float64, self *Actor, _ *World) NodeStatus {
			self.Emit(Intent{Kind: kind, Value: val})
			return StatusSuccess
		}), nil
	})

	// --- Built-in Consideration Inputs ---
	r.RegisterConsideration(PrimitiveDef{
		Name:        "nearest_tag_distance",
		Description: "Returns distance to the nearest Actor with tag (or max_radius if none exists).",
		Params: []ParamSpec{
			{Name: "tag", Type: "string", Required: true, Description: "Target Actor tag"},
			{Name: "max_radius", Type: "float", Default: 30.0, Min: FloatBound(0.1), Max: FloatBound(10000), Description: "Fallback distance when no matching Actor exists"},
		},
	}, func(p Params) (func(self *Actor, world *World) float64, error) {
		tag := p.String("tag", "")
		maxRad := p.Float("max_radius", 30.0)
		return func(self *Actor, world *World) float64 {
			t := nearestByTagWithin(self, world, tag, maxRad, false)
			if t == nil {
				return maxRad
			}
			return self.Position.Dist(t.Position)
		}, nil
	})

	r.RegisterConsideration(PrimitiveDef{
		Name:        "tag_count",
		Description: "Returns the number of active Actors in the World with the given tag.",
		Params: []ParamSpec{
			{Name: "tag", Type: "string", Required: true, Description: "Target Actor tag"},
		},
	}, func(p Params) (func(self *Actor, world *World) float64, error) {
		tag := p.String("tag", "")
		return func(_ *Actor, world *World) float64 {
			return float64(len(world.FindByTag(tag)))
		}, nil
	})

	r.RegisterConsideration(PrimitiveDef{
		Name:        "blackboard_float",
		Description: "Returns a numeric value from the Actor or World Blackboard.",
		Params: []ParamSpec{
			{Name: "key", Type: "string", Required: true, Description: "Blackboard key"},
			{Name: "default", Type: "float", Default: 0.0, Description: "Fallback value if absent"},
			{Name: "scope", Type: "string", Default: "actor", Description: "'actor' or 'world'"},
		},
	}, func(p Params) (func(self *Actor, world *World) float64, error) {
		key := p.String("key", "")
		def := p.Float("default", 0)
		scope := p.String("scope", "actor")
		return func(self *Actor, world *World) float64 {
			if scope == "world" {
				return world.Blackboard().FloatOr(key, def)
			}
			return self.Blackboard().FloatOr(key, def)
		}, nil
	})

	r.RegisterConsideration(PrimitiveDef{
		Name:        "distance_from_origin",
		Description: "Returns self.Position.Len() (distance from world origin).",
	}, func(_ Params) (func(self *Actor, world *World) float64, error) {
		return func(self *Actor, _ *World) float64 {
			return self.Position.Len()
		}, nil
	})

	return r
}

func nearestByTagWithin(self *Actor, world *World, tag string, radius float64, requireLOS bool) *Actor {
	if self == nil || world == nil || tag == "" || radius <= 0 {
		return nil
	}
	var best *Actor
	bestSq := radius * radius
	for _, other := range world.FindByTag(tag) {
		if other.ID == self.ID {
			continue
		}
		dSq := self.Position.DistSq(other.Position)
		if dSq <= bestSq {
			if requireLOS && !world.HasLineOfSight(self.Position, other.Position) {
				continue
			}
			best = other
			bestSq = dSq
		}
	}
	return best
}

func onCaptureTarget(self *Actor, world *World, target *Actor, destroy bool, scoreKey, intentKind string) {
	if scoreKey != "" {
		self.Blackboard().AddFloat(scoreKey, 1)
	}
	if intentKind != "" {
		self.Emit(Intent{Kind: intentKind, Target: target.ID})
	}
	if destroy {
		world.Destroy(target.ID)
	}
}

// RegisterSwapHandler registers a deterministic OnEvent handler on world for
// events of kind eventKind (default "set_behavior" if empty).
//
// When an Event{Kind: eventKind, Target: actorID, Payload: specJSON} arrives:
//   - Payload (string or []byte) is compiled via r.CompileJSON
//   - If Target != InvalidActorID, that Actor's Behavior is replaced
//   - Each updated Actor emits Intent{Kind: "behavior_swapped", ActorID: id}
func (r *BehaviorRegistry) RegisterSwapHandler(world *World, eventKind string) {
	if r == nil || world == nil {
		return
	}
	if eventKind == "" {
		eventKind = "set_behavior"
	}
	world.OnEvent(eventKind, func(w *World, ev Event) {
		var raw []byte
		switch p := ev.Payload.(type) {
		case string:
			raw = []byte(p)
		case []byte:
			raw = p
		default:
			return
		}
		if ev.Target != InvalidActorID {
			a := w.ActorByID(ev.Target)
			if a == nil {
				return
			}
			bt, err := r.CompileJSON(raw)
			if err != nil {
				a.Emit(Intent{Kind: "behavior_error", Payload: err.Error()})
				return
			}
			a.Behavior = bt
			a.Blackboard().SetString("behavior_spec", string(raw))
			a.Emit(Intent{Kind: "behavior_swapped", Target: a.ID})
		}
	})
}

// IntentSummary records the total number of Intents emitted for a given Kind
// during a counterfactual rollout.
type IntentSummary struct {
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

// RolloutConfig configures a headless counterfactual rollout evaluation of a
// candidate BehaviorSpec.
type RolloutConfig struct {
	// BuildWorld constructs a fresh World for the given seed.
	BuildWorld func(seed uint64) *World
	// Registry compiles Spec for each matching Actor.
	Registry *BehaviorRegistry
	// Seed is passed to BuildWorld (defaults to 42 if 0).
	Seed uint64
	// TargetActor applies Spec to a specific ActorID when non-zero.
	TargetActor ActorID
	// TargetTag applies Spec to all Actors with this Tag when non-empty.
	TargetTag string
	// Spec is the candidate BehaviorSpec to compile and evaluate.
	Spec BehaviorSpec
	// Ticks is the number of simulation steps to run (defaults to 120 if <= 0).
	Ticks int
	// DT is the step duration in seconds (defaults to 0.05 if <= 0).
	DT float64
	// Events is an optional slice of per-tick event frames to inject during the rollout.
	Events []Frame
	// Score computes a scalar fitness score from the final World and Intent counts.
	Score func(w *World, intents []IntentSummary) float64
}

// RolloutResult holds the deterministic outcome of EvaluateRollout.
type RolloutResult struct {
	Ticks        int              `json:"ticks"`
	SimTime      float64          `json:"sim_time"`
	WorldHash    uint64           `json:"world_hash"`
	Score        float64          `json:"score"`
	IntentCounts []IntentSummary  `json:"intent_counts,omitempty"`
	Traces       []ExecutionTrace `json:"traces,omitempty"`
	Snapshot     WorldSnapshot    `json:"snapshot"`
}

// EvaluateRollout compiles cfg.Spec, installs it on the target Actor(s) in a
// fresh headless World, steps cfg.Ticks frames, and returns a RolloutResult.
func EvaluateRollout(cfg RolloutConfig) (RolloutResult, error) {
	if cfg.BuildWorld == nil {
		return RolloutResult{}, fmt.Errorf("%w: RolloutConfig.BuildWorld is nil", ErrInvalidSpec)
	}
	if cfg.Registry == nil {
		return RolloutResult{}, fmt.Errorf("%w: RolloutConfig.Registry is nil", ErrInvalidSpec)
	}
	if err := cfg.Registry.ValidateSpec(cfg.Spec); err != nil {
		return RolloutResult{}, err
	}
	seed := cfg.Seed
	if seed == 0 {
		seed = 42
	}
	ticks := cfg.Ticks
	if ticks <= 0 {
		ticks = 120
	}
	dt := cfg.DT
	if dt <= 0 {
		dt = 0.05
	}

	w := cfg.BuildWorld(seed)
	for _, a := range w.Actors() {
		match := (cfg.TargetActor != InvalidActorID && a.ID == cfg.TargetActor) ||
			(cfg.TargetTag != "" && a.Tag == cfg.TargetTag)
		if match {
			bt, err := cfg.Registry.Compile(cfg.Spec)
			if err != nil {
				return RolloutResult{}, err
			}
			a.Behavior = bt
		}
	}

	intentTotals := make(map[string]int)
	for i := 0; i < ticks; i++ {
		stepDT := dt
		var stepEvents []Event
		if i < len(cfg.Events) {
			if cfg.Events[i].DT > 0 {
				stepDT = cfg.Events[i].DT
			}
			stepEvents = cfg.Events[i].Events
		}
		w.Tick(stepDT, stepEvents)
		for _, in := range w.Intents() {
			intentTotals[in.Kind]++
		}
	}

	var summaries []IntentSummary
	for k, c := range intentTotals {
		summaries = append(summaries, IntentSummary{Kind: k, Count: c})
	}
	slices.SortFunc(summaries, func(a, b IntentSummary) int {
		return strings.Compare(a.Kind, b.Kind)
	})

	var traces []ExecutionTrace
	for _, a := range w.Actors() {
		match := (cfg.TargetActor != InvalidActorID && a.ID == cfg.TargetActor) ||
			(cfg.TargetTag != "" && a.Tag == cfg.TargetTag)
		if match {
			if tr, ok := TraceActor(a); ok {
				traces = append(traces, tr)
			}
		}
	}

	score := 0.0
	if cfg.Score != nil {
		score = cfg.Score(w, summaries)
	}
	return RolloutResult{
		Ticks:        ticks,
		SimTime:      w.SimTime(),
		WorldHash:    w.Hash(),
		Score:        score,
		IntentCounts: summaries,
		Traces:       traces,
		Snapshot:     w.Snapshot(),
	}, nil
}

// RankedCandidate pairs a named candidate BehaviorSpec with its RolloutResult.
type RankedCandidate struct {
	Name   string        `json:"name"`
	Spec   BehaviorSpec  `json:"spec"`
	Result RolloutResult `json:"result"`
}

// CompareRollouts evaluates multiple named candidate BehaviorSpecs against baseCfg
// in deterministic name order and returns them ranked by Score descending
// (breaking ties by Name ascending).
func CompareRollouts(baseCfg RolloutConfig, candidates map[string]BehaviorSpec) ([]RankedCandidate, error) {
	names := make([]string, 0, len(candidates))
	for name := range candidates {
		names = append(names, name)
	}
	slices.Sort(names)

	out := make([]RankedCandidate, 0, len(names))
	for _, name := range names {
		cfg := baseCfg
		cfg.Spec = candidates[name]
		res, err := EvaluateRollout(cfg)
		if err != nil {
			return nil, fmt.Errorf("candidate %q: %w", name, err)
		}
		out = append(out, RankedCandidate{
			Name:   name,
			Spec:   candidates[name],
			Result: res,
		})
	}

	slices.SortFunc(out, func(a, b RankedCandidate) int {
		if a.Result.Score > b.Result.Score {
			return -1
		}
		if a.Result.Score < b.Result.Score {
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out, nil
}

func cloneParamsMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// CloneSpec returns a deep copy of spec so parameter mutations never alias
// slices, pointers, or parameter maps.
func CloneSpec(spec BehaviorSpec) BehaviorSpec {
	out := spec
	out.Params = cloneParamsMap(spec.Params)
	if spec.Child != nil {
		ch := CloneSpec(*spec.Child)
		out.Child = &ch
	}
	if len(spec.Children) > 0 {
		out.Children = make([]BehaviorSpec, len(spec.Children))
		for i, ch := range spec.Children {
			out.Children[i] = CloneSpec(ch)
		}
	}
	if len(spec.Options) > 0 {
		out.Options = make([]UtilityOptionSpec, len(spec.Options))
		for i, opt := range spec.Options {
			cp := opt
			if opt.Child != nil {
				ch := CloneSpec(*opt.Child)
				cp.Child = &ch
			}
			if len(opt.Considerations) > 0 {
				cp.Considerations = make([]ConsiderationSpec, len(opt.Considerations))
				for j, cs := range opt.Considerations {
					cp.Considerations[j] = ConsiderationSpec{
						Name:   cs.Name,
						Params: cloneParamsMap(cs.Params),
						Min:    cs.Min,
						Max:    cs.Max,
						Curve:  cs.Curve,
					}
				}
			}
			out.Options[i] = cp
		}
	}
	if len(spec.States) > 0 {
		out.States = make([]FSMStateSpec, len(spec.States))
		for i, st := range spec.States {
			cp := st
			if st.Child != nil {
				ch := CloneSpec(*st.Child)
				cp.Child = &ch
			}
			out.States[i] = cp
		}
	}
	if len(spec.Transitions) > 0 {
		out.Transitions = make([]FSMTransitionSpec, len(spec.Transitions))
		for i, tr := range spec.Transitions {
			cp := tr
			if tr.Condition != nil {
				cond := CloneSpec(*tr.Condition)
				cp.Condition = &cond
			}
			out.Transitions[i] = cp
		}
	}
	if len(spec.Sensors) > 0 {
		out.Sensors = make([]GOAPSensorSpec, len(spec.Sensors))
		for i, s := range spec.Sensors {
			cp := s
			if s.Condition != nil {
				cond := CloneSpec(*s.Condition)
				cp.Condition = &cond
			}
			out.Sensors[i] = cp
		}
	}
	if len(spec.Goals) > 0 {
		out.Goals = make([]GOAPGoalSpec, len(spec.Goals))
		for i, g := range spec.Goals {
			out.Goals[i] = GOAPGoalSpec{
				Name:     g.Name,
				Priority: g.Priority,
				Desired:  slices.Clone(g.Desired),
			}
		}
	}
	if len(spec.Actions) > 0 {
		out.Actions = make([]GOAPActionSpec, len(spec.Actions))
		for i, a := range spec.Actions {
			cp := GOAPActionSpec{
				Name:          a.Name,
				Cost:          a.Cost,
				Preconditions: slices.Clone(a.Preconditions),
				Effects:       slices.Clone(a.Effects),
			}
			if a.Condition != nil {
				cond := CloneSpec(*a.Condition)
				cp.Condition = &cond
			}
			if a.Child != nil {
				ch := CloneSpec(*a.Child)
				cp.Child = &ch
			}
			out.Actions[i] = cp
		}
	}
	return out
}

// MutateConfig controls how MutateSpec perturbs numeric parameters in a BehaviorSpec.
type MutateConfig struct {
	// Registry provides parameter bounds (Min, Max, Default) for registered primitives.
	// If nil, NewStandardRegistry(StandardRegistryConfig{}) is used.
	Registry *BehaviorRegistry
	// Probability is the chance in (0, 1] that each tunable parameter is perturbed (default 0.5).
	Probability float64
	// Sigma is the relative perturbation magnitude as a fraction of the current value (default 0.25).
	Sigma float64
}

func roundCent(v float64) float64 {
	return math.Round(v*100.0) / 100.0
}

func perturbFloatBounds(rng *RNG, cur, sigma float64, minB, maxB *float64) float64 {
	scale := math.Max(math.Abs(cur), 0.5)
	next := cur + rng.FloatRange(-1.0, 1.0)*sigma*scale
	if minB != nil && next < *minB {
		next = *minB
	}
	if maxB != nil && next > *maxB {
		next = *maxB
	}
	next = roundCent(next)
	if minB != nil && next < *minB {
		next = *minB
	}
	if maxB != nil && next > *maxB {
		next = *maxB
	}
	return next
}

// MutateSpec returns a deterministically perturbed deep copy of spec using rng.
// Numeric parameters on actions, conditions, considerations, utility option weights,
// decorator durations, and FSM transition timers are perturbed within valid registry bounds.
func MutateSpec(rng *RNG, spec BehaviorSpec, cfg MutateConfig) BehaviorSpec {
	out := CloneSpec(spec)
	if rng == nil {
		return out
	}
	reg := cfg.Registry
	if reg == nil {
		reg = NewStandardRegistry(StandardRegistryConfig{})
	}
	prob := cfg.Probability
	if prob <= 0 || prob > 1 {
		prob = 0.5
	}
	sigma := cfg.Sigma
	if sigma <= 0 {
		sigma = 0.25
	}

	var slots []func()
	collectParamsSlots := func(def PrimitiveDef, paramsPtr *map[string]any) {
		for _, ps := range def.Params {
			if ps.Type != "float" && ps.Type != "int" {
				continue
			}
			psCopy := ps
			pMapPtr := paramsPtr
			slots = append(slots, func() {
				if *pMapPtr == nil {
					*pMapPtr = make(map[string]any)
				}
				p := Params{raw: *pMapPtr}
				defVal := 1.0
				switch d := psCopy.Default.(type) {
				case float64:
					defVal = d
				case int:
					defVal = float64(d)
				}
				cur := p.Float(psCopy.Name, defVal)
				next := perturbFloatBounds(rng, cur, sigma, psCopy.Min, psCopy.Max)
				if psCopy.Type == "int" {
					(*pMapPtr)[psCopy.Name] = int(math.Round(next))
				} else {
					(*pMapPtr)[psCopy.Name] = next
				}
			})
		}
	}

	var walk func(node *BehaviorSpec)
	walk = func(node *BehaviorSpec) {
		if node == nil {
			return
		}
		switch node.Type {
		case "condition":
			if rc, ok := reg.conditions[node.Name]; ok {
				collectParamsSlots(rc.def, &node.Params)
			}
		case "action":
			if ra, ok := reg.actions[node.Name]; ok {
				collectParamsSlots(ra.def, &node.Params)
			}
		case "cooldown", "timeout":
			if node.Seconds > 0 {
				secPtr := &node.Seconds
				slots = append(slots, func() {
					minS, maxS := 0.05, 60.0
					*secPtr = perturbFloatBounds(rng, *secPtr, sigma, &minS, &maxS)
				})
			}
		case "utility":
			for i := range node.Options {
				optPtr := &node.Options[i]
				slots = append(slots, func() {
					curW := optPtr.Weight
					if curW <= 0 {
						curW = 1.0
					}
					minW, maxW := 0.05, 10.0
					optPtr.Weight = perturbFloatBounds(rng, curW, sigma, &minW, &maxW)
				})
				for j := range optPtr.Considerations {
					csPtr := &optPtr.Considerations[j]
					if rc, ok := reg.considerations[csPtr.Name]; ok {
						collectParamsSlots(rc.def, &csPtr.Params)
					}
				}
				walk(optPtr.Child)
			}
		case "fsm":
			for i := range node.States {
				walk(node.States[i].Child)
			}
			for i := range node.Transitions {
				trPtr := &node.Transitions[i]
				if trPtr.AfterSeconds > 0 {
					afterPtr := &trPtr.AfterSeconds
					slots = append(slots, func() {
						minS, maxS := 0.05, 60.0
						*afterPtr = perturbFloatBounds(rng, *afterPtr, sigma, &minS, &maxS)
					})
				}
				walk(trPtr.Condition)
			}
		case "goap":
			for i := range node.Sensors {
				walk(node.Sensors[i].Condition)
			}
			for i := range node.Goals {
				gPtr := &node.Goals[i]
				slots = append(slots, func() {
					curP := gPtr.Priority
					if curP <= 0 {
						curP = 1.0
					}
					minP, maxP := 0.1, 20.0
					gPtr.Priority = perturbFloatBounds(rng, curP, sigma, &minP, &maxP)
				})
			}
			for i := range node.Actions {
				aPtr := &node.Actions[i]
				slots = append(slots, func() {
					curC := aPtr.Cost
					if curC <= 0 {
						curC = 1.0
					}
					minC, maxC := 0.1, 20.0
					aPtr.Cost = perturbFloatBounds(rng, curC, sigma, &minC, &maxC)
				})
				walk(aPtr.Condition)
				walk(aPtr.Child)
			}
		}
		walk(node.Child)
		for i := range node.Children {
			walk(&node.Children[i])
		}
	}

	walk(&out)
	if len(slots) == 0 {
		return out
	}

	mutated := 0
	for _, fn := range slots {
		if rng.Float64() < prob {
			fn()
			mutated++
		}
	}
	if mutated == 0 {
		idx := rng.IntRange(0, len(slots)-1)
		slots[idx]()
	}
	return out
}

// OptimizeConfig configures deterministic evolutionary parameter tuning of a
// seed BehaviorSpec over headless rollouts.
type OptimizeConfig struct {
	// Rollout is the base RolloutConfig used to evaluate each candidate.
	Rollout RolloutConfig
	// Initial is the starting BehaviorSpec (generation 0 baseline).
	Initial BehaviorSpec
	// Generations is the number of evolutionary generations to run (default 4 if <= 0).
	Generations int
	// Population is the number of mutated candidates evaluated per generation (default 6 if <= 0).
	Population int
	// Mutate controls parameter perturbation.
	Mutate MutateConfig
	// Seed seeds the evolutionary search PRNG (defaults to Rollout.Seed if 0).
	Seed uint64
}

// OptimizeResult holds the deterministic outcome of OptimizeSpec across generations.
type OptimizeResult struct {
	InitialScore float64           `json:"initial_score"`
	BestScore    float64           `json:"best_score"`
	BestSpec     BehaviorSpec      `json:"best_spec"`
	BestResult   RolloutResult     `json:"best_result"`
	History      []RankedCandidate `json:"history"`
}

// OptimizeSpec runs a deterministic (1 + lambda) evolutionary strategy using
// MutateSpec and EvaluateRollout to tune spec parameters for maximum Score.
func OptimizeSpec(cfg OptimizeConfig) (OptimizeResult, error) {
	baseCfg := cfg.Rollout
	baseCfg.Spec = CloneSpec(cfg.Initial)
	initRes, err := EvaluateRollout(baseCfg)
	if err != nil {
		return OptimizeResult{}, err
	}

	gens := cfg.Generations
	if gens <= 0 {
		gens = 4
	}
	pop := cfg.Population
	if pop <= 0 {
		pop = 6
	}
	seed := cfg.Seed
	if seed == 0 {
		seed = cfg.Rollout.Seed
		if seed == 0 {
			seed = 42
		}
	}
	mutCfg := cfg.Mutate
	if mutCfg.Registry == nil {
		mutCfg.Registry = cfg.Rollout.Registry
	}

	rng := NewRNG(seed)
	bestSpec := CloneSpec(cfg.Initial)
	bestRes := initRes
	history := []RankedCandidate{
		{Name: "gen0_initial", Spec: CloneSpec(bestSpec), Result: bestRes},
	}

	for g := 1; g <= gens; g++ {
		candidates := make(map[string]BehaviorSpec, pop)
		for i := 0; i < pop; i++ {
			name := fmt.Sprintf("gen%d_cand%d", g, i+1)
			candidates[name] = MutateSpec(rng, bestSpec, mutCfg)
		}
		ranked, err := CompareRollouts(cfg.Rollout, candidates)
		if err != nil {
			return OptimizeResult{}, err
		}
		if len(ranked) > 0 && ranked[0].Result.Score > bestRes.Score {
			bestSpec = CloneSpec(ranked[0].Spec)
			bestRes = ranked[0].Result
			history = append(history, ranked[0])
		}
	}

	return OptimizeResult{
		InitialScore: initRes.Score,
		BestScore:    bestRes.Score,
		BestSpec:     bestSpec,
		BestResult:   bestRes,
		History:      history,
	}, nil
}
