package grokotel

import (
	"encoding/binary"
	"errors"
)

// This file is the whole of telltale's OTLP surface: a bounds-checked walk of
// protobuf wire format, and the extraction of exactly one record type from an
// ExportLogsServiceRequest. It is hand-rolled for the same reason
// internal/sqlite is (decisions/006): the alternative is a protobuf runtime
// plus generated OTLP bindings on a binary whose statusline fast path is
// re-benchmarked whenever deps change (ADR-002), spent on reading five fields.
//
// Field numbers below are from the OTLP v1 protos, and every one of them was
// CONFIRMED against a live capture before being written down here: grok 1.0.0
// (3cd0d0cbce) exporting to a dump collector on 2026-08-10 (design.md §3.9a's
// export addendum). Where the spec and the capture could have disagreed, the
// capture wins — e.g. the event name arrives in LogRecord field 12
// (event_name), not as an attribute, which is a fact about grok's exporter
// version and not about the schema doc.
var errMalformed = errors.New("malformed protobuf payload")

// field is one wire-format field. Exactly one of num's value slots is
// meaningful, selected by wire: varint and the fixed widths land in num64,
// length-delimited payloads land in bytes.
type field struct {
	num   uint64
	wire  uint64
	num64 uint64
	bytes []byte
}

// each walks one message's fields in order. It never allocates a tree — the
// caller recurses into bytes where the schema says a submessage lives. A
// malformed buffer (truncated varint, length past the end, or a group wire
// type, which OTLP never emits) fails the whole walk: a payload we cannot
// frame is a payload we may not count from.
func each(b []byte, fn func(field) error) error {
	i := 0
	for i < len(b) {
		tag, n := binary.Uvarint(b[i:])
		if n <= 0 {
			return errMalformed
		}
		num, wire := tag>>3, tag&7
		if num == 0 {
			return errMalformed
		}
		i += n
		f := field{num: num, wire: wire}
		switch wire {
		case 0: // varint
			v, m := binary.Uvarint(b[i:])
			if m <= 0 {
				return errMalformed
			}
			f.num64 = v
			i += m
		case 1: // fixed64
			if i+8 > len(b) {
				return errMalformed
			}
			f.num64 = binary.LittleEndian.Uint64(b[i : i+8])
			i += 8
		case 2: // length-delimited
			l, m := binary.Uvarint(b[i:])
			if m <= 0 || l > uint64(len(b)-i-m) {
				return errMalformed
			}
			f.bytes = b[i+m : i+m+int(l)]
			i += m + int(l)
		case 5: // fixed32
			if i+4 > len(b) {
				return errMalformed
			}
			f.num64 = uint64(binary.LittleEndian.Uint32(b[i : i+4]))
			i += 4
		default:
			return errMalformed
		}
		if err := fn(f); err != nil {
			return err
		}
	}
	return nil
}

// apiRequest is one grok_code.api_request log record, reduced to what
// telltale keeps. The four counts are pointers for the same reason
// cursorhook.Turn's are: the extraction sets a field only when the wire
// carried it, and absent is not zero (§4a.1). Everything else the record
// carries — model, duration, stop reason, user and team ids — has no
// destination here and never leaves this parse.
type apiRequest struct {
	// sessionID and sequence exist for duplicate refusal only (the exporter
	// retries on failure, and a retried batch counted twice is an overstated
	// total). They are held in the collector's memory and are never written
	// to the cache file — the file is totals and window only, like cursor's.
	sessionID string
	sequence  uint64
	hasSeq    bool

	input, output, reasoning, cacheRead *int64
}

// complete mirrors cursorhook.Turn.Complete: only a record carrying all four
// of its vendor's counts may be accumulated. Summing three and treating the
// fourth as zero leaves the total wrong by an amount nothing can name.
func (r apiRequest) complete() bool {
	return r.input != nil && r.output != nil && r.reasoning != nil && r.cacheRead != nil
}

func (r apiRequest) nonnegative() bool {
	for _, v := range []*int64{r.input, r.output, r.reasoning, r.cacheRead} {
		if v != nil && *v < 0 {
			return false
		}
	}
	return true
}

// apiRequests extracts every grok_code.api_request record from one
// /v1/logs body. The nesting is ExportLogsServiceRequest.resource_logs(1) →
// ResourceLogs.scope_logs(2) → ScopeLogs.log_records(2), all measured live.
// Records with any other event name — session_start, turn_completed,
// mcp_server_connection and the rest of the §3.9a event table — are walked
// past without being read: turn_completed carries no token counts (measured),
// and one record type is one claim.
func apiRequests(body []byte) ([]apiRequest, error) {
	var out []apiRequest
	err := each(body, func(rl field) error {
		if rl.num != 1 || rl.wire != 2 {
			return nil
		}
		return each(rl.bytes, func(sl field) error {
			if sl.num != 2 || sl.wire != 2 {
				return nil
			}
			return each(sl.bytes, func(lr field) error {
				if lr.num != 2 || lr.wire != 2 {
					return nil
				}
				r, isAPI, err := parseRecord(lr.bytes)
				if err != nil {
					return err
				}
				if isAPI {
					out = append(out, r)
				}
				return nil
			})
		})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// parseRecord reads one LogRecord: field 12 is event_name, field 6 is a
// repeated KeyValue attribute. The attribute keys matched here are the whole
// allowlist — a key with no case below falls through and its value is
// dropped, which is how prompt text stays impossible to keep even if a
// content gate were opened on the grok side.
func parseRecord(b []byte) (apiRequest, bool, error) {
	var r apiRequest
	isAPI := false
	err := each(b, func(f field) error {
		switch {
		case f.num == 12 && f.wire == 2:
			isAPI = string(f.bytes) == "grok_code.api_request"
		case f.num == 6 && f.wire == 2:
			key, val, err := parseKeyValue(f.bytes)
			if err != nil {
				return err
			}
			switch key {
			case "input_tokens":
				r.input = val.intp()
			case "output_tokens":
				r.output = val.intp()
			case "reasoning_tokens":
				r.reasoning = val.intp()
			case "cache_read_tokens":
				r.cacheRead = val.intp()
			case "session.id":
				r.sessionID = val.str
			case "event.sequence":
				if p := val.intp(); p != nil && *p >= 0 {
					r.sequence, r.hasSeq = uint64(*p), true
				}
			}
		}
		return nil
	})
	return r, isAPI, err
}

// anyValue is an OTLP AnyValue narrowed to the two kinds this parse can use:
// string_value (field 1) and int_value (field 3). Bools, doubles, arrays and
// kvlists have no destination and are left where they lie.
type anyValue struct {
	str    string
	i      int64
	hasInt bool
}

func (v anyValue) intp() *int64 {
	if !v.hasInt {
		return nil
	}
	n := v.i
	return &n
}

func parseKeyValue(b []byte) (string, anyValue, error) {
	var key string
	var val anyValue
	err := each(b, func(f field) error {
		switch {
		case f.num == 1 && f.wire == 2:
			key = string(f.bytes)
		case f.num == 2 && f.wire == 2:
			return each(f.bytes, func(v field) error {
				switch {
				case v.num == 1 && v.wire == 2:
					val.str = string(v.bytes)
				case v.num == 3 && v.wire == 0:
					// int_value is a plain varint int64; a negative count
					// arrives as its two's-complement and is rejected later
					// by the nonnegative gate, not silently reinterpreted.
					val.i, val.hasInt = int64(v.num64), true
				}
				return nil
			})
		}
		return nil
	})
	return key, val, err
}
