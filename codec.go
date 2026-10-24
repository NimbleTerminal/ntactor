// Copyright (c) 2026 Neomantra Corp.

package ntactor

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
)

const (
	// MaxEventLogBytes is the maximum size (16 MiB) accepted when decoding an
	// EventLog from JSON or binary bytes.
	MaxEventLogBytes = 16 << 20
	// MaxEventLogFrames is the maximum number of frames (1,000,000) accepted
	// when decoding an EventLog.
	MaxEventLogFrames = 1_000_000
	// MaxFrameEvents is the maximum number of events per frame (16,384)
	// accepted when decoding an EventLog.
	MaxFrameEvents = 16_384
)

var (
	// ErrInvalidEventLog is returned when an encoded EventLog is malformed or
	// exceeds safety bounds.
	ErrInvalidEventLog = errors.New("ntactor: invalid or oversized event log")

	binaryMagic = [4]byte{'N', 'T', 'A', '1'}
)

// EncodeEventLogJSON writes log as indented JSON to w.
func EncodeEventLogJSON(w io.Writer, log EventLog) error {
	if w == nil {
		return ErrInvalidEventLog
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(log)
}

// DecodeEventLogJSON reads a JSON-encoded EventLog from r within MaxEventLogBytes.
func DecodeEventLogJSON(r io.Reader) (EventLog, error) {
	if r == nil {
		return EventLog{}, ErrInvalidEventLog
	}
	lr := io.LimitReader(r, MaxEventLogBytes+1)
	data, err := io.ReadAll(lr)
	if err != nil {
		return EventLog{}, err
	}
	if len(data) > MaxEventLogBytes {
		return EventLog{}, fmt.Errorf("%w: exceeds %d bytes", ErrInvalidEventLog, MaxEventLogBytes)
	}
	var log EventLog
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&log); err != nil {
		return EventLog{}, fmt.Errorf("%w: %v", ErrInvalidEventLog, err)
	}
	if err := validateEventLog(log); err != nil {
		return EventLog{}, err
	}
	return log, nil
}

// MarshalEventLogBinary serializes log into a compact, bit-exact binary format.
// String payloads on Events are preserved; non-string Payload values are omitted
// in binary mode (use JSON or Event fields Kind/Target/Vector/Value).
func MarshalEventLogBinary(log EventLog) ([]byte, error) {
	if err := validateEventLog(log); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.Write(binaryMagic[:])

	var tmp [8]byte
	putU64 := func(v uint64) {
		binary.LittleEndian.PutUint64(tmp[:], v)
		buf.Write(tmp[:])
	}
	putF64 := func(v float64) {
		putU64(math.Float64bits(v))
	}
	putStr := func(s string) error {
		if len(s) > 65535 {
			return fmt.Errorf("%w: string length %d exceeds 65535", ErrInvalidEventLog, len(s))
		}
		var slen [2]byte
		binary.LittleEndian.PutUint16(slen[:], uint16(len(s)))
		buf.Write(slen[:])
		buf.WriteString(s)
		return nil
	}

	putU64(log.Seed)
	putU64(uint64(len(log.Frames)))

	for _, f := range log.Frames {
		putF64(f.DT)
		putU64(uint64(len(f.Events)))
		for _, ev := range f.Events {
			if err := putStr(ev.Kind); err != nil {
				return nil, err
			}
			putU64(uint64(ev.Target))
			putF64(ev.Vector.X)
			putF64(ev.Vector.Y)
			putF64(ev.Vector.Z)
			putF64(ev.Value)
			payloadStr, _ := ev.Payload.(string)
			if err := putStr(payloadStr); err != nil {
				return nil, err
			}
		}
	}
	return buf.Bytes(), nil
}

// UnmarshalEventLogBinary decodes a binary EventLog produced by MarshalEventLogBinary.
func UnmarshalEventLogBinary(data []byte) (EventLog, error) {
	if len(data) < 20 || len(data) > MaxEventLogBytes {
		return EventLog{}, ErrInvalidEventLog
	}
	if !bytes.Equal(data[:4], binaryMagic[:]) {
		return EventLog{}, fmt.Errorf("%w: bad magic header", ErrInvalidEventLog)
	}
	offset := 4

	readU64 := func() (uint64, error) {
		if offset+8 > len(data) {
			return 0, io.ErrUnexpectedEOF
		}
		v := binary.LittleEndian.Uint64(data[offset : offset+8])
		offset += 8
		return v, nil
	}
	readF64 := func() (float64, error) {
		u, err := readU64()
		if err != nil {
			return 0, err
		}
		f := math.Float64frombits(u)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, fmt.Errorf("%w: non-finite float", ErrInvalidEventLog)
		}
		return f, nil
	}
	readStr := func() (string, error) {
		if offset+2 > len(data) {
			return "", io.ErrUnexpectedEOF
		}
		n := int(binary.LittleEndian.Uint16(data[offset : offset+2]))
		offset += 2
		if offset+n > len(data) {
			return "", io.ErrUnexpectedEOF
		}
		s := string(data[offset : offset+n])
		offset += n
		return s, nil
	}

	seed, err := readU64()
	if err != nil {
		return EventLog{}, fmt.Errorf("%w: %v", ErrInvalidEventLog, err)
	}
	frameCount, err := readU64()
	if err != nil || frameCount > MaxEventLogFrames {
		return EventLog{}, fmt.Errorf("%w: invalid frame count", ErrInvalidEventLog)
	}

	frames := make([]Frame, frameCount)
	for i := range frames {
		dt, err := readF64()
		if err != nil || dt < 0 {
			return EventLog{}, fmt.Errorf("%w: invalid frame dt", ErrInvalidEventLog)
		}
		evCount, err := readU64()
		if err != nil || evCount > MaxFrameEvents {
			return EventLog{}, fmt.Errorf("%w: invalid event count", ErrInvalidEventLog)
		}
		var evs []Event
		if evCount > 0 {
			evs = make([]Event, evCount)
			for j := range evs {
				kind, err := readStr()
				if err != nil {
					return EventLog{}, fmt.Errorf("%w: %v", ErrInvalidEventLog, err)
				}
				target, err := readU64()
				if err != nil {
					return EventLog{}, fmt.Errorf("%w: %v", ErrInvalidEventLog, err)
				}
				vx, err := readF64()
				if err != nil {
					return EventLog{}, err
				}
				vy, err := readF64()
				if err != nil {
					return EventLog{}, err
				}
				vz, err := readF64()
				if err != nil {
					return EventLog{}, err
				}
				val, err := readF64()
				if err != nil {
					return EventLog{}, err
				}
				pStr, err := readStr()
				if err != nil {
					return EventLog{}, fmt.Errorf("%w: %v", ErrInvalidEventLog, err)
				}
				var payload any
				if pStr != "" {
					payload = pStr
				}
				evs[j] = Event{
					Kind:    kind,
					Target:  ActorID(target),
					Vector:  Vec3{X: vx, Y: vy, Z: vz},
					Value:   val,
					Payload: payload,
				}
			}
		}
		frames[i] = Frame{DT: dt, Events: evs}
	}

	if offset != len(data) {
		return EventLog{}, fmt.Errorf("%w: trailing bytes", ErrInvalidEventLog)
	}
	return EventLog{Seed: seed, Frames: frames}, nil
}

func validateEventLog(log EventLog) error {
	if len(log.Frames) > MaxEventLogFrames {
		return fmt.Errorf("%w: frame count %d exceeds %d", ErrInvalidEventLog, len(log.Frames), MaxEventLogFrames)
	}
	for _, f := range log.Frames {
		if f.DT < 0 || math.IsNaN(f.DT) || math.IsInf(f.DT, 0) {
			return fmt.Errorf("%w: invalid frame DT", ErrInvalidEventLog)
		}
		if len(f.Events) > MaxFrameEvents {
			return fmt.Errorf("%w: frame event count %d exceeds %d", ErrInvalidEventLog, len(f.Events), MaxFrameEvents)
		}
		for _, ev := range f.Events {
			if math.IsNaN(ev.Vector.X) || math.IsInf(ev.Vector.X, 0) ||
				math.IsNaN(ev.Vector.Y) || math.IsInf(ev.Vector.Y, 0) ||
				math.IsNaN(ev.Vector.Z) || math.IsInf(ev.Vector.Z, 0) ||
				math.IsNaN(ev.Value) || math.IsInf(ev.Value, 0) {
				return fmt.Errorf("%w: non-finite event float", ErrInvalidEventLog)
			}
		}
	}
	return nil
}
