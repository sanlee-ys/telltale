//go:build windows

package councilhost

import "testing"

// TestARebuiltBinaryDoesNotDisownItsHost.
//
// The name half of verifyHostProcess, pinned against the shape the go command
// leaves behind on Windows. It will not overwrite a mapped executable, so a
// `go install` over a running host renames the host's file to `telltale.exe~`
// and the probe reads that name back. Before this test the probe called that
// a reused pid, and `telltale council` then removed host.json and rebuilt five
// seats over a host that was alive (measured 2026-09-03, sameImage's comment).
//
// The forgiveness is exactly one trailing `~`. Anything else is still a
// stranger, because the name half is what keeps `telltale council kill` from
// ending whatever process took the number.
func TestARebuiltBinaryDoesNotDisownItsHost(t *testing.T) {
	cases := []struct {
		image, self string
		want        bool
	}{
		{"telltale.exe", "telltale.exe", true},
		{"TELLTALE.EXE", "telltale.exe", true},
		{"telltale.exe~", "telltale.exe", true},
		{"TELLTALE.EXE~", "telltale.exe", true},
		{"telltale.exe~~", "telltale.exe", false},
		{"telltale.exe", "telltale.exe~", false},
		{"chrome.exe", "telltale.exe", false},
		{"~", "telltale.exe", false},
		{"", "telltale.exe", false},
	}
	for _, c := range cases {
		if got := sameImage(c.image, c.self); got != c.want {
			t.Errorf("sameImage(%q, %q) = %v, want %v", c.image, c.self, got, c.want)
		}
	}
}
