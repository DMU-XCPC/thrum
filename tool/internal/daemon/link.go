package daemon

import (
	"encoding/json"
	"net"
	"sync"
	"time"
)

// peerLink is a live connection to a peer site. The incoming side is read by the
// serve loop and written here; the outgoing side is a Client. It carries requests,
// queues nothing, and is site-level: it belongs to no actor and no job.
type peerLink struct {
	conn     net.Conn
	enc      *json.Encoder
	site     string
	outgoing bool
	writeMu  sync.Mutex
	lastSeen time.Time
}

func newPeerLink(conn net.Conn, enc *json.Encoder, site string, outgoing bool) *peerLink {
	return &peerLink{conn: conn, enc: enc, site: site, outgoing: outgoing, lastSeen: time.Now()}
}

// send writes one frame — a request or a response — under the link's write lock.
// The serve loop and the daemon's sender both use it, so their frames never
// interleave on the wire.
func (p *peerLink) send(v any) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	return p.enc.Encode(v)
}

func (p *peerLink) seen() { p.lastSeen = time.Now() }

func (p *peerLink) close() error { return p.conn.Close() }
