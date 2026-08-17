package hud

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sanlee-ys/telltale/internal/model"
	"github.com/sanlee-ys/telltale/internal/quotacache"
	"github.com/sanlee-ys/telltale/internal/usagecache"
)

// The launch-time hide (§7.20) removes a vendor from every surface at once:
// the snapshot is stripped as the scan lands, so the grid, the vendor lines,
// the relayed quota and the relayed spend cannot disagree about who is hidden.

func hiddenSnap() Snapshot {
	return Snapshot{
		At: pinned,
		Sessions: []*model.Session{
			sess(model.VendorClaude, "00000000-aaaa-4bbb-8ccc-00000000h001",
				`C:\src\code\telltale`, "claude-opus-5", 10*time.Second),
			sess(model.VendorGemini, "00000000-aaaa-4bbb-8ccc-00000000h002",
				`C:\src\code\notes-api`, "gemini-3-pro", 20*time.Second),
			sess(model.VendorCursor, "00000000-aaaa-4bbb-8ccc-00000000h003",
				`C:\src\code\desk`, "composer-2.5", 30*time.Second),
		},
		Vendors: []VendorView{
			watching(model.VendorClaude, `%USERPROFILE%\.claude\projects`, fullCaps),
			watching(model.VendorGemini, `%USERPROFILE%\.gemini\tmp`, fullCaps),
			watching(model.VendorCursor, `%USERPROFILE%\.cursor`, fullCaps),
		},
		Account: []quotacache.Account{
			{Vendor: model.VendorClaude, WrittenAt: pinned},
			{Vendor: model.VendorGemini, WrittenAt: pinned},
		},
		Spend: []usagecache.Total{
			{Vendor: model.VendorCursor},
		},
	}
}

func TestDropHiddenStripsEveryVendorSurface(t *testing.T) {
	got := dropHidden(hiddenSnap(), []model.VendorID{model.VendorCursor, model.VendorGemini})

	if len(got.Sessions) != 1 || got.Sessions[0].Vendor != model.VendorClaude {
		t.Errorf("sessions kept %d rows, want claude's only: %+v", len(got.Sessions), keys(got.Sessions))
	}
	if len(got.Vendors) != 1 || got.Vendors[0].Vendor != model.VendorClaude {
		t.Errorf("vendor lines kept %d entries, want claude's only", len(got.Vendors))
	}
	if len(got.Account) != 1 || got.Account[0].Vendor != model.VendorClaude {
		t.Errorf("relayed quota kept %d entries, want claude's only", len(got.Account))
	}
	if len(got.Spend) != 0 {
		t.Errorf("relayed spend kept %d entries for a hidden vendor", len(got.Spend))
	}
}

func TestAnEmptyHideListChangesNothing(t *testing.T) {
	snap := hiddenSnap()
	got := dropHidden(snap, nil)
	if len(got.Sessions) != 3 || len(got.Vendors) != 3 || len(got.Account) != 2 || len(got.Spend) != 1 {
		t.Errorf("an empty hide list altered the snapshot: %+v", got)
	}
}

// The v cycle steps over hidden vendors: a filter that can only ever select an
// empty grid is a dead stop on a one-key cycle. FilterAll is never skipped.
func TestVendorCycleSkipsHiddenVendors(t *testing.T) {
	m := New(Options{Hide: []model.VendorID{model.VendorCursor, model.VendorGemini}})
	m.st.Now = pinned
	m = send(t, m, tea.WindowSizeMsg{Width: 120, Height: 10})

	for _, want := range []Filter{
		FilterClaude, FilterCodex, FilterAntigravity, FilterGrok, FilterPi,
		FilterSelfReported, FilterAll,
	} {
		m = send(t, m, key("v"))
		if m.st.Filter != want {
			t.Fatalf("filter = %v, want %v", m.st.Filter, want)
		}
	}
}

// The footer states the hide for the whole run. A hidden vendor's rows are not
// merely absent but excluded, and nothing else on screen can say so: the
// hidden vendors leave the header census entirely.
func TestTheFooterStatesTheHide(t *testing.T) {
	st := NewState()
	st.Now = pinned
	st.Width, st.Height = 120, 14
	st.Snap = dropHidden(hiddenSnap(), []model.VendorID{model.VendorCursor, model.VendorGemini})
	st.Hidden = []model.VendorID{model.VendorCursor, model.VendorGemini}

	out := Render(st, PlainStyles(), UnicodeGlyphs())
	if !strings.Contains(out, "hidden cursor gemini") {
		t.Errorf("the footer does not state the hide:\n%s", out)
	}
	for _, word := range []string{"gemini", "cursor"} {
		if strings.Count(out, word) != 1 {
			t.Errorf("%q appears somewhere beyond the footer notice — a hidden vendor rendered:\n%s", word, out)
		}
	}
}
