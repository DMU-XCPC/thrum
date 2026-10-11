//go:build !linux

package watch

// watchMany answers that this platform cannot watch, which sends the follower to its timer.
//
// The tag is !linux rather than !unix because inotify is Linux's, and a system that is
// neither gets the same answer the timer gives — reached the same way. (The rest of the
// tool builds for unix only, so this file is a promise about the shape rather than about a
// platform anyone runs today.)
func watchMany(paths []string) (<-chan struct{}, func() error, func(), error) {
	return nil, nil, nil, errNoWatch
}
