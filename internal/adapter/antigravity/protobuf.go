package antigravity

import "encoding/binary"

// A schema-free protobuf wire walk, stdlib only.
//
// Antigravity CLI stores its generation metadata as protobuf blobs with no
// .proto anywhere on disk (docs/design.md §3.8). The wire format is
// self-describing enough to walk without one: every field carries its number
// and one of six wire types, and a length-delimited field can be recursed into
// when the caller knows it is a message.
//
// What this walk deliberately does NOT do is guess. It never infers meaning
// from a field's shape, never treats an unparseable submessage as an error,
// and hands the caller raw values to interpret against the field map §3.8
// recorded from live observation. Everything read through here is
// cross-checked by the token invariant in antigravity.go before it can render.

// wireType values, from the protobuf encoding spec.
const (
	wireVarint = 0
	wireI64    = 1
	wireBytes  = 2
	wireSGroup = 3
	wireEGroup = 4
	wireI32    = 5
)

// field is one decoded wire field. Num and Wire always mean something; which
// of Val and Data carries the payload depends on Wire.
type field struct {
	Num  int
	Wire int
	Val  uint64 // varint, i32 and i64 payloads
	Data []byte // length-delimited payload, aliasing the input
}

// walk decodes the top-level fields of a protobuf message, calling fn for
// each. It stops at the first byte it cannot decode rather than reporting an
// error: these blobs are read from a live file and a schema-free walk into a
// field that turned out to be a string will hit garbage by construction. The
// caller learns nothing from "the 41st byte of a string did not look like a
// tag", and everything from whether the fields it wanted were found.
//
// fn returns false to stop the walk early.
func walk(b []byte, fn func(f field) bool) {
	i := 0
	for i < len(b) {
		key, n := binary.Uvarint(b[i:])
		if n <= 0 {
			return
		}
		i += n
		num, wire := int(key>>3), int(key&7)
		if num == 0 {
			return
		}
		switch wire {
		case wireVarint:
			v, n := binary.Uvarint(b[i:])
			if n <= 0 {
				return
			}
			i += n
			if !fn(field{Num: num, Wire: wire, Val: v}) {
				return
			}
		case wireI64:
			if i+8 > len(b) {
				return
			}
			v := binary.LittleEndian.Uint64(b[i : i+8])
			i += 8
			if !fn(field{Num: num, Wire: wire, Val: v}) {
				return
			}
		case wireI32:
			if i+4 > len(b) {
				return
			}
			v := uint64(binary.LittleEndian.Uint32(b[i : i+4]))
			i += 4
			if !fn(field{Num: num, Wire: wire, Val: v}) {
				return
			}
		case wireBytes:
			ln, n := binary.Uvarint(b[i:])
			if n <= 0 {
				return
			}
			i += n
			if ln > uint64(len(b)-i) {
				return
			}
			data := b[i : i+int(ln)]
			i += int(ln)
			if !fn(field{Num: num, Wire: wire, Data: data}) {
				return
			}
		case wireSGroup, wireEGroup:
			// Deprecated group encoding. Nothing in the observed corpus uses
			// it, and skipping a group correctly requires tracking nesting the
			// rest of this walk has no use for.
			return
		default:
			return
		}
	}
}

// messages returns every length-delimited field with the given number.
// Repeated fields are the normal case, so this returns a slice rather than
// pretending there is exactly one.
func messages(b []byte, num int) [][]byte {
	var out [][]byte
	walk(b, func(f field) bool {
		if f.Num == num && f.Wire == wireBytes {
			out = append(out, f.Data)
		}
		return true
	})
	return out
}

// lastString returns the last length-delimited field with the given number as
// a string. Last rather than first: where a field repeats, the later value is
// the vendor's newer writing of it.
func lastString(b []byte, num int) (string, bool) {
	var out []byte
	found := false
	walk(b, func(f field) bool {
		if f.Num == num && f.Wire == wireBytes {
			out, found = f.Data, true
		}
		return true
	})
	if !found {
		return "", false
	}
	return string(out), true
}

// lastVarint returns the last varint field with the given number.
func lastVarint(b []byte, num int) (uint64, bool) {
	var out uint64
	found := false
	walk(b, func(f field) bool {
		if f.Num == num && f.Wire == wireVarint {
			out, found = f.Val, true
		}
		return true
	})
	return out, found
}
