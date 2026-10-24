// Copyright (c) 2026 Neomantra Corp.

package ntactor

import "math"

const (
	defaultStepDT = 1.0 / 60.0
	defaultMaxDT  = 0.1
)

// Frame represents a single recorded or scripted simulation step: the
// deterministic delta time and the external Events injected on that tick.
type Frame struct {
	DT     float64
	Events []Event
}

// Clone returns a deep copy of the Frame's Events slice.
func (f Frame) Clone() Frame {
	var evs []Event
	if len(f.Events) > 0 {
		evs = make([]Event, len(f.Events))
		copy(evs, f.Events)
	}
	return Frame{
		DT:     f.DT,
		Events: evs,
	}
}

// EventLog holds a World seed and an ordered sequence of Frames (dt + events).
// Re-running a World initialized with Seed through Frames via HistoricalDriver
// reproduces the exact simulation outcome without state snapshots.
type EventLog struct {
	Seed   uint64
	Frames []Frame
}

// Append records a frame with dt and a copy of events into the EventLog.
func (l *EventLog) Append(dt float64, events ...Event) {
	var evs []Event
	if len(events) > 0 {
		evs = make([]Event, len(events))
		copy(evs, events)
	}
	l.Frames = append(l.Frames, Frame{
		DT:     dt,
		Events: evs,
	})
}

// Len returns the number of recorded frames in the log.
func (l EventLog) Len() int {
	return len(l.Frames)
}

// Clone returns a deep copy of the EventLog.
func (l EventLog) Clone() EventLog {
	out := EventLog{
		Seed:   l.Seed,
		Frames: make([]Frame, len(l.Frames)),
	}
	for i, f := range l.Frames {
		out.Frames[i] = f.Clone()
	}
	return out
}

// Driver supplies per-tick (dt, events) pairs and can drive World.Tick.
type Driver interface {
	Step() (dt float64, events []Event)
	Tick(world *World) (dt float64, events []Event)
}

// LiveConfig configures a LiveDriver.
type LiveConfig struct {
	// Seed is stored in the recorded EventLog when Record is enabled.
	Seed uint64
	// DefaultDT is used when neither Accumulate nor Clock supplies elapsed time.
	// If <= 0, it defaults to 1/60s.
	DefaultDT float64
	// MinDT is the minimum clamped dt for variable steps.
	MinDT float64
	// MaxDT is the maximum clamped dt for variable steps to prevent spiral-of-death
	// after pauses. If 0, it defaults to 0.1s; set < 0 to disable upper clamping.
	MaxDT float64
	// FixedDT, when > 0, causes accumulated wall time to be consumed in fixed
	// increments of FixedDT.
	FixedDT float64
	// Clock is an optional monotonic time source returning elapsed seconds.
	Clock func() float64
	// Record, when true, logs every produced (dt, events) Frame for later
	// deterministic replay with HistoricalDriver.
	Record bool
}

// LiveDriver accumulates elapsed wall time and live external Events, producing
// clamped or fixed dt values for World.Tick and optionally recording an EventLog.
type LiveDriver struct {
	cfg            LiveConfig
	defaultDT      float64
	maxDT          float64
	accumulated    float64
	hasAccumulated bool
	clockPrimed    bool
	lastClock      float64
	pendingEvents  []Event
	log            EventLog
}

// NewLiveDriver constructs a LiveDriver from cfg.
func NewLiveDriver(cfg LiveConfig) *LiveDriver {
	defDT := cfg.DefaultDT
	if defDT <= 0 {
		if cfg.FixedDT > 0 {
			defDT = cfg.FixedDT
		} else {
			defDT = defaultStepDT
		}
	}
	maxDT := cfg.MaxDT
	if maxDT == 0 {
		maxDT = defaultMaxDT
	}
	return &LiveDriver{
		cfg:       cfg,
		defaultDT: defDT,
		maxDT:     maxDT,
		log:       EventLog{Seed: cfg.Seed},
	}
}

// Accumulate adds elapsed wall-clock seconds to the driver's time accumulator.
// Negative, NaN, or infinite values are ignored.
func (d *LiveDriver) Accumulate(wallSeconds float64) {
	if wallSeconds < 0 || math.IsNaN(wallSeconds) || math.IsInf(wallSeconds, 0) {
		return
	}
	d.accumulated += wallSeconds
	d.hasAccumulated = true
}

// Accumulated returns the unconsumed wall-clock seconds currently in the accumulator.
func (d *LiveDriver) Accumulated() float64 {
	return d.accumulated
}

// PushEvent queues one or more external Events to be emitted on the next Step.
func (d *LiveDriver) PushEvent(events ...Event) {
	d.pendingEvents = append(d.pendingEvents, events...)
}

// Step computes the next simulation dt and drains queued live Events.
// If recording is enabled, the produced (dt, events) frame is appended to the log.
func (d *LiveDriver) Step() (dt float64, events []Event) {
	if d.cfg.Clock != nil {
		now := d.cfg.Clock()
		if !d.clockPrimed {
			d.clockPrimed = true
			d.lastClock = now
		} else {
			delta := now - d.lastClock
			d.lastClock = now
			if delta > 0 && !math.IsNaN(delta) && !math.IsInf(delta, 0) {
				d.accumulated += delta
				d.hasAccumulated = true
			}
		}
	}

	if d.cfg.FixedDT > 0 {
		if d.hasAccumulated {
			if d.accumulated >= d.cfg.FixedDT {
				dt = d.cfg.FixedDT
				d.accumulated -= d.cfg.FixedDT
			} else {
				dt = 0
			}
		} else {
			dt = d.cfg.FixedDT
		}
	} else {
		if d.hasAccumulated {
			dt = d.accumulated
			d.accumulated = 0
			d.hasAccumulated = false
		} else {
			dt = d.defaultDT
		}
		if d.cfg.MinDT > 0 && dt < d.cfg.MinDT {
			dt = d.cfg.MinDT
		}
		if d.maxDT > 0 && dt > d.maxDT {
			dt = d.maxDT
		}
	}

	if len(d.pendingEvents) > 0 {
		events = make([]Event, len(d.pendingEvents))
		copy(events, d.pendingEvents)
		d.pendingEvents = d.pendingEvents[:0]
	}

	if d.cfg.Record {
		d.log.Append(dt, events...)
	}

	return dt, events
}

// Tick calls Step and immediately advances world via world.Tick(dt, events).
func (d *LiveDriver) Tick(world *World) (dt float64, events []Event) {
	dt, events = d.Step()
	if world != nil {
		world.Tick(dt, events)
	}
	return dt, events
}

// Log returns a deep copy of the recorded EventLog.
func (d *LiveDriver) Log() EventLog {
	return d.log.Clone()
}

// SetRecord enables or disables frame recording.
func (d *LiveDriver) SetRecord(record bool) {
	d.cfg.Record = record
}

// ResetLog clears all previously recorded frames while preserving the configured Seed.
func (d *LiveDriver) ResetLog() {
	d.log.Frames = d.log.Frames[:0]
}
