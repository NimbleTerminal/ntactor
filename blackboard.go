// Copyright (c) 2026 Neomantra Corp.

package ntactor

import (
	"encoding/json"
	"math"
	"slices"
	"strings"
)

// ValueKind identifies the concrete type stored in a BlackboardValue.
type ValueKind uint8

const (
	// KindFloat indicates a float64 BlackboardValue.
	KindFloat ValueKind = iota + 1
	// KindInt indicates an int64 BlackboardValue.
	KindInt
	// KindBool indicates a bool BlackboardValue.
	KindBool
	// KindString indicates a string BlackboardValue.
	KindString
	// KindVec3 indicates a Vec3 BlackboardValue.
	KindVec3
	// KindActorID indicates an ActorID BlackboardValue.
	KindActorID
)

// String returns the name of the ValueKind.
func (k ValueKind) String() string {
	switch k {
	case KindFloat:
		return "float"
	case KindInt:
		return "int"
	case KindBool:
		return "bool"
	case KindString:
		return "string"
	case KindVec3:
		return "vec3"
	case KindActorID:
		return "actor_id"
	default:
		return "invalid"
	}
}

// BlackboardValue holds a single typed value inside a Blackboard without boxing
// primitives on the heap.
type BlackboardValue struct {
	kind ValueKind
	f    float64
	i    int64
	b    bool
	s    string
	v    Vec3
	id   ActorID
}

// FloatValue constructs a float64 BlackboardValue.
func FloatValue(v float64) BlackboardValue {
	return BlackboardValue{kind: KindFloat, f: v}
}

// IntValue constructs an int64 BlackboardValue.
func IntValue(v int64) BlackboardValue {
	return BlackboardValue{kind: KindInt, i: v}
}

// BoolValue constructs a bool BlackboardValue.
func BoolValue(v bool) BlackboardValue {
	return BlackboardValue{kind: KindBool, b: v}
}

// StringValue constructs a string BlackboardValue.
func StringValue(v string) BlackboardValue {
	return BlackboardValue{kind: KindString, s: v}
}

// Vec3Value constructs a Vec3 BlackboardValue.
func Vec3Value(v Vec3) BlackboardValue {
	return BlackboardValue{kind: KindVec3, v: v}
}

// ActorIDValue constructs an ActorID BlackboardValue.
func ActorIDValue(v ActorID) BlackboardValue {
	return BlackboardValue{kind: KindActorID, id: v}
}

// Kind returns the ValueKind of the BlackboardValue.
func (v BlackboardValue) Kind() ValueKind { return v.kind }

// Float returns the float64 value (or int64 converted to float64) and true if numeric.
func (v BlackboardValue) Float() (float64, bool) {
	switch v.kind {
	case KindFloat:
		return v.f, true
	case KindInt:
		return float64(v.i), true
	default:
		return 0, false
	}
}

// Int returns the int64 value and true if Kind is KindInt.
func (v BlackboardValue) Int() (int64, bool) {
	if v.kind == KindInt {
		return v.i, true
	}
	return 0, false
}

// Bool returns the bool value and true if Kind is KindBool.
func (v BlackboardValue) Bool() (bool, bool) {
	if v.kind == KindBool {
		return v.b, true
	}
	return false, false
}

// StringVal returns the string value and true if Kind is KindString.
func (v BlackboardValue) StringVal() (string, bool) {
	if v.kind == KindString {
		return v.s, true
	}
	return "", false
}

// Vec3 returns the Vec3 value and true if Kind is KindVec3.
func (v BlackboardValue) Vec3() (Vec3, bool) {
	if v.kind == KindVec3 {
		return v.v, true
	}
	return Vec3{}, false
}

// ActorID returns the ActorID value and true if Kind is KindActorID.
func (v BlackboardValue) ActorID() (ActorID, bool) {
	if v.kind == KindActorID {
		return v.id, true
	}
	return InvalidActorID, false
}

// MarshalJSON serializes BlackboardValue as a compact JSON value.
func (v BlackboardValue) MarshalJSON() ([]byte, error) {
	switch v.kind {
	case KindFloat:
		return json.Marshal(v.f)
	case KindInt:
		return json.Marshal(v.i)
	case KindBool:
		return json.Marshal(v.b)
	case KindString:
		return json.Marshal(v.s)
	case KindVec3:
		return json.Marshal(v.v)
	case KindActorID:
		return json.Marshal(uint64(v.id))
	default:
		return []byte("null"), nil
	}
}

// BlackboardEntry is a single key-value pair in a Blackboard.
type BlackboardEntry struct {
	Key   string          `json:"key"`
	Kind  string          `json:"kind"`
	Value BlackboardValue `json:"value"`
}

// Blackboard is a deterministic, sorted key-value memory store attached to a
// World or an Actor. Keys are maintained in ascending lexical order so
// iteration, snapshots, and World.Hash are 100% deterministic without map
// iteration order hazards.
type Blackboard struct {
	entries []BlackboardEntry
}

// NewBlackboard constructs an empty Blackboard.
func NewBlackboard() *Blackboard {
	return &Blackboard{}
}

func (b *Blackboard) find(key string) (int, bool) {
	return slices.BinarySearchFunc(b.entries, key, func(e BlackboardEntry, target string) int {
		return strings.Compare(e.Key, target)
	})
}

// Set stores val under key, inserting or replacing in sorted key order.
func (b *Blackboard) Set(key string, val BlackboardValue) {
	if b == nil || key == "" || val.kind == 0 {
		return
	}
	idx, found := b.find(key)
	entry := BlackboardEntry{
		Key:   key,
		Kind:  val.kind.String(),
		Value: val,
	}
	if found {
		b.entries[idx] = entry
		return
	}
	b.entries = slices.Insert(b.entries, idx, entry)
}

// Get retrieves the BlackboardValue for key.
func (b *Blackboard) Get(key string) (BlackboardValue, bool) {
	if b == nil || len(b.entries) == 0 {
		return BlackboardValue{}, false
	}
	idx, found := b.find(key)
	if !found {
		return BlackboardValue{}, false
	}
	return b.entries[idx].Value, true
}

// Has reports whether key exists in the Blackboard.
func (b *Blackboard) Has(key string) bool {
	_, ok := b.Get(key)
	return ok
}

// Delete removes key from the Blackboard and returns true if it was present.
func (b *Blackboard) Delete(key string) bool {
	if b == nil || len(b.entries) == 0 {
		return false
	}
	idx, found := b.find(key)
	if !found {
		return false
	}
	b.entries = slices.Delete(b.entries, idx, idx+1)
	return true
}

// Clear removes all entries from the Blackboard.
func (b *Blackboard) Clear() {
	if b != nil {
		b.entries = b.entries[:0]
	}
}

// Len returns the number of entries in the Blackboard.
func (b *Blackboard) Len() int {
	if b == nil {
		return 0
	}
	return len(b.entries)
}

// Entries returns a copy of all entries in ascending key order.
func (b *Blackboard) Entries() []BlackboardEntry {
	if b == nil || len(b.entries) == 0 {
		return nil
	}
	return slices.Clone(b.entries)
}

// Clone returns a deep copy of the Blackboard.
func (b *Blackboard) Clone() *Blackboard {
	if b == nil {
		return NewBlackboard()
	}
	return &Blackboard{entries: slices.Clone(b.entries)}
}

// SetFloat sets a float64 entry on the Blackboard.
func (b *Blackboard) SetFloat(key string, v float64) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		v = 0
	}
	b.Set(key, FloatValue(v))
}

// Float returns the numeric value for key as a float64.
func (b *Blackboard) Float(key string) (float64, bool) {
	v, ok := b.Get(key)
	if !ok {
		return 0, false
	}
	return v.Float()
}

// FloatOr returns the float64 value for key, or fallback if not present.
func (b *Blackboard) FloatOr(key string, fallback float64) float64 {
	if v, ok := b.Float(key); ok {
		return v
	}
	return fallback
}

// AddFloat adds delta to the float64 entry at key (initializing from 0 if absent)
// and returns the new value.
func (b *Blackboard) AddFloat(key string, delta float64) float64 {
	next := b.FloatOr(key, 0) + delta
	b.SetFloat(key, next)
	return next
}

// SetInt sets an int64 entry on the Blackboard.
func (b *Blackboard) SetInt(key string, v int64) {
	b.Set(key, IntValue(v))
}

// Int returns the int64 value for key.
func (b *Blackboard) Int(key string) (int64, bool) {
	v, ok := b.Get(key)
	if !ok {
		return 0, false
	}
	return v.Int()
}

// IntOr returns the int64 value for key, or fallback if not present.
func (b *Blackboard) IntOr(key string, fallback int64) int64 {
	if v, ok := b.Int(key); ok {
		return v
	}
	return fallback
}

// AddInt adds delta to the int64 entry at key (initializing from 0 if absent)
// and returns the new value.
func (b *Blackboard) AddInt(key string, delta int64) int64 {
	next := b.IntOr(key, 0) + delta
	b.SetInt(key, next)
	return next
}

// SetBool sets a bool entry on the Blackboard.
func (b *Blackboard) SetBool(key string, v bool) {
	b.Set(key, BoolValue(v))
}

// Bool returns the bool value for key.
func (b *Blackboard) Bool(key string) (bool, bool) {
	v, ok := b.Get(key)
	if !ok {
		return false, false
	}
	return v.Bool()
}

// BoolOr returns the bool value for key, or fallback if not present.
func (b *Blackboard) BoolOr(key string, fallback bool) bool {
	if v, ok := b.Bool(key); ok {
		return v
	}
	return fallback
}

// SetString sets a string entry on the Blackboard.
func (b *Blackboard) SetString(key, v string) {
	b.Set(key, StringValue(v))
}

// String returns the string value for key.
func (b *Blackboard) String(key string) (string, bool) {
	v, ok := b.Get(key)
	if !ok {
		return "", false
	}
	return v.StringVal()
}

// StringOr returns the string value for key, or fallback if not present.
func (b *Blackboard) StringOr(key, fallback string) string {
	if v, ok := b.String(key); ok {
		return v
	}
	return fallback
}

// SetVec3 sets a Vec3 entry on the Blackboard.
func (b *Blackboard) SetVec3(key string, v Vec3) {
	b.Set(key, Vec3Value(v))
}

// Vec3 returns the Vec3 value for key.
func (b *Blackboard) Vec3(key string) (Vec3, bool) {
	v, ok := b.Get(key)
	if !ok {
		return Vec3{}, false
	}
	return v.Vec3()
}

// Vec3Or returns the Vec3 value for key, or fallback if not present.
func (b *Blackboard) Vec3Or(key string, fallback Vec3) Vec3 {
	if v, ok := b.Vec3(key); ok {
		return v
	}
	return fallback
}

// SetActorID sets an ActorID entry on the Blackboard.
func (b *Blackboard) SetActorID(key string, id ActorID) {
	b.Set(key, ActorIDValue(id))
}

// ActorID returns the ActorID value for key.
func (b *Blackboard) ActorID(key string) (ActorID, bool) {
	v, ok := b.Get(key)
	if !ok {
		return InvalidActorID, false
	}
	return v.ActorID()
}

// ActorIDOr returns the ActorID value for key, or fallback if not present.
func (b *Blackboard) ActorIDOr(key string, fallback ActorID) ActorID {
	if v, ok := b.ActorID(key); ok {
		return v
	}
	return fallback
}

func (b *Blackboard) mixHash(mixUint64 func(uint64), mixString func(string)) {
	if b == nil {
		mixUint64(0)
		return
	}
	mixUint64(uint64(len(b.entries)))
	for _, e := range b.entries {
		mixString(e.Key)
		mixUint64(uint64(e.Value.kind))
		switch e.Value.kind {
		case KindFloat:
			mixUint64(math.Float64bits(e.Value.f))
		case KindInt:
			mixUint64(uint64(e.Value.i))
		case KindBool:
			if e.Value.b {
				mixUint64(1)
			} else {
				mixUint64(0)
			}
		case KindString:
			mixString(e.Value.s)
		case KindVec3:
			mixUint64(math.Float64bits(e.Value.v.X))
			mixUint64(math.Float64bits(e.Value.v.Y))
			mixUint64(math.Float64bits(e.Value.v.Z))
		case KindActorID:
			mixUint64(uint64(e.Value.id))
		}
	}
}

// ActorSnapshot is a JSON-serializable snapshot of a single Actor's kinematics
// and Blackboard memory.
type ActorSnapshot struct {
	ID         ActorID           `json:"id"`
	Tag        string            `json:"tag"`
	Position   Vec3              `json:"position"`
	Velocity   Vec3              `json:"velocity"`
	Radius     float64           `json:"radius,omitempty"`
	Blackboard []BlackboardEntry `json:"blackboard,omitempty"`
}

// WorldSnapshot is a deterministic, JSON-serializable snapshot of a World's
// clock, digest, global Blackboard, and ordered Actor states.
type WorldSnapshot struct {
	Seed       uint64            `json:"seed"`
	TickCount  uint64            `json:"tick_count"`
	SimTime    float64           `json:"sim_time"`
	Hash       uint64            `json:"hash"`
	Blackboard []BlackboardEntry `json:"blackboard,omitempty"`
	Actors     []ActorSnapshot   `json:"actors"`
}

// Snapshot captures a deterministic WorldSnapshot of the current simulation state.
func (w *World) Snapshot() WorldSnapshot {
	actors := w.Actors()
	snaps := make([]ActorSnapshot, 0, len(actors))
	for _, a := range actors {
		if a == nil || a.destroyed {
			continue
		}
		snaps = append(snaps, ActorSnapshot{
			ID:         a.ID,
			Tag:        a.Tag,
			Position:   a.Position,
			Velocity:   a.Velocity,
			Radius:     a.Radius,
			Blackboard: a.bb.Entries(),
		})
	}
	return WorldSnapshot{
		Seed:       w.seed,
		TickCount:  w.tickCount,
		SimTime:    w.simTime,
		Hash:       w.Hash(),
		Blackboard: w.bb.Entries(),
		Actors:     snaps,
	}
}

// SnapshotJSON serializes w.Snapshot() as indented JSON.
func (w *World) SnapshotJSON() ([]byte, error) {
	return json.MarshalIndent(w.Snapshot(), "", "  ")
}
