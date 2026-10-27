//go:build !unix

package snapshot

// matchOwner has nothing to do where files have no Unix owner.
func matchOwner(string, ...string) error { return nil }
