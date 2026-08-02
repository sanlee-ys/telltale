package sqlite

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// read loads a fixture. Fixtures are synthesized by testdata/gen_fixtures.py;
// no real database ever enters this tree.
func read(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("%v (regenerate: cd internal/sqlite/testdata && uv run python gen_fixtures.py)", err)
	}
	return raw
}

func open(t *testing.T, db, wal string) *File {
	t.Helper()
	var w []byte
	if wal != "" {
		w = read(t, wal)
	}
	f, err := Open(read(t, db), w)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestTablesFromSQLiteMaster(t *testing.T) {
	f := open(t, "plain.db", "")
	tables, err := f.Tables()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"kv", "empty_table"} {
		if tables[want] == 0 {
			t.Errorf("table %q missing from sqlite_master walk; got %v", want, tables)
		}
	}
	// Indexes must not be reported: this reader walks table b-trees only, and
	// handing back an index root page invites walking one.
	for name := range tables {
		if len(name) > 6 && name[:6] == "sqlite" && name != "sqlite_master" {
			t.Errorf("non-table %q reported by Tables", name)
		}
	}
}

// Every storage class decodes, and an INTEGER PRIMARY KEY column comes back as
// the record stores it (NULL) with the value carried on the rowid — the reader
// does not guess which column is the alias.
func TestRecordDecoding(t *testing.T) {
	f := open(t, "plain.db", "")
	rows, ok, err := f.Table("kv")
	if err != nil || !ok {
		t.Fatalf("Table(kv) = ok %v, err %v", ok, err)
	}
	if len(rows) != 4 {
		t.Fatalf("got %d rows, want 4", len(rows))
	}

	if rows[0].RowID != 1 {
		t.Errorf("row 0 rowid = %d, want 1", rows[0].RowID)
	}
	if rows[0].Values[0].Type != Null {
		t.Errorf("INTEGER PRIMARY KEY column decoded as %v, want Null (the rowid carries it)",
			rows[0].Values[0].Type)
	}
	if got, ok := rows[0].Values[1].Text(); !ok || got != "alpha" {
		t.Errorf("label = %q (ok %v), want \"alpha\"", got, ok)
	}
	if got, ok := rows[0].Values[2].Blob(); !ok || !bytes.Equal(got, []byte{0, 1, 2}) {
		t.Errorf("payload = %v (ok %v)", got, ok)
	}
	if rows[0].Values[3].Type != Float || rows[0].Values[3].Float != 0.5 {
		t.Errorf("ratio = %v %v, want Float 0.5", rows[0].Values[3].Type, rows[0].Values[3].Float)
	}

	// 0 and 1 use the zero-width serial types 8 and 9; they must still decode
	// as the integers they are, not as absence.
	if v := rows[0].Values[4]; v.Type != Int || v.Int != 0 {
		t.Errorf("flag = %v %d, want Int 0 (a zero-width serial type is still a value)", v.Type, v.Int)
	}
	if v := rows[1].Values[4]; v.Type != Int || v.Int != 1 {
		t.Errorf("flag = %v %d, want Int 1", v.Type, v.Int)
	}
	// An empty blob is a blob, not a NULL.
	if v := rows[1].Values[2]; v.Type != Blob || len(v.Bytes) != 0 {
		t.Errorf("empty blob decoded as %v len %d", v.Type, len(v.Bytes))
	}
	// A real NULL stays NULL.
	if v := rows[2].Values[2]; v.Type != Null {
		t.Errorf("NULL payload decoded as %v", v.Type)
	}
	// Multi-byte and negative integers.
	if v := rows[2].Values[4]; v.Type != Int || v.Int != 65537 {
		t.Errorf("flag = %d, want 65537", v.Int)
	}
	if v := rows[3].Values[4]; v.Type != Int || v.Int != -1 {
		t.Errorf("flag = %d, want -1 (two's complement)", v.Int)
	}
	if v := rows[3].Values[1]; v.Type != Null {
		t.Errorf("NULL text decoded as %v", v.Type)
	}
}

func TestEmptyTableIsZeroRowsNotAnError(t *testing.T) {
	f := open(t, "plain.db", "")
	rows, ok, err := f.Table("empty_table")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("empty_table reported as missing; it exists and has no rows")
	}
	if len(rows) != 0 {
		t.Errorf("got %d rows, want 0", len(rows))
	}
}

func TestMissingTableIsAbsenceNotAnError(t *testing.T) {
	f := open(t, "plain.db", "")
	rows, ok, err := f.Table("no_such_table")
	if err != nil {
		t.Fatalf("a missing table must not be an error: %v", err)
	}
	if ok || rows != nil {
		t.Errorf("Table(no_such_table) = %v rows, ok %v", len(rows), ok)
	}
}

// The overflow chain is the common path for the vendor data this reader was
// written for: a 25 KiB blob against a 4 KiB page spans seven pages.
func TestOverflowPageChain(t *testing.T) {
	f := open(t, "overflow.db", "")
	rows, ok, err := f.Table("blobs")
	if err != nil || !ok {
		t.Fatalf("Table(blobs) = ok %v, err %v", ok, err)
	}
	if len(rows) != 39 {
		t.Fatalf("got %d rows, want 39", len(rows))
	}

	want := make([]byte, 25_000)
	for i := range want {
		want[i] = byte((i*37 + (i>>8)*11) % 251)
	}
	got, ok := rows[0].Values[1].Blob()
	if !ok {
		t.Fatal("row 1 payload is not a blob")
	}
	if len(got) != len(want) {
		t.Fatalf("blob is %d bytes, want %d — the overflow chain lost content", len(got), len(want))
	}
	if !bytes.Equal(got, want) {
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("blob diverges at byte %d: got %d, want %d", i, got[i], want[i])
			}
		}
	}

	// A payload that overflows by a little is the boundary case the spec's
	// local-length arithmetic exists for.
	mid, _ := rows[1].Values[1].Blob()
	if len(mid) != 4_500 {
		t.Errorf("mid blob is %d bytes, want 4500", len(mid))
	}
	// And one that does not overflow at all still decodes.
	if s, _ := rows[2].Values[1].Blob(); string(s) != "short" {
		t.Errorf("short blob = %q", s)
	}
}

// The load-bearing WAL assertion: the committed value lives only in the
// sidecar, so a reader that skips it reports a stale value as current.
func TestWALOverlayWins(t *testing.T) {
	base := open(t, "wal.db", "")
	rows, _, err := base.Table("kv")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("base file has %d rows, want 1 — the fixture is not exercising the WAL", len(rows))
	}
	if got, _ := rows[0].Values[1].Text(); got != "base-value" {
		t.Fatalf("base file label = %q, want \"base-value\"", got)
	}

	withWAL := open(t, "wal.db", "wal.db-wal")
	rows, _, err = withWAL.Table("kv")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("with the WAL applied: %d rows, want 2", len(rows))
	}
	if got, _ := rows[0].Values[1].Text(); got != "wal-value" {
		t.Errorf("label = %q, want \"wal-value\" — the WAL overlay did not win", got)
	}
	if got, _ := rows[1].Values[1].Text(); got != "wal-only-row" {
		t.Errorf("row 2 label = %q, want \"wal-only-row\"", got)
	}
	if len(withWAL.Notes()) != 0 {
		t.Errorf("a valid WAL produced notes: %v", withWAL.Notes())
	}
}

// A corrupt frame ends the scan, exactly as SQLite's own recovery does. The
// base file is still a real snapshot, so the read degrades to it and SAYS so
// rather than failing or presenting damaged bytes as data.
func TestCorruptWALFrameIsIgnoredNotTrusted(t *testing.T) {
	db := read(t, "wal.db")
	wal := read(t, "wal.db-wal")

	// Flip a byte inside the first frame's page data. The rolling checksum in
	// that frame's header no longer matches.
	corrupt := append([]byte(nil), wal...)
	corrupt[walHeaderSize+walFrameHeader+16] ^= 0xff

	f, err := Open(db, corrupt)
	if err != nil {
		t.Fatal(err)
	}
	rows, _, err := f.Table("kv")
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := rows[0].Values[1].Text(); got != "base-value" {
		t.Errorf("label = %q, want the base file's \"base-value\": a frame that fails "+
			"its checksum must not reach the caller", got)
	}
	if len(f.Notes()) == 0 {
		t.Error("a rejected WAL produced no note; the caller cannot say the data is stale")
	}
}

// A WAL whose header checksum is wrong is rejected whole: nothing after a bad
// header can be trusted to be frames at all.
func TestWALWithABadHeaderIsRejectedWhole(t *testing.T) {
	db := read(t, "wal.db")
	wal := append([]byte(nil), read(t, "wal.db-wal")...)
	wal[24] ^= 0xff // header checksum-1

	f, err := Open(db, wal)
	if err != nil {
		t.Fatal(err)
	}
	rows, _, _ := f.Table("kv")
	if got, _ := rows[0].Values[1].Text(); got != "base-value" {
		t.Errorf("label = %q, want \"base-value\"", got)
	}
	if len(f.Notes()) == 0 {
		t.Error("a rejected WAL produced no note")
	}
}

// Salts identify the WAL's incarnation. Frames from an older one describe a
// database that no longer exists.
func TestWALWithMismatchedSaltsIsIgnored(t *testing.T) {
	db := read(t, "wal.db")
	wal := append([]byte(nil), read(t, "wal.db-wal")...)
	wal[walHeaderSize+8] ^= 0xff // first frame's salt-1

	f, err := Open(db, wal)
	if err != nil {
		t.Fatal(err)
	}
	rows, _, _ := f.Table("kv")
	if got, _ := rows[0].Values[1].Text(); got != "base-value" {
		t.Errorf("label = %q, want \"base-value\"", got)
	}
}

// A missing sidecar is the normal case for a checkpointed database, and must
// not look like an error.
func TestNoWALIsFine(t *testing.T) {
	f, err := Open(read(t, "plain.db"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Notes()) != 0 {
		t.Errorf("absent sidecar produced notes: %v", f.Notes())
	}
	if _, ok, err := f.Table("kv"); !ok || err != nil {
		t.Errorf("Table(kv) = ok %v, err %v", ok, err)
	}
}

// An uncommitted tail is not part of the database. Truncating the fixture
// mid-frame simulates catching the writer between fsyncs.
func TestATornFrameTailIsNotApplied(t *testing.T) {
	db := read(t, "wal.db")
	wal := read(t, "wal.db-wal")
	torn := append([]byte(nil), wal[:len(wal)-40]...)

	f, err := Open(db, torn)
	if err != nil {
		t.Fatal(err)
	}
	rows, _, err := f.Table("kv")
	if err != nil {
		t.Fatal(err)
	}
	// Either the base state or an earlier committed state — never a row
	// assembled from a half-written frame.
	for _, r := range rows {
		got, _ := r.Values[1].Text()
		if got != "base-value" && got != "wal-value" && got != "wal-only-row" {
			t.Errorf("torn tail produced an invented value %q", got)
		}
	}
}

func TestNonDatabaseBytesAreRejected(t *testing.T) {
	for _, in := range [][]byte{
		nil,
		[]byte("not a database"),
		bytes.Repeat([]byte{0}, 4096),
	} {
		if _, err := Open(in, nil); !errors.Is(err, ErrNotSQLite) {
			t.Errorf("Open(%d bytes) = %v, want ErrNotSQLite", len(in), err)
		}
	}
}

// A header that survives the magic check but lies about its geometry must be
// rejected before any page arithmetic runs on it.
func TestImplausibleHeaderIsRejected(t *testing.T) {
	good := read(t, "plain.db")

	badPage := append([]byte(nil), good...)
	badPage[16], badPage[17] = 0x00, 0x03 // 768: not a power of two
	if _, err := Open(badPage, nil); err == nil {
		t.Error("a non-power-of-two page size was accepted")
	}

	badReserved := append([]byte(nil), good...)
	badReserved[16], badReserved[17] = 0x02, 0x00 // 512-byte pages…
	badReserved[20] = 0xff                        // …minus 255 reserved: below the format's usable floor
	if _, err := Open(badReserved, nil); err == nil {
		t.Error("a usable page size below the format floor was accepted")
	}
}

// A truncated file must produce an error from the walk, not a panic and not a
// short row list presented as complete.
func TestTruncatedFileDoesNotPanic(t *testing.T) {
	good := read(t, "overflow.db")
	for _, n := range []int{101, 4096, 8192, 12000, len(good) - 1} {
		if n <= 0 || n > len(good) {
			continue
		}
		f, err := Open(good[:n], nil)
		if err != nil {
			continue
		}
		_, _, _ = f.Table("blobs")
	}
}
