//go:build race

package render

// The race detector slows rendering about fifteen-fold.
const raceSlowdown = 15
