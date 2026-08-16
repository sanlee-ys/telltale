// Package bindaddr holds the three things a loopback-only listener in this
// repo needs at bind time and cannot get from net alone: whether a host is
// loopback, whether a failed bind failed because something already holds the
// address, and which port a collision message should suggest instead.
//
// Two modes bind this way and both meet the same collision: `telltale otel
// grok` (internal/grokotel, design.md §7.16a) on 127.0.0.1:4318, and
// `telltale events` (internal/eventsink, §7.21) on 127.0.0.1:4519. What each
// mode SAYS about a collision is entirely its own — the sink names emitters
// and the collector names OTLP exporters — so the prose stays in each package
// and only the mechanism lives here.
//
// # Why the mechanism is shared and the prose is not
//
// InUse is the reason this is a package rather than a helper written twice.
// It is not the one errors.Is call it looks like it should be, the reason is
// measured rather than assumed, and a second copy is a second place for a
// later reader to "simplify" it back to the wrong one arm. Windows is the
// primary target (ADR-002), so that simplification would pass CI on the
// platform CI does not run and fail on the one it does.
package bindaddr

import (
	"errors"
	"net"
	"strconv"
	"syscall"
)

// wsaeAddrInUse is Winsock's WSAEADDRINUSE. See InUse for why the number is
// written out here rather than reached through syscall.EADDRINUSE.
const wsaeAddrInUse = 10048

// InUse reports whether a bind failed because something already holds the
// address.
//
// Measured 2026-08-16, go 1.26 on Windows 11: a second net.Listen on a held
// 127.0.0.1 port returns syscall.Errno(10048) — WSAEADDRINUSE — while Windows
// builds define syscall.EADDRINUSE as one of Go's synthetic APPLICATION_ERROR
// constants, 536870914 on that box. So errors.Is(err, syscall.EADDRINUSE) is
// FALSE for the very error it names, on this repo's primary target (ADR-002).
// The numeric arm covers that; the errors.Is arm covers the Unix builds, where
// syscall.EADDRINUSE is the real errno. 10048 is not a valid errno on those
// platforms, so the numeric arm cannot fire there by accident.
//
// TestARealCollisionIsDetectedOnThisPlatform holds a port and binds it again,
// so whichever arm this platform needs is the arm the suite exercises.
func InUse(err error) bool {
	if errors.Is(err, syscall.EADDRINUSE) {
		return true
	}
	var errno syscall.Errno
	return errors.As(err, &errno) && uintptr(errno) == wsaeAddrInUse
}

// IsLoopback reports whether host is one a loopback-only mode may bind. The
// literal "localhost" is accepted beside the addresses because a mode's own
// default may be written either way; note that on Windows it resolves to ::1
// first, which §7.21's Trap 3 measured costing the event emitter a full
// connect-retry cycle on every hook.
func IsLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Next is the port a collision message suggests: the failed port plus one, so
// a message about an already-moved listener never suggests the port that just
// failed. An unparseable or last port falls back to fallback plus one, which
// is the caller's own default port — arithmetic on nonsense is worse than a
// known-good starting point.
//
// The suggestion is a starting point and each caller's message says nothing
// more about it. telltale does NOT scan for a free port and offer that: a port
// free at the moment of the scan is not free at the moment of the bind, and a
// suggestion that looked verified would be the dishonest one (§4a.1).
func Next(host, port, fallback string) string {
	n, err := strconv.Atoi(port)
	if err != nil || n <= 0 || n >= 65535 {
		n, _ = strconv.Atoi(fallback)
	}
	return net.JoinHostPort(host, strconv.Itoa(n+1))
}
