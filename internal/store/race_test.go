//go:build race

package store

// raceEnabled is true when tests run under the race detector, which slows
// SQLite by orders of magnitude, so timing assertions are skipped.
const raceEnabled = true
