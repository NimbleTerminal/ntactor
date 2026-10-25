// Copyright (c) 2026 Neomantra Corp.

package ntactor

import (
	"math"
	"slices"
)

// SenseKind identifies the sensory modality that produced or refreshed a MemoryRecord.
type SenseKind string

const (
	// SenseVision indicates line-of-sight or vision-cone detection.
	SenseVision SenseKind = "vision"
	// SenseRadius indicates proximity/radar detection.
	SenseRadius SenseKind = "radius"
	// SenseSound indicates an acoustic stimulus (such as footsteps or alerts).
	SenseSound SenseKind = "sound"
	// SenseDamage indicates a direct hit/damage stimulus.
	SenseDamage SenseKind = "damage"
)

// MemoryRecord tracks an Actor's perception of a target Actor over time,
// retaining its last known position and velocity even after line of sight is
// lost until Confidence decays to 0.
type MemoryRecord struct {
	TargetID          ActorID   `json:"target_id"`
	Tag               string    `json:"tag"`
	LastKnownPosition Vec3      `json:"last_known_position"`
	LastKnownVelocity Vec3      `json:"last_known_velocity"`
	LastSeenTime      float64   `json:"last_seen_time"`
	Age               float64   `json:"age"`
	Confidence        float64   `json:"confidence"`
	Sense             SenseKind `json:"sense"`
	Visible           bool      `json:"visible"`
}

// ExtrapolatedPosition projects LastKnownPosition forward along LastKnownVelocity
// by min(Age, maxLeadSeconds).
func (r MemoryRecord) ExtrapolatedPosition(maxLeadSeconds float64) Vec3 {
	if maxLeadSeconds <= 0 || r.Age <= 0 {
		return r.LastKnownPosition
	}
	t := r.Age
	if t > maxLeadSeconds {
		t = maxLeadSeconds
	}
	return r.LastKnownPosition.Add(r.LastKnownVelocity.Scale(t))
}

// SensoryMemory is a deterministic, ActorID-sorted store of MemoryRecords owned
// by an Actor.
type SensoryMemory struct {
	records []MemoryRecord
}

// NewSensoryMemory constructs an empty SensoryMemory.
func NewSensoryMemory() *SensoryMemory {
	return &SensoryMemory{}
}

func (m *SensoryMemory) find(id ActorID) (int, bool) {
	return slices.BinarySearchFunc(m.records, id, func(r MemoryRecord, target ActorID) int {
		if r.TargetID < target {
			return -1
		}
		if r.TargetID > target {
			return 1
		}
		return 0
	})
}

// Observe records or refreshes a perception of target on m.
func (m *SensoryMemory) Observe(target *Actor, sense SenseKind, confidence, simTime float64, visible bool) {
	if m == nil || target == nil || target.ID == InvalidActorID {
		return
	}
	conf := clamp01(confidence)
	if conf <= 0 {
		return
	}
	if sense == "" {
		sense = SenseVision
	}
	rec := MemoryRecord{
		TargetID:          target.ID,
		Tag:               target.Tag,
		LastKnownPosition: target.Position,
		LastKnownVelocity: target.Velocity,
		LastSeenTime:      simTime,
		Age:               0,
		Confidence:        conf,
		Sense:             sense,
		Visible:           visible,
	}
	idx, found := m.find(target.ID)
	if found {
		m.records[idx] = rec
	} else {
		m.records = slices.Insert(m.records, idx, rec)
	}
}

// ObserveStimulus records a positional sensory stimulus (e.g. sound or alert)
// from targetID without requiring direct visibility.
func (m *SensoryMemory) ObserveStimulus(targetID ActorID, tag string, pos Vec3, sense SenseKind, confidence, simTime float64) {
	if m == nil || targetID == InvalidActorID {
		return
	}
	conf := clamp01(confidence)
	if conf <= 0 {
		return
	}
	if sense == "" {
		sense = SenseSound
	}
	idx, found := m.find(targetID)
	if found {
		prev := m.records[idx]
		// Do not overwrite higher-confidence active visual contact with a weaker sound.
		if prev.Visible && prev.Confidence > conf {
			return
		}
		if conf < prev.Confidence {
			conf = prev.Confidence
		}
		m.records[idx] = MemoryRecord{
			TargetID:          targetID,
			Tag:               tag,
			LastKnownPosition: pos,
			LastKnownVelocity: prev.LastKnownVelocity,
			LastSeenTime:      simTime,
			Age:               0,
			Confidence:        conf,
			Sense:             sense,
			Visible:           false,
		}
		return
	}
	m.records = slices.Insert(m.records, idx, MemoryRecord{
		TargetID:          targetID,
		Tag:               tag,
		LastKnownPosition: pos,
		LastSeenTime:      simTime,
		Age:               0,
		Confidence:        conf,
		Sense:             sense,
		Visible:           false,
	})
}

// Step advances Age by dt, resets Visible to false (prior to current-tick sensor
// scans), decays Confidence by decayRate*dt, and prunes expired records.
func (m *SensoryMemory) Step(dt, decayRate float64) {
	if m == nil || len(m.records) == 0 {
		return
	}
	if dt < 0 || math.IsNaN(dt) || math.IsInf(dt, 0) {
		dt = 0
	}
	if decayRate < 0 || math.IsNaN(decayRate) || math.IsInf(decayRate, 0) {
		decayRate = 0
	}
	write := 0
	for i := range m.records {
		r := m.records[i]
		r.Age += dt
		r.Visible = false
		r.Confidence -= decayRate * dt
		if r.Confidence > 1e-9 {
			m.records[write] = r
			write++
		}
	}
	clear(m.records[write:])
	m.records = m.records[:write]
}

// PerceiveVisionCone steps existing memories by (dt, decayRate) and refreshes
// all active Actors inside self's forward vision cone (which checks
// World.HasLineOfSight).
func (m *SensoryMemory) PerceiveVisionCone(self *Actor, world *World, radius, halfAngleRad, decayRate, dt float64) {
	if m == nil || self == nil || world == nil {
		return
	}
	m.Step(dt, decayRate)
	fwd := self.Velocity
	if fwd.LenSq() == 0 {
		fwd = Vec2(1, 0)
	}
	for _, other := range world.ActorsWithinCone(self.Position, fwd, radius, halfAngleRad) {
		if other.ID != self.ID {
			m.Observe(other, SenseVision, 1.0, world.SimTime(), true)
		}
	}
}

// PerceiveRadius steps existing memories by (dt, decayRate) and refreshes all
// active Actors within radius of self (checking World.HasLineOfSight when
// requireLOS is true).
func (m *SensoryMemory) PerceiveRadius(self *Actor, world *World, radius float64, requireLOS bool, decayRate, dt float64) {
	if m == nil || self == nil || world == nil {
		return
	}
	m.Step(dt, decayRate)
	sense := SenseRadius
	if requireLOS {
		sense = SenseVision
	}
	for _, other := range world.Neighbors(self.ID, radius) {
		if requireLOS && !world.HasLineOfSight(self.Position, other.Position) {
			continue
		}
		m.Observe(other, sense, 1.0, world.SimTime(), true)
	}
}

// Get looks up the MemoryRecord for targetID.
func (m *SensoryMemory) Get(targetID ActorID) (MemoryRecord, bool) {
	if m == nil || len(m.records) == 0 {
		return MemoryRecord{}, false
	}
	idx, found := m.find(targetID)
	if !found {
		return MemoryRecord{}, false
	}
	return m.records[idx], true
}

// Forget removes the MemoryRecord for targetID and returns true if it existed.
func (m *SensoryMemory) Forget(targetID ActorID) bool {
	if m == nil || len(m.records) == 0 {
		return false
	}
	idx, found := m.find(targetID)
	if !found {
		return false
	}
	m.records = slices.Delete(m.records, idx, idx+1)
	return true
}

// Clear removes all MemoryRecords from m.
func (m *SensoryMemory) Clear() {
	if m != nil {
		m.records = m.records[:0]
	}
}

// Len returns the number of active MemoryRecords.
func (m *SensoryMemory) Len() int {
	if m == nil {
		return 0
	}
	return len(m.records)
}

// Records returns a copy of all MemoryRecords in ascending TargetID order.
func (m *SensoryMemory) Records() []MemoryRecord {
	if m == nil || len(m.records) == 0 {
		return nil
	}
	return slices.Clone(m.records)
}

// Clone returns a deep copy of m.
func (m *SensoryMemory) Clone() *SensoryMemory {
	if m == nil {
		return NewSensoryMemory()
	}
	return &SensoryMemory{records: slices.Clone(m.records)}
}

// NearestByTag returns the MemoryRecord with Tag == tag and Confidence >= minConfidence
// whose LastKnownPosition is closest to selfPos. Ties are broken by lowest TargetID.
func (m *SensoryMemory) NearestByTag(selfPos Vec3, tag string, minConfidence float64) (MemoryRecord, bool) {
	if m == nil || len(m.records) == 0 {
		return MemoryRecord{}, false
	}
	var best MemoryRecord
	bestSq := math.MaxFloat64
	found := false
	for _, r := range m.records {
		if (tag != "" && r.Tag != tag) || r.Confidence < minConfidence {
			continue
		}
		dSq := selfPos.DistSq(r.LastKnownPosition)
		if !found || dSq < bestSq {
			best = r
			bestSq = dSq
			found = true
		}
	}
	return best, found
}

// BestByTag returns the MemoryRecord with Tag == tag and Confidence >= minConfidence
// maximizing Confidence / (1 + distance(selfPos, LastKnownPosition)).
func (m *SensoryMemory) BestByTag(selfPos Vec3, tag string, minConfidence float64) (MemoryRecord, bool) {
	if m == nil || len(m.records) == 0 {
		return MemoryRecord{}, false
	}
	var best MemoryRecord
	bestScore := -1.0
	found := false
	for _, r := range m.records {
		if (tag != "" && r.Tag != tag) || r.Confidence < minConfidence {
			continue
		}
		dist := selfPos.Dist(r.LastKnownPosition)
		score := r.Confidence / (1.0 + dist*0.1)
		if !found || score > bestScore {
			best = r
			bestScore = score
			found = true
		}
	}
	return best, found
}

// MaxConfidenceByTag returns the highest Confidence in [0, 1] among records
// matching tag (or 0 if none exist).
func (m *SensoryMemory) MaxConfidenceByTag(tag string) float64 {
	if m == nil || len(m.records) == 0 {
		return 0
	}
	best := 0.0
	for _, r := range m.records {
		if (tag == "" || r.Tag == tag) && r.Confidence > best {
			best = r.Confidence
		}
	}
	return best
}

// EmitSound propagates an acoustic stimulus from source to all other active
// Actors within radius in world, recording a SenseSound MemoryRecord with linear
// distance falloff loudness * (1 - dist/radius).
func EmitSound(world *World, source *Actor, radius, loudness float64) {
	if world == nil || source == nil || radius <= 0 || loudness <= 0 {
		return
	}
	peak := clamp01(loudness)
	for _, listener := range world.Neighbors(source.ID, radius) {
		dist := listener.Position.Dist(source.Position)
		if dist > radius {
			continue
		}
		conf := peak * (1.0 - dist/radius)
		if conf > 0 {
			listener.Memory().ObserveStimulus(source.ID, source.Tag, source.Position, SenseSound, conf, world.SimTime())
		}
	}
}

func (m *SensoryMemory) mixHash(mixUint64 func(uint64), mixString func(string)) {
	if m == nil || len(m.records) == 0 {
		mixUint64(0)
		return
	}
	mixUint64(uint64(len(m.records)))
	for _, r := range m.records {
		mixUint64(uint64(r.TargetID))
		mixString(r.Tag)
		mixUint64(math.Float64bits(r.LastKnownPosition.X))
		mixUint64(math.Float64bits(r.LastKnownPosition.Y))
		mixUint64(math.Float64bits(r.LastKnownPosition.Z))
		mixUint64(math.Float64bits(r.LastKnownVelocity.X))
		mixUint64(math.Float64bits(r.LastKnownVelocity.Y))
		mixUint64(math.Float64bits(r.LastKnownVelocity.Z))
		mixUint64(math.Float64bits(r.LastSeenTime))
		mixUint64(math.Float64bits(r.Age))
		mixUint64(math.Float64bits(r.Confidence))
		mixString(string(r.Sense))
		if r.Visible {
			mixUint64(1)
		} else {
			mixUint64(0)
		}
	}
}

func clamp01(v float64) float64 {
	if math.IsNaN(v) || v <= 0 {
		return 0
	}
	if v >= 1 {
		return 1
	}
	return v
}
