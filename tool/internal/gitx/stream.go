package gitx

import (
	"bytes"
	"io"
	"os/exec"
)

// Stream runs git and hands back its standard output as a stream, which is how a
// payload too large to hold in memory is read. The returned function must be called to
// release the process and to learn whether git was happy.
func Stream(dir string, args ...string) (io.ReadCloser, func() error, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(), "LC_ALL=C", "LANG=C")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	wait := func() error {
		if err := cmd.Wait(); err != nil {
			return &Error{Args: args, Stderr: stderr.String(), Err: err}
		}
		return nil
	}
	return stdout, wait, nil
}

// RunInput runs git with the given bytes on its standard input.
func RunInput(dir string, input []byte, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(), "LC_ALL=C", "LANG=C")
	cmd.Stdin = bytes.NewReader(input)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", &Error{Args: args, Stderr: stderr.String(), Err: err}
	}
	return stdout.String(), nil
}
