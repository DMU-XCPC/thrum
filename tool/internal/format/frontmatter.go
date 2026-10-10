// Package format reads and writes the records a workspace is made of.
//
// Every structured file is either YAML front-matter followed by a Markdown body, or
// pure YAML when there is nothing to say in prose. Documents are edited textually:
// the body is carried through byte for byte and only the delimited front-matter
// block reaches the YAML parser. That is what makes a library without perfect
// comment round-tripping good enough — what must survive is the prose, and the prose
// never passes through a parser.
package format

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// delim is the front-matter delimiter, on a line of its own at both ends.
const delim = "---"

// ErrNoFrontMatter is returned by Split for a file that does not begin with the
// opening delimiter.
var ErrNoFrontMatter = errors.New("no front-matter")

// Split separates a document into its front-matter block and the body after it.
// Both halves come back verbatim, including their newlines, so that
// Join(Split(d)) reproduces d exactly.
func Split(doc []byte) (front, body []byte, err error) {
	if !bytes.HasPrefix(doc, []byte(delim+"\n")) {
		return nil, nil, ErrNoFrontMatter
	}
	rest := doc[len(delim)+1:]

	for i := 0; ; {
		nl := bytes.IndexByte(rest[i:], '\n')
		line := rest[i:]
		if nl >= 0 {
			line = rest[i : i+nl]
		}
		if bytes.Equal(line, []byte(delim)) {
			front = rest[:i]
			if nl >= 0 {
				body = rest[i+nl+1:]
			}
			return front, body, nil
		}
		if nl < 0 {
			return nil, nil, ErrNoFrontMatter
		}
		i += nl + 1
	}
}

// Join is Split's inverse.
func Join(front, body []byte) []byte {
	out := make([]byte, 0, len(front)+len(body)+8)
	out = append(out, delim...)
	out = append(out, '\n')
	out = append(out, front...)
	out = append(out, delim...)
	out = append(out, '\n')
	return append(out, body...)
}

// Decode parses a front-matter block into a record. Unknown fields are refused,
// because the formats are closed: a field a record does not define is a mistake
// rather than an extension. Resource registries are the deliberate exception and use
// DecodeLoose.
func Decode(front []byte, v any) error {
	dec := yaml.NewDecoder(bytes.NewReader(front))
	dec.KnownFields(true)
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("front-matter: %w", err)
	}
	return nil
}

// DecodeLoose parses a front-matter block while keeping unknown fields. A record a
// project extends — a resource registry entry in particular — carries fields the
// tooling does not read, and those belong to whoever reads the file.
func DecodeLoose(front []byte, v any) error {
	if err := yaml.Unmarshal(front, v); err != nil {
		return fmt.Errorf("front-matter: %w", err)
	}
	return nil
}

// Encode renders a record as a front-matter block.
//
// The indent is two spaces, which is what the formats are written in by hand: a
// record the tool writes and a record a person writes should look the same.
func Encode(v any) ([]byte, error) {
	return MarshalYAML(v)
}

// MarshalYAML renders a value with the indentation the formats use.
func MarshalYAML(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("front-matter: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("front-matter: %w", err)
	}
	return buf.Bytes(), nil
}

// LoadDocument reads a document and returns its two halves.
func LoadDocument(path string) (front, body []byte, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	front, body, err = Split(b)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", path, err)
	}
	return front, body, nil
}

// SaveDocument writes a document back, replacing the file atomically.
func SaveDocument(path string, front, body []byte) error {
	return WriteFileAtomic(path, Join(front, body))
}

// WriteFileAtomic writes data by way of a temporary file in the same directory and a
// rename, so that a crash leaves either the old file or the new one and never a
// half-written one. Every replacement in this tool is done this way.
func WriteFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		if tmp != "" {
			_ = os.Remove(tmp)
		}
	}()

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	tmp = ""
	return nil
}
