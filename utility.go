// Copyright (c) 2026 Neomantra Corp.

package ntactor

import "math"

// CurveKind identifies the response curve function used to map a normalized
// [0, 1] consideration input into a [0, 1] utility score.
type CurveKind string

const (
	// CurveLinear maps x in [0, 1] linearly: y = x.
	CurveLinear CurveKind = "linear"
	// CurveQuadratic maps x in [0, 1] via a power curve: y = x^Exponent (default Exponent=2).
	CurveQuadratic CurveKind = "quadratic"
	// CurveSqrt maps x in [0, 1] via square root: y = sqrt(x).
	CurveSqrt CurveKind = "sqrt"
	// CurveLogistic maps x in [0, 1] via a smooth S-curve centered at Midpoint (default 0.5).
	CurveLogistic CurveKind = "logistic"
	// CurveStep maps x < Midpoint to 0 and x >= Midpoint to 1 (default Midpoint=0.5).
	CurveStep CurveKind = "step"
)

// ResponseCurve transforms a normalized input x in [0, 1] into a utility score
// in [0, 1]. When Invert is true, the resulting score is replaced by 1 - score.
type ResponseCurve struct {
	Kind      CurveKind `json:"kind,omitempty"`
	Exponent  float64   `json:"exponent,omitempty"`
	Midpoint  float64   `json:"midpoint,omitempty"`
	Steepness float64   `json:"steepness,omitempty"`
	Invert    bool      `json:"invert,omitempty"`
}

// Evaluate computes the utility score in [0, 1] for normalized input x in [0, 1].
func (c ResponseCurve) Evaluate(x float64) float64 {
	if math.IsNaN(x) {
		return 0
	}
	x = math.Max(0, math.Min(1, x))
	var y float64
	switch c.Kind {
	case "", CurveLinear:
		y = x
	case CurveQuadratic:
		exp := c.Exponent
		if exp <= 0 {
			exp = 2.0
		}
		y = math.Pow(x, exp)
	case CurveSqrt:
		y = math.Sqrt(x)
	case CurveLogistic:
		mid := c.Midpoint
		if mid <= 0 || mid >= 1 {
			mid = 0.5
		}
		k := c.Steepness
		if k <= 0 {
			k = 10.0
		}
		raw := 1.0 / (1.0 + math.Exp(-k*(x-mid)))
		lo := 1.0 / (1.0 + math.Exp(-k*(0-mid)))
		hi := 1.0 / (1.0 + math.Exp(-k*(1-mid)))
		if hi > lo {
			y = (raw - lo) / (hi - lo)
		} else {
			y = raw
		}
	case CurveStep:
		mid := c.Midpoint
		if mid <= 0 || mid >= 1 {
			mid = 0.5
		}
		if x >= mid {
			y = 1.0
		} else {
			y = 0.0
		}
	default:
		y = x
	}
	if c.Invert {
		y = 1.0 - y
	}
	return math.Max(0, math.Min(1, y))
}

// Consideration evaluates one normalized factor contributing to a UtilityOption's score.
type Consideration struct {
	Name  string
	Input func(self *Actor, world *World) float64
	Min   float64
	Max   float64
	Curve ResponseCurve
}

// Score normalizes Input(self, world) into [0, 1] using [Min, Max] and applies Curve.
func (c Consideration) Score(self *Actor, world *World) float64 {
	if c.Input == nil {
		return 0
	}
	raw := c.Input(self, world)
	if math.IsNaN(raw) {
		return 0
	}
	minV, maxV := c.Min, c.Max
	if maxV <= minV {
		maxV = minV + 1.0
	}
	norm := (raw - minV) / (maxV - minV)
	return c.Curve.Evaluate(norm)
}

// ScoreConsiderations combines multiple Consideration scores in [0, 1] using
// multiplicative scoring with Dave Mark's compensation factor so options with
// more considerations are not unfairly penalized.
func ScoreConsiderations(self *Actor, world *World, considerations []Consideration) float64 {
	n := len(considerations)
	if n == 0 {
		return 1.0
	}
	combined := 1.0
	modFactor := 1.0 - (1.0 / float64(n))
	for _, c := range considerations {
		s := c.Score(self, world)
		if s <= 0 {
			return 0
		}
		makeUp := (1.0 - s) * modFactor
		s = s + (makeUp * s)
		combined *= s
	}
	return math.Max(0, math.Min(1, combined))
}

// UtilityOption pairs a candidate BTNode with a base weight and considerations.
type UtilityOption struct {
	Name           string
	Weight         float64
	Considerations []Consideration
	Node           BTNode
}

// UtilitySelectorNode is a BTNode (and Behavior) that scores all candidate
// UtilityOptions each tick in deterministic slice order and evaluates the
// highest-scoring option whose score is > 0.
type UtilitySelectorNode struct {
	options       []UtilityOption
	selectedIndex int
	selectedName  string
	selectedScore float64
}

// UtilitySelector constructs a UtilitySelectorNode from options.
func UtilitySelector(options ...UtilityOption) *UtilitySelectorNode {
	return &UtilitySelectorNode{
		options:       options,
		selectedIndex: -1,
	}
}

// SelectedName returns the Name of the option chosen on the most recent tick.
func (u *UtilitySelectorNode) SelectedName() string {
	if u == nil {
		return ""
	}
	return u.selectedName
}

// SelectedScore returns the utility score of the option chosen on the most recent tick.
func (u *UtilitySelectorNode) SelectedScore() float64 {
	if u == nil {
		return 0
	}
	return u.selectedScore
}

// Evaluate scores all options and evaluates the winning option's BTNode.
func (u *UtilitySelectorNode) Evaluate(dt float64, self *Actor, world *World) NodeStatus {
	if u == nil || len(u.options) == 0 {
		return StatusFailure
	}
	bestIdx := -1
	bestScore := 0.0
	for i, opt := range u.options {
		if opt.Node == nil {
			continue
		}
		w := opt.Weight
		if w <= 0 {
			w = 1.0
		}
		score := w * ScoreConsiderations(self, world, opt.Considerations)
		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}
	if bestIdx < 0 {
		u.selectedIndex = -1
		u.selectedName = ""
		u.selectedScore = 0
		return StatusFailure
	}
	win := u.options[bestIdx]
	u.selectedIndex = bestIdx
	u.selectedName = win.Name
	u.selectedScore = bestScore
	if self != nil {
		self.Blackboard().SetString("utility_choice", win.Name)
		self.Blackboard().SetFloat("utility_score", bestScore)
	}
	return win.Node.Evaluate(dt, self, world)
}

// Tick adapts UtilitySelectorNode directly to the Behavior interface.
func (u *UtilitySelectorNode) Tick(dt float64, self *Actor, world *World) {
	u.Evaluate(dt, self, world)
}
