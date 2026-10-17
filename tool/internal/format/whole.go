package format

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// DecodeWhole parses a pure-YAML record, refusing fields the record does not define.
func DecodeWhole(data []byte, v any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(v); err != nil {
		return err
	}
	return nil
}

// ReadWhole reads a pure-YAML record. A missing file is not an error.
func ReadWhole(path string, v any) (bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := DecodeWhole(data, v); err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	return true, nil
}
