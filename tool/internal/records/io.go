package records

import (
	"fmt"
	"os"

	"github.com/multiagentworkspace/thrum/tool/internal/format"
)

// Read parses a pure-YAML record. found is false when the file is not there.
func Read(path string, v any) (found bool, err error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := format.DecodeWhole(data, v); err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	return true, nil
}

// Write replaces a pure-YAML record atomically.
func Write(path string, v any) error {
	data, err := format.MarshalYAML(v)
	if err != nil {
		return err
	}
	return format.WriteFileAtomic(path, data)
}

// ReadDoc parses a front-matter record and returns the two halves.
func ReadDoc(path string) (front, body []byte, found bool, err error) {
	front, body, err = format.LoadDocument(path)
	if os.IsNotExist(err) {
		return nil, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, err
	}
	return front, body, true, nil
}
