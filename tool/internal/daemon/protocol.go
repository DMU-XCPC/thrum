// Package daemon is the one long-running process a site may run.
//
// It is the site's endpoint, and it is optional: everything a single machine needs
// works with no daemon at all, because the workspace is files and the command-line
// program writes them itself. What a daemon adds is being *reachable* — answering a
// peer's question, holding a lease another site asked for, and taking delivery of a
// message addressed to an actor here.
//
// It adds no rule of its own. An actor is a name, and nothing here starts or supervises
// anything: the commands that talk to it say what a person is doing, and never name it.
package daemon

import (
	"bufio"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/workspace"
)

// Verb is what a request asks for.
type Verb string

const (
	// VerbStatus is what this site is, and what it holds.
	VerbStatus Verb = "status"
	// VerbRoster is the names this site has seen.
	VerbRoster Verb = "roster"
	// VerbRosterLearn is the push in the other direction: what one site has seen, for a peer
	// to merge into its own. It answers with how many entries it changed.
	VerbRosterLearn Verb = "roster.learn"
	// VerbJob answers two things at once: whether this site holds the job — whether a
	// working directory here has its branch checked out — and what its claim says. It
	// is what makes a claim globally consistent without anybody agreeing on anything:
	// the site that holds the job is the one site whose answer counts, and everyone
	// asks it.
	VerbJob Verb = "job"
	// VerbOccupy and VerbVacate are how the claim at var/occupy/ is written and
	// cleared. An actor never edits it itself: it goes through the tooling.
	VerbOccupy Verb = "occupy"
	VerbVacate Verb = "vacate"
	// VerbHolds asks a site whether it is where a resource lives, which is the only
	// question that can settle where a lease may be taken.
	VerbHolds Verb = "holds"
	// VerbLeaseTake, VerbLeaseRenew and VerbLeaseRelease are taken at the resource's
	// home site, never at the site that wants it.
	VerbLeaseTake    Verb = "lease.take"
	VerbLeaseRenew   Verb = "lease.renew"
	VerbLeaseRelease Verb = "lease.release"
	// VerbDeliver hands a message, with its attachments, to the site of the actor it
	// is addressed to.
	VerbDeliver Verb = "msg.deliver"
	// VerbExec is the forced command a peer runs: the same JSON, in and out.
	VerbExec Verb = "exec"
)

// Request is one call. Args is the verb's own structure, encoded, and Files are the
// attachment bytes when there are any.
type Request struct {
	Verb  Verb            `json:"verb"`
	Args  json.RawMessage `json:"args,omitempty"`
	Files []byte          `json:"files,omitempty"`
}

// Response is one answer. Data is the verb's own structure, encoded.
type Response struct {
	OK    bool            `json:"ok"`
	Error string          `json:"error,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
}

// Fail turns an error into a response.
func Fail(err error) Response {
	if err == nil {
		return Response{OK: true}
	}
	return Response{Error: err.Error()}
}

// Payload wraps a value as a response body.
func Payload(v any) (Response, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return Response{}, err
	}
	return Response{OK: true, Data: data}, nil
}

// DecodeArgs reads a request's arguments into the verb's own structure.
func (r Request) DecodeArgs(into any) error {
	if len(r.Args) == 0 {
		return nil
	}
	return json.Unmarshal(r.Args, into)
}

// Args encodes a verb's arguments for sending.
func Args(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return data
}

// Handler answers one request. The socket and the forced command share it, so that a
// peer and a local command do exactly the same thing.
type Handler func(Request) Response

// Call sends one request and reads the answer.
//
// The framing is a JSON line, then the attachments as raw bytes, then a JSON line back.
// It is deliberately not base64: an attachment is a file, and a file that has been
// through an encoder is a file that has to fit in memory twice.
func Call(conn net.Conn, req Request) (Response, error) {
	if err := write(conn, req); err != nil {
		return Response{}, err
	}
	return read(conn)
}

// write sends one request, with its attachment bytes behind the JSON.
func write(w io.Writer, req Request) error {
	head := Request{Verb: req.Verb, Args: req.Args}
	line, err := json.Marshal(head)
	if err != nil {
		return err
	}
	if _, err := w.Write(append(line, '\n')); err != nil {
		return err
	}
	if len(req.Files) > 0 {
		if _, err := w.Write(req.Files); err != nil {
			return err
		}
	}
	return nil
}

// read reads one answer.
func read(r io.Reader) (Response, error) {
	buffered := bufio.NewReader(r)
	line, err := buffered.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return Response{}, err
	}
	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return Response{}, err
	}
	if !resp.OK {
		return resp, fmt.Errorf("%s", resp.Error)
	}
	return resp, nil
}

// ReadRequest reads one request, and its attachment bytes.
//
// The sizes are in the arguments, so the bytes are read exactly and the connection is
// left where the reply goes. A reader that buffered ahead would swallow them, which is
// why this reads a line rather than decoding a stream.
func ReadRequest(r io.Reader) (Request, error) {
	buffered := bufio.NewReader(r)
	line, err := buffered.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return Request{}, err
	}
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		return Request{}, err
	}

	size, err := attachmentBytes(req.Args)
	if err != nil {
		return Request{}, err
	}
	if size > 0 {
		req.Files = make([]byte, size)
		if _, err := io.ReadFull(buffered, req.Files); err != nil {
			return Request{}, err
		}
	}
	return req, nil
}

// Reply writes one answer.
func Reply(w io.Writer, resp Response) error {
	return json.NewEncoder(w).Encode(resp)
}

// attachmentBytes is how many bytes follow the header, which the receiving side learns
// from the arguments rather than from a length prefix of its own.
func attachmentBytes(args json.RawMessage) (int, error) {
	if len(args) == 0 {
		return 0, nil
	}
	var probe struct {
		Attachments []struct {
			Name string `json:"name"`
			Size int64  `json:"size"`
		} `json:"attachments"`
	}
	if err := json.Unmarshal(args, &probe); err != nil {
		return 0, nil // a verb with no attachments at all
	}
	total := 0
	for _, a := range probe.Attachments {
		if a.Size < 0 {
			return 0, fmt.Errorf("an attachment cannot be a negative size")
		}
		total += int(a.Size)
	}
	return total, nil
}

// DeliverOptions is what handing a message to a site needs.
type DeliverOptions struct {
	Message     json.RawMessage `json:"message"`
	Attachments []struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
	} `json:"attachments,omitempty"`
}

var _ = base64.StdEncoding
var _ = binary.BigEndian

// Local sends one request to the daemon on this site, if there is one. A missing socket
// is not an error: it means this site runs no daemon, and most operations do not care.
func Local(l workspace.Layout, req Request) (Response, bool, error) {
	conn, err := net.DialTimeout("unix", l.DaemonSocket(), time.Second)
	if err != nil {
		return Response{}, false, nil
	}
	defer conn.Close()
	resp, err := Call(conn, req)
	return resp, true, err
}

// Exec runs the forced command: one request in, one response out. It is what a peer
// runs, whether through this daemon's own SSH server or through a system sshd whose
// authorized_keys names it.
func Exec(h Handler, in io.Reader, out io.Writer) error {
	req, err := ReadRequest(in)
	if err != nil {
		return Reply(out, Fail(err))
	}
	return Reply(out, h(req))
}
