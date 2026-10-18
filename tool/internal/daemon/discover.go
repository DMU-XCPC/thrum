package daemon

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/multiagentworkspace/thrum/tool/internal/discovery"
	"github.com/multiagentworkspace/thrum/tool/internal/format"
	"github.com/multiagentworkspace/thrum/tool/internal/layout"
)

// discoveredFor is how long an announcement is a usable hint. It is three of the
// default intervals; the address is a hint, so a stale one is ignored, not trusted.
const discoveredFor = 15 * time.Second

type discovered struct {
	Reach string `yaml:"reach"`
	Seen  string `yaml:"seen"`
}

func discoveredPath(l layout.Layout, site string) string {
	return filepath.Join(l.Var(), "discovered", site+".yaml")
}

// discoveredReach is the last address a peer announced, if it is still fresh.
func discoveredReach(l layout.Layout, site string, maxAge time.Duration) string {
	var d discovered
	data, err := os.ReadFile(discoveredPath(l, site))
	if err != nil {
		return ""
	}
	if format.DecodeWhole(data, &d) != nil {
		return ""
	}
	seen, err := time.Parse(format.RFC3339UTC, d.Seen)
	if err != nil {
		return ""
	}
	if maxAge > 0 && time.Since(seen) > maxAge {
		return ""
	}
	return d.Reach
}

// advertisedReach is what this daemon tells the LAN: the address a peer can dial.
// It is the local or declared reach when there is one, otherwise the listen address
// with the wildcard host replaced by the machine's outbound address.
func (s *server) advertisedReach() string {
	if r := reachFor(s.l, s.site, rosterOf(s.l)); r != "" {
		return r
	}
	host, port, err := net.SplitHostPort(s.listen)
	if err != nil {
		return ""
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = outboundIP()
	}
	if host == "" || port == "" {
		return ""
	}
	return net.JoinHostPort(host, port)
}

func outboundIP() string {
	c, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer c.Close()
	if a, ok := c.LocalAddr().(*net.UDPAddr); ok {
		return a.IP.String()
	}
	return ""
}

// discoverLoop announces this site on the interval, records the peers it hears, and
// answers a join query for this site.
func (s *server) discoverLoop(conn *discovery.Socket) {
	buf := make([]byte, 4096)
	go func() {
		for {
			p, addr, err := conn.Recv(buf)
			if err != nil {
				select {
				case <-s.stop:
					return
				default:
				}
				if errors.Is(err, syscall.EBADF) {
					return
				}
				continue
			}
			if p.Workspace != s.workspace {
				continue
			}
			if p.Query {
				if p.Peer != s.site {
					continue
				}
				if reach := s.advertisedReach(); reach != "" {
					_ = conn.Reply(addr, discovery.Packet{
						Workspace: s.workspace, Site: s.site, Reach: reach,
						Version: protocolVersion, Sent: time.Now().UTC().Format(time.RFC3339),
					})
				}
				continue
			}
			if p.Site == s.site || p.Reach == "" {
				continue
			}
			data, err := format.MarshalYAML(discovered{
				Reach: p.Reach, Seen: time.Now().UTC().Format(format.RFC3339UTC),
			})
			if err != nil {
				continue
			}
			_ = os.MkdirAll(filepath.Dir(discoveredPath(s.l, p.Site)), 0o755)
			_ = format.WriteFileAtomic(discoveredPath(s.l, p.Site), data)
		}
	}()
	announce := func() {
		if reach := s.advertisedReach(); reach != "" {
			_ = conn.Send(discovery.Packet{
				Workspace: s.workspace, Site: s.site, Reach: reach,
				Version: protocolVersion, Sent: time.Now().UTC().Format(time.RFC3339),
			})
		}
	}
	announce()
	t := time.NewTicker(s.discoverEvery)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
			announce()
		}
	}
}
