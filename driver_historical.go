// Copyright (c) 2026 Neomantra Corp.

package ntactor

// HistoricalDriver replays a predetermined sequence of (dt, events) Frames,
// guaranteeing identical inputs to World.Tick across runs.
type HistoricalDriver struct {
	log   EventLog
	index int
}

// NewHistoricalDriver constructs a HistoricalDriver from a recorded or scripted
// EventLog. The log is deep-copied so external mutations do not affect replay.
func NewHistoricalDriver(log EventLog) *HistoricalDriver {
	return &HistoricalDriver{
		log: log.Clone(),
	}
}

// NewHistoricalDriverFromFrames constructs a HistoricalDriver from an explicit
// sequence of Frames.
func NewHistoricalDriverFromFrames(frames ...Frame) *HistoricalDriver {
	return NewHistoricalDriver(EventLog{Frames: frames})
}

// Seed returns the seed stored in the driver's EventLog.
func (d *HistoricalDriver) Seed() uint64 {
	return d.log.Seed
}

// Len returns the total number of frames in the historical sequence.
func (d *HistoricalDriver) Len() int {
	return len(d.log.Frames)
}

// Index returns the index of the next frame to be replayed.
func (d *HistoricalDriver) Index() int {
	return d.index
}

// Remaining returns the number of frames not yet replayed.
func (d *HistoricalDriver) Remaining() int {
	if d.index >= len(d.log.Frames) {
		return 0
	}
	return len(d.log.Frames) - d.index
}

// Done reports whether all frames in the historical sequence have been consumed.
func (d *HistoricalDriver) Done() bool {
	return d.index >= len(d.log.Frames)
}

// Reset rewinds the replay cursor to the first frame.
func (d *HistoricalDriver) Reset() {
	d.index = 0
}

// Log returns a deep copy of the driver's EventLog.
func (d *HistoricalDriver) Log() EventLog {
	return d.log.Clone()
}

// Step returns the next predetermined (dt, events) pair and advances the replay
// cursor. Once Done() is true, Step returns (0, nil).
func (d *HistoricalDriver) Step() (dt float64, events []Event) {
	if d.index >= len(d.log.Frames) {
		return 0, nil
	}
	f := d.log.Frames[d.index].Clone()
	d.index++
	return f.DT, f.Events
}

// Tick consumes the next Frame and calls world.Tick(dt, events).
// If Done() was already true before the call, world.Tick is not called and
// (0, nil) is returned.
func (d *HistoricalDriver) Tick(world *World) (dt float64, events []Event) {
	if d.Done() {
		return 0, nil
	}
	dt, events = d.Step()
	if world != nil {
		world.Tick(dt, events)
	}
	return dt, events
}

// StepWorld advances world by one historical frame and returns true, or returns
// false without calling world.Tick when all frames have been replayed.
func (d *HistoricalDriver) StepWorld(world *World) bool {
	if d.Done() {
		return false
	}
	dt, events := d.Step()
	if world != nil {
		world.Tick(dt, events)
	}
	return true
}

// RunAll replays all remaining frames in order on world.
func (d *HistoricalDriver) RunAll(world *World) {
	for d.StepWorld(world) {
	}
}
