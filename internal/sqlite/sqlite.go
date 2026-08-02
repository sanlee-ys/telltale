// Package sqlite is a minimal, read-only, byte-level reader for SQLite
// database files. It exists because one vendor (Antigravity CLI) stores its
// session state in SQLite and this repo takes zero dependencies for a read
// path (decisions/001, decisions/006).
//
// # Scope, deliberately small
//
// This is NOT a SQL engine. There is no query planner, no index use, no
// expression evaluation, no write path of any kind. It does exactly three
// things:
//
//   - parse the 100-byte database header (page size, reserved space);
//   - walk the `sqlite_master` b-tree on page 1 to find a table's root page;
//   - walk that table's b-tree and hand back its rows as decoded record
//     values, following overflow-page chains.
//
// A caller that wants a WHERE clause writes a Go `if`.
//
// # The WAL overlay, and why it is not optional
//
// A live SQLite writer in WAL mode leaves the newest committed pages in the
// `-wal` sidecar, not in the `.db` file — a sidecar LARGER than its database
// has been observed in the wild (docs/design.md §3.8). Reading the `.db` bytes
// alone would therefore silently report a stale snapshot as current, which is
// the honest-gauge rule's exact failure mode.
//
// Open takes both byte slices and applies SQLite's own recovery semantics:
// a frame counts only when its salts match the WAL header's and its rolling
// checksum verifies, and frames after the last commit frame are ignored. A
// frame that fails either check ends the scan — that is what SQLite itself
// does, and it is also the torn-read defense, since these bytes were copied
// out from under a process that may have been mid-write.
//
// # Reading, never locking
//
// The caller passes bytes. This package opens no files, takes no locks, and
// cannot write: telltale reads vendor state and never mutates it, and a
// monitor that can corrupt the thing it monitors is not a monitor.
//
// Every structural field is bounds-checked against the byte slice it came
// from. A corrupt or truncated file yields an error or a short row list, never
// a panic and never an invented value.
package sqlite

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// Header constants from the SQLite file format spec.
const (
	headerSize     = 100
	minPageSize    = 512
	maxPageSize    = 65536
	minUsableSize  = 480 // the format's own floor
	walHeaderSize  = 32
	walFrameHeader = 24
	walMagicLE     = 0x377f0682
	walMagicBE     = 0x377f0683
)

var magic = []byte("SQLite format 3\x00")

// Page type bytes. Index pages are recognized so an unexpected one is an
// error rather than being decoded as a table page.
const (
	pageInteriorIndex = 0x02
	pageInteriorTable = 0x05
	pageLeafIndex     = 0x0a
	pageLeafTable     = 0x0d
)

// maxCells bounds a single b-tree walk. A corrupt page can point at itself
// through a chain this reader has not visited yet; the visited set catches
// cycles and this catches everything else.
const maxCells = 1 << 20

// ErrNotSQLite reports a byte slice that does not begin with the SQLite magic
// string. It is distinguishable so a caller can tell "not a database" from
// "a damaged database".
var ErrNotSQLite = errors.New("sqlite: not a SQLite database file")

// ValueType is a decoded record column's type, following SQLite's storage
// classes. There is no "unset": a column the record stores as NULL is Null,
// which is a value, not an absence of one.
type ValueType uint8

const (
	Null ValueType = iota
	Int
	Float
	Text
	Blob
)

// Value is one decoded column.
type Value struct {
	Type  ValueType
	Int   int64
	Float float64
	// Bytes carries Text and Blob payloads. It aliases the decoded record
	// buffer, which the reader owns; callers must not modify it.
	Bytes []byte
}

// Text returns the column as a string when it is one, and false otherwise.
// An INTEGER column is not silently formatted into a string: this reader
// hands back what the file says, and conversion is the caller's decision.
func (v Value) Text() (string, bool) {
	if v.Type != Text {
		return "", false
	}
	return string(v.Bytes), true
}

// Blob returns the column's bytes when it is a BLOB, and false otherwise.
func (v Value) Blob() ([]byte, bool) {
	if v.Type != Blob {
		return nil, false
	}
	return v.Bytes, true
}

// Row is one table row: its rowid and its decoded columns.
//
// A column declared INTEGER PRIMARY KEY is an alias for the rowid and the
// record stores NULL in its place. The rowid is carried separately here rather
// than back-filled into that column, because this reader has not parsed the
// table's CREATE statement and guessing which column is the alias would be
// exactly the kind of inference this repo refuses.
type Row struct {
	RowID  int64
	Values []Value
}

// File is a read-only view over a database's bytes plus its WAL overlay.
type File struct {
	pageSize int
	usable   int
	pages    int
	base     []byte
	overlay  map[uint32][]byte
	notes    []string
}

// Open parses a database file's bytes and overlays a WAL sidecar's committed
// frames. A nil or empty wal is normal (no sidecar, or a checkpointed one).
//
// A WAL that cannot be trusted is IGNORED rather than fatal: the base file is
// still a real, if older, snapshot, and the reason is recorded in Notes so the
// caller can say so out loud instead of presenting stale data as fresh.
func Open(db, wal []byte) (*File, error) {
	if len(db) < headerSize {
		return nil, ErrNotSQLite
	}
	for i, b := range magic {
		if db[i] != b {
			return nil, ErrNotSQLite
		}
	}

	pageSize := int(binary.BigEndian.Uint16(db[16:18]))
	if pageSize == 1 {
		// The format encodes 65536 as 1, because it does not fit in the field.
		pageSize = maxPageSize
	}
	if pageSize < minPageSize || pageSize > maxPageSize || pageSize&(pageSize-1) != 0 {
		return nil, fmt.Errorf("sqlite: implausible page size %d", pageSize)
	}
	reserved := int(db[20])
	usable := pageSize - reserved
	if usable < minUsableSize {
		return nil, fmt.Errorf("sqlite: usable page size %d below the format floor", usable)
	}

	f := &File{
		pageSize: pageSize,
		usable:   usable,
		pages:    len(db) / pageSize,
		base:     db,
	}

	// The header's page count is authoritative only when the file change
	// counter matches the version-valid-for number; otherwise the file was
	// written by a legacy writer and the byte length is the better answer.
	if len(db) >= 96 {
		change := binary.BigEndian.Uint32(db[24:28])
		validFor := binary.BigEndian.Uint32(db[92:96])
		declared := int(binary.BigEndian.Uint32(db[28:32]))
		if change == validFor && declared > 0 && declared <= f.pages {
			f.pages = declared
		}
	}
	if f.pages == 0 {
		return nil, errors.New("sqlite: file is shorter than one page")
	}

	if len(wal) > 0 {
		if err := f.applyWAL(wal); err != nil {
			f.overlay = nil
			f.notes = append(f.notes, "wal sidecar ignored: "+err.Error())
		}
	}
	return f, nil
}

// Notes are operator-facing structural remarks from parsing — a rejected WAL,
// most often. They never carry row content.
func (f *File) Notes() []string { return f.notes }

// PageSize is the database's page size in bytes.
func (f *File) PageSize() int { return f.pageSize }

// applyWAL builds the page overlay from a WAL sidecar.
//
// The rules are SQLite's own. Frames are scanned in order while a rolling
// checksum verifies and the salts match the header's; the first frame that
// fails either ends the scan (a partially written tail is normal on a live
// database). Only frames up to and including the LAST commit frame are
// applied: an uncommitted tail is by definition not yet part of the database.
func (f *File) applyWAL(wal []byte) error {
	if len(wal) < walHeaderSize {
		return errors.New("shorter than a WAL header")
	}
	m := binary.BigEndian.Uint32(wal[0:4])
	if m != walMagicLE && m != walMagicBE {
		return errors.New("bad WAL magic")
	}
	bigEndian := m == walMagicBE
	if ps := int(binary.BigEndian.Uint32(wal[8:12])); ps != f.pageSize {
		// A page size disagreement means the sidecar belongs to a different
		// incarnation of the database than the bytes we read.
		return fmt.Errorf("page size %d does not match the database's %d", ps, f.pageSize)
	}
	salt1 := binary.BigEndian.Uint32(wal[16:20])
	salt2 := binary.BigEndian.Uint32(wal[20:24])

	s0, s1 := checksum(0, 0, wal[0:24], bigEndian)
	if s0 != binary.BigEndian.Uint32(wal[24:28]) || s1 != binary.BigEndian.Uint32(wal[28:32]) {
		return errors.New("header checksum mismatch")
	}

	overlay := map[uint32][]byte{}
	pending := map[uint32][]byte{}
	committed := 0
	commitPages := 0

	off := walHeaderSize
	frame := walFrameHeader + f.pageSize
	for off+frame <= len(wal) {
		h := wal[off : off+walFrameHeader]
		data := wal[off+walFrameHeader : off+frame]
		if binary.BigEndian.Uint32(h[8:12]) != salt1 || binary.BigEndian.Uint32(h[12:16]) != salt2 {
			// A reset WAL reuses the file with new salts; stale frames past
			// this point belong to an older incarnation.
			break
		}
		n0, n1 := checksum(s0, s1, h[0:8], bigEndian)
		n0, n1 = checksum(n0, n1, data, bigEndian)
		if n0 != binary.BigEndian.Uint32(h[16:20]) || n1 != binary.BigEndian.Uint32(h[20:24]) {
			break
		}
		s0, s1 = n0, n1

		pageNo := binary.BigEndian.Uint32(h[0:4])
		if pageNo == 0 {
			break
		}
		pending[pageNo] = data

		if dbSize := binary.BigEndian.Uint32(h[4:8]); dbSize != 0 {
			// Commit frame: everything pending is now part of the database.
			for p, d := range pending {
				overlay[p] = d
			}
			pending = map[uint32][]byte{}
			committed++
			commitPages = int(dbSize)
		}
		off += frame
	}

	if committed == 0 {
		return errors.New("no committed frames")
	}
	f.overlay = overlay
	// A commit frame states the database's size in pages, which is how a
	// WAL-side growth (or truncation) becomes visible without touching the
	// base file.
	f.pages = commitPages
	return nil
}

// checksum is the WAL's rolling checksum over an 8-byte-aligned span.
func checksum(s0, s1 uint32, data []byte, bigEndian bool) (uint32, uint32) {
	for i := 0; i+8 <= len(data); i += 8 {
		var x0, x1 uint32
		if bigEndian {
			x0 = binary.BigEndian.Uint32(data[i : i+4])
			x1 = binary.BigEndian.Uint32(data[i+4 : i+8])
		} else {
			x0 = binary.LittleEndian.Uint32(data[i : i+4])
			x1 = binary.LittleEndian.Uint32(data[i+4 : i+8])
		}
		s0 += x0 + s1
		s1 += x1 + s0
	}
	return s0, s1
}

// page returns page n (1-based), preferring the WAL overlay.
func (f *File) page(n uint32) ([]byte, error) {
	if n == 0 {
		return nil, errors.New("sqlite: page 0 does not exist")
	}
	if p, ok := f.overlay[n]; ok {
		return p, nil
	}
	start := (int(n) - 1) * f.pageSize
	if start < 0 || start+f.pageSize > len(f.base) {
		return nil, fmt.Errorf("sqlite: page %d is outside the file", n)
	}
	return f.base[start : start+f.pageSize], nil
}

// Tables maps table name to root page, read from `sqlite_master` on page 1.
// Indexes, views and triggers are skipped: this reader walks table b-trees
// only, and returning an index root page would invite walking one.
func (f *File) Tables() (map[string]uint32, error) {
	rows, err := f.walkTable(1)
	if err != nil {
		return nil, err
	}
	out := make(map[string]uint32, len(rows))
	for _, r := range rows {
		// sqlite_master: (type, name, tbl_name, rootpage, sql)
		if len(r.Values) < 4 {
			continue
		}
		kind, ok := r.Values[0].Text()
		if !ok || kind != "table" {
			continue
		}
		name, ok := r.Values[1].Text()
		if !ok || name == "" {
			continue
		}
		if r.Values[3].Type != Int || r.Values[3].Int <= 0 {
			continue
		}
		out[name] = uint32(r.Values[3].Int)
	}
	return out, nil
}

// Table returns every row of the named table.
//
// A table that does not exist is not an error here — it is absence, and the
// caller decides what that means. The boolean says which happened.
func (f *File) Table(name string) ([]Row, bool, error) {
	tables, err := f.Tables()
	if err != nil {
		return nil, false, err
	}
	root, ok := tables[name]
	if !ok {
		return nil, false, nil
	}
	rows, err := f.walkTable(root)
	if err != nil {
		return nil, true, err
	}
	return rows, true, nil
}

// walkTable walks a table b-tree depth-first, left to right, so rows come back
// in rowid order.
func (f *File) walkTable(root uint32) ([]Row, error) {
	var out []Row
	visited := map[uint32]bool{}
	cells := 0

	var walk func(n uint32) error
	walk = func(n uint32) error {
		if visited[n] {
			return fmt.Errorf("sqlite: b-tree cycle at page %d", n)
		}
		visited[n] = true

		p, err := f.page(n)
		if err != nil {
			return err
		}
		// Page 1 carries the 100-byte file header before its b-tree header.
		base := 0
		if n == 1 {
			base = headerSize
		}
		if base+12 > len(p) {
			return fmt.Errorf("sqlite: page %d is too short for a b-tree header", n)
		}
		typ := p[base]
		count := int(binary.BigEndian.Uint16(p[base+3 : base+5]))

		switch typ {
		case pageLeafTable:
			ptrs := base + 8
			if ptrs+2*count > f.usable {
				return fmt.Errorf("sqlite: page %d cell array overruns the page", n)
			}
			for i := 0; i < count; i++ {
				cells++
				if cells > maxCells {
					return errors.New("sqlite: cell budget exhausted")
				}
				off := int(binary.BigEndian.Uint16(p[ptrs+2*i : ptrs+2*i+2]))
				row, err := f.leafCell(p, off)
				if err != nil {
					return err
				}
				out = append(out, row)
			}
			return nil

		case pageInteriorTable:
			ptrs := base + 12
			if ptrs+2*count > f.usable {
				return fmt.Errorf("sqlite: page %d cell array overruns the page", n)
			}
			for i := 0; i < count; i++ {
				off := int(binary.BigEndian.Uint16(p[ptrs+2*i : ptrs+2*i+2]))
				if off+4 > f.usable {
					return fmt.Errorf("sqlite: page %d interior cell out of bounds", n)
				}
				child := binary.BigEndian.Uint32(p[off : off+4])
				if err := walk(child); err != nil {
					return err
				}
			}
			right := binary.BigEndian.Uint32(p[base+8 : base+12])
			return walk(right)

		case pageLeafIndex, pageInteriorIndex:
			return fmt.Errorf("sqlite: page %d is an index page, not a table page", n)
		default:
			return fmt.Errorf("sqlite: page %d has unknown b-tree type 0x%02x", n, typ)
		}
	}

	if err := walk(root); err != nil {
		return nil, err
	}
	return out, nil
}

// leafCell decodes one table-leaf cell, following the overflow chain when the
// payload does not fit on the page.
//
// Blobs in the vendor data this reader was written for run to tens of
// kilobytes against a 4 KiB page, so the overflow path is the common path, not
// the exotic one.
func (f *File) leafCell(p []byte, off int) (Row, error) {
	if off < 0 || off >= f.usable {
		return Row{}, errors.New("sqlite: leaf cell offset out of bounds")
	}
	payloadLen, n := uvarint(p[off:f.usable])
	if n <= 0 {
		return Row{}, errors.New("sqlite: unreadable payload length")
	}
	cur := off + n
	rowid, n := varint(p[cur:f.usable])
	if n <= 0 {
		return Row{}, errors.New("sqlite: unreadable rowid")
	}
	cur += n

	total := int(payloadLen)
	if total < 0 || total > 1<<30 {
		return Row{}, fmt.Errorf("sqlite: implausible payload length %d", payloadLen)
	}

	// Spec arithmetic for how much of the payload lives on this page.
	u := f.usable
	x := u - 35
	local := total
	if total > x {
		m := ((u - 12) * 32 / 255) - 23
		k := m + ((total - m) % (u - 4))
		if k <= x {
			local = k
		} else {
			local = m
		}
	}
	if cur+local > u {
		return Row{}, errors.New("sqlite: cell payload overruns the page")
	}

	payload := make([]byte, 0, total)
	payload = append(payload, p[cur:cur+local]...)

	if local < total {
		if cur+local+4 > u {
			return Row{}, errors.New("sqlite: overflow pointer overruns the page")
		}
		next := binary.BigEndian.Uint32(p[cur+local : cur+local+4])
		seen := map[uint32]bool{}
		for next != 0 && len(payload) < total {
			if seen[next] {
				return Row{}, errors.New("sqlite: overflow page cycle")
			}
			seen[next] = true
			op, err := f.page(next)
			if err != nil {
				return Row{}, err
			}
			if len(op) < 4 {
				return Row{}, errors.New("sqlite: short overflow page")
			}
			want := total - len(payload)
			avail := u - 4
			if want > avail {
				want = avail
			}
			if 4+want > len(op) {
				return Row{}, errors.New("sqlite: overflow page content out of bounds")
			}
			payload = append(payload, op[4:4+want]...)
			next = binary.BigEndian.Uint32(op[0:4])
		}
		if len(payload) != total {
			return Row{}, fmt.Errorf("sqlite: overflow chain ended %d bytes short", total-len(payload))
		}
	}

	vals, err := decodeRecord(payload)
	if err != nil {
		return Row{}, err
	}
	return Row{RowID: rowid, Values: vals}, nil
}

// decodeRecord decodes SQLite's record format: a header of serial-type
// varints followed by the column bodies.
func decodeRecord(rec []byte) ([]Value, error) {
	hdrLen, n := uvarint(rec)
	if n <= 0 || int(hdrLen) > len(rec) || int(hdrLen) < n {
		return nil, errors.New("sqlite: unreadable record header")
	}
	var serials []uint64
	for i := n; i < int(hdrLen); {
		s, m := uvarint(rec[i:int(hdrLen)])
		if m <= 0 {
			return nil, errors.New("sqlite: unreadable serial type")
		}
		serials = append(serials, s)
		i += m
	}

	body := int(hdrLen)
	out := make([]Value, 0, len(serials))
	for _, s := range serials {
		size, err := serialSize(s)
		if err != nil {
			return nil, err
		}
		if body+size > len(rec) {
			return nil, errors.New("sqlite: record body is shorter than its header declares")
		}
		raw := rec[body : body+size]
		body += size

		switch {
		case s == 0:
			out = append(out, Value{Type: Null})
		case s >= 1 && s <= 6:
			out = append(out, Value{Type: Int, Int: beInt(raw)})
		case s == 7:
			bits := uint64(0)
			for _, b := range raw {
				bits = bits<<8 | uint64(b)
			}
			out = append(out, Value{Type: Float, Float: math.Float64frombits(bits)})
		case s == 8:
			out = append(out, Value{Type: Int, Int: 0})
		case s == 9:
			out = append(out, Value{Type: Int, Int: 1})
		case s >= 12 && s%2 == 0:
			out = append(out, Value{Type: Blob, Bytes: raw})
		case s >= 13 && s%2 == 1:
			out = append(out, Value{Type: Text, Bytes: raw})
		default:
			// Serial types 10 and 11 are reserved for internal use and must
			// not appear in a database file.
			return nil, fmt.Errorf("sqlite: reserved serial type %d", s)
		}
	}
	return out, nil
}

func serialSize(s uint64) (int, error) {
	switch {
	case s == 0, s == 8, s == 9:
		return 0, nil
	case s >= 1 && s <= 4:
		return int(s), nil
	case s == 5:
		return 6, nil
	case s == 6, s == 7:
		return 8, nil
	case s == 10 || s == 11:
		return 0, fmt.Errorf("sqlite: reserved serial type %d", s)
	case s >= 12:
		n := (s - 12) / 2
		if n > 1<<30 {
			return 0, fmt.Errorf("sqlite: implausible column length %d", n)
		}
		return int(n), nil
	}
	return 0, fmt.Errorf("sqlite: unknown serial type %d", s)
}

// beInt decodes a big-endian two's-complement integer of 1, 2, 3, 4, 6 or 8
// bytes.
func beInt(raw []byte) int64 {
	if len(raw) == 0 {
		return 0
	}
	v := int64(int8(raw[0]))
	for _, b := range raw[1:] {
		v = v<<8 | int64(b)
	}
	return v
}

// uvarint reads a SQLite varint as unsigned. SQLite varints are big-endian,
// up to nine bytes, with the ninth byte contributing all eight of its bits.
func uvarint(b []byte) (uint64, int) {
	var v uint64
	for i := 0; i < 8 && i < len(b); i++ {
		v = v<<7 | uint64(b[i]&0x7f)
		if b[i]&0x80 == 0 {
			return v, i + 1
		}
	}
	if len(b) >= 9 {
		v = v<<8 | uint64(b[8])
		return v, 9
	}
	return 0, 0
}

// varint reads a SQLite varint as a signed 64-bit value.
func varint(b []byte) (int64, int) {
	v, n := uvarint(b)
	return int64(v), n
}
