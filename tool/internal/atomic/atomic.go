// Package atomic is the retry discipline the design prescribes for the one file that
// holds several entries.
//
// There is no lock for it: read, change, and write only if the file is unchanged since
// the read, otherwise retry — which is cheaper than persistent state and is what the
// design says to do instead.
package atomic

import (
	"bytes"
	"fmt"
	"os"

	"github.com/multiagentworkspace/thrum/tool/internal/format"
)

// Retries is how many times a contended change is attempted before giving up and
// letting the caller decide.
const Retries = 5

// Replace applies a change to a file, retrying when somebody else wrote in between.
//
// apply is handed the file's current contents, or nil when it is absent, and returns
// what the file should say instead. Returning nil means the file should not exist.
// The result is written only while the file still holds what apply was given;
// otherwise apply is called again with what is there now.
func Replace(path string, apply func(current []byte) ([]byte, error)) error {
	for attempt := 0; attempt < Retries; attempt++ {
		before, err := read(path)
		if err != nil {
			return err
		}
		after, err := apply(before)
		if err != nil {
			return err
		}
		if bytes.Equal(before, after) {
			return nil
		}

		now, err := read(path)
		if err != nil {
			return err
		}
		if !bytes.Equal(before, now) {
			continue // somebody wrote in between: work the change out again
		}
		if after == nil {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
			return nil
		}
		return format.WriteFileAtomic(path, after)
	}
	return fmt.Errorf("%s kept changing under us", path)
}

// read returns a file's contents, or nil when it is not there.
func read(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return b, nil
}
