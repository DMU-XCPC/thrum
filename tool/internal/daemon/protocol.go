// Package daemon is the one long-running process a site may run.
//
// It is optional: every operation works with no daemon running, because the
// command-line program does its work locally and returns. The daemon exists for the
// three things a file cannot do — being alive, waking something, and holding an
// invariant across machines — and it adds no rule of its own.
package daemon

import (
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
	// VerbStatus reports what this site knows: its actors, its peers, its occupancy.
	VerbStatus Verb = "status"
	// VerbOccupy and VerbVacate are how an actor touches the site-local note: it goes
	// through here rather than editing the file, because the note describes a running
	// world and this is what runs it.
	VerbOccupy Verb = "occupy"
	VerbVacate Verb = "vacate"
	// VerbStart and VerbStop run an actor on a job.
	VerbStart Verb = "start"
	VerbStop  Verb = "stop"
	// VerbAnnounce and VerbNotify are a job reaching done. Announce is what the site
	// where it happened says, and it is the only thing that fans out; Notify is what a
	// peer hears, and it wakes that peer's own waiters and stops there. One hop is all
	// the design asks for, and a broadcast that re-broadcasts never ends.
	VerbAnnounce Verb = "announce"
	VerbNotify   Verb = "notify"
	// VerbAsk answers who is on a job at this site.
	VerbAsk Verb = "ask"
	// VerbLease takes or releases a unit at the site the resource lives on.
	VerbLeaseTake    Verb = "lease.take"
	VerbLeaseRenew   Verb = "lease.renew"
	VerbLeaseRelease Verb = "lease.release"
	// VerbPeers reports the roster and which peers are reachable.
	VerbPeers Verb = "peers"
	// VerbHolds asks a site whether it is where a resource lives, which is the only
	// question that can settle where a lease may be taken.
	VerbHolds Verb = "holds"
	// VerbExec is the forced command a peer runs: the same JSON, in and out.
	VerbExec Verb = "exec"
)

// Request is one call. Args is the verb's own structure, encoded.
type Request struct {
	Verb Verb            `json:"verb"`
	Args json.RawMessage `json:"args,omitempty"`
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

// Call sends one request over a connection and reads the answer.
func Call(conn net.Conn, req Request) (Response, error) {
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return Response{}, err
	}
	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return Response{}, err
	}
	if !resp.OK {
		return resp, fmt.Errorf("%s", resp.Error)
	}
	return resp, nil
}

// Local sends one request to the daemon on this site, if there is one. A missing
// socket is not an error: it means there is no daemon, and the caller does the work
// itself.
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
	var req Request
	if err := json.NewDecoder(in).Decode(&req); err != nil {
		return json.NewEncoder(out).Encode(Fail(err))
	}
	return json.NewEncoder(out).Encode(h(req))
}
