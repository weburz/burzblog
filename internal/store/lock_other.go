//go:build !unix && !windows

package store

// lockDir does nothing where flock is missing: stopping the server
// before a restore is then up to whoever runs it.
func lockDir(string, string, bool) (func(), error) {
	return func() {}, nil
}
