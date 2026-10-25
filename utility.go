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

// ConsiderationTrace records the evaluation breakdown of a single Consideration
// during a UtilitySelector evaluation.
type ConsiderationTrace struct {
	Name       string  `json:"name"`
	RawInput   float64 `json:"raw_input"`
	Normalized float64 `json:"normalized"`
	Score      float64 `json:"score"`
}

// UtilityOptionTrace records the score breakdown of a candidate UtilityOption
// during a UtilitySelector evaluation.
type UtilityOptionTrace struct {
	Name           string               `json:"name"`
	Weight         float64              `json:"weight"`
	Score          float64              `json:"score"`
	Selected       bool                 `json:"selected"`
	Considerations []ConsiderationTrace `json:"considerations,omitempty"`
}

// Consideration evaluates one normalized factor contributing to a UtilityOption's score.
type Consideration struct {
	Name  string
	Input func(self *Actor, world *World) float64
	Min   float64
	Max   float64
	Curve ResponseCurve
}

// EvaluateTrace computes the raw input, normalized [0, 1] value, and response-curve
// utility score for c.
func (c Consideration) EvaluateTrace(self *Actor, world *World) ConsiderationTrace {
	if c.Input == nil {
		return ConsiderationTrace{Name: c.Name}
	}
	raw := c.Input(self, world)
	if math.IsNaN(raw) {
		return ConsiderationTrace{Name: c.Name}
	}
	minV, maxV := c.Min, c.Max
	if maxV <= minV {
		maxV = minV + 1.0
	}
	norm := math.Max(0, math.Min(1, (raw-minV)/(maxV-minV)))
	return ConsiderationTrace{
		Name:       c.Name,
		RawInput:   raw,
		Normalized: norm,
		Score:      c.Curve.Evaluate(norm),
	}
}

// Score normalizes Input(self, world) into [0, 1] using [Min, Max] and applies Curve.
func (c Consideration) Score(self *Actor, world *World) float64 {
	return c.EvaluateTrace(self, world).Score
}

func scoreConsiderationsTrace(self *Actor, world *World, considerations []Consideration) (float64, []ConsiderationTrace) {
	n := len(considerations)
	if n == 0 {
		return 1.0, nil
	}
	traces := make([]ConsiderationTrace, n)
	combined := 1.0
	zeroed := false
	modFactor := 1.0 - (1.0 / float64(n))
	for i, c := range considerations {
		ct := c.EvaluateTrace(self, world)
		traces[i] = ct
		if zeroed {
			continue
		}
		s := ct.Score
		if s <= 0 {
			combined = 0
			zeroed = true
			continue
		}
		makeUp := (1.0 - s) * modFactor
		s = s + (makeUp * s)
		combined *= s
	}
	if zeroed {
		return 0, traces
	}
	return math.Max(0, math.Min(1, combined)), traces
}

// ScoreConsiderations combines multiple Consideration scores in [0, 1] using
// multiplicative scoring with Dave Mark's compensation factor so options with
// more considerations are not unfairly penalized.
func ScoreConsiderations(self *Actor, world *World, considerations []Consideration) float64 {
	score, _ := scoreConsiderationsTrace(self, world, considerations)
	return score
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
	lastTrace     []UtilityOptionTrace
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

// LastTrace returns a copy of the option and consideration score breakdown from
// the most recent Evaluate call.
func (u *UtilitySelectorNode) LastTrace() []UtilityOptionTrace {
	if u == nil || len(u.lastTrace) == 0 {
		return nil
	}
	out := make([]UtilityOptionTrace, len(u.lastTrace))
	for i, t := range u.lastTrace {
		if len(t.Considerations) > 0 {
			cp := make([]ConsiderationTrace, len(t.Considerations))
			copy(cp, t.Considerations)
			t.Considerations = cp
		}
		out[i] = t
	}
	return out
}

// Evaluate scores all options and evaluates the winning option's BTNode.
func (u *UtilitySelectorNode) Evaluate(dt float64, self *Actor, world *World) NodeStatus {
	if u == nil || len(u.options) == 0 {
		return StatusFailure
	}
	if cap(u.lastTrace) < len(u.options) {
		u.lastTrace = make([]UtilityOptionTrace, len(u.options))
	} else {
		u.lastTrace = u.lastTrace[:len(u.options)]
	}
	bestIdx := -1
	bestScore := 0.0
	for i, opt := range u.options {
		w := opt.Weight
		if w <= 0 {
			w = 1.0
		}
		rawCombined, cTraces := scoreConsiderationsTrace(self, world, opt.Considerations)
		score := 0.0
		if opt.Node != nil {
			score = w * rawCombined
		}
		u.lastTrace[i] = UtilityOptionTrace{
			Name:           opt.Name,
			Weight:         w,
			Score:          score,
			Selected:       false,
			Considerations: cTraces,
		}
		if opt.Node != nil && score > bestScore {
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
	u.lastTrace[bestIdx].Selected = true
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
