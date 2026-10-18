// Package discovery is the LAN hint: a daemon announces its address on a multicast
// group and a joining machine asks for one. The network is trusted, so a packet is a
// hint, not a credential: the site key still authenticates the connection.
//
// The socket is built from syscalls rather than net.ListenMulticastUDP: several
// daemons on one host must receive their own and each other's announcements, and the
// loopback delivery that gives them that is easiest to get right at this level.
package discovery

import (
	"encoding/json"
	"net"
	"syscall"
	"time"
)

const (
	// Group is the IPv4 local-scope group thrum uses on the LAN.
	Group = "239.255.77.70"
	// Port is fixed so no configuration is needed on either side.
	Port = 7709
)

// Packet is both the announcement and the question. A query names the site it wants
// in Peer; an answer is the ordinary announcement from that site.
type Packet struct {
	Workspace string `json:"workspace"`
	Site      string `json:"site"`
	Reach     string `json:"reach,omitempty"`
	Version   string `json:"version,omitempty"`
	Sent      string `json:"sent,omitempty"`
	Query     bool   `json:"query,omitempty"`
	Peer      string `json:"peer,omitempty"`
}

// Addr is where a question came from, so an answer can go back to it.
type Addr = syscall.SockaddrInet4

// Socket is the joined multicast socket.
type Socket struct {
	fd int
}

func groupAddr() *syscall.SockaddrInet4 {
	ip := net.ParseIP(Group).To4()
	return &syscall.SockaddrInet4{Port: Port, Addr: [4]byte{ip[0], ip[1], ip[2], ip[3]}}
}

func socket() (int, error) {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM, syscall.IPPROTO_UDP)
	if err != nil {
		return -1, err
	}
	if err := syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1); err != nil {
		_ = syscall.Close(fd)
		return -1, err
	}
	// Without this the sender's own host never hears the announcement.
	if err := syscall.SetsockoptInt(fd, syscall.IPPROTO_IP, syscall.IP_MULTICAST_LOOP, 1); err != nil {
		_ = syscall.Close(fd)
		return -1, err
	}
	return fd, nil
}

// Listen joins the group on the fixed port. Several daemons on one host may listen.
func Listen() (*Socket, error) {
	fd, err := socket()
	if err != nil {
		return nil, err
	}
	if err := syscall.Bind(fd, &syscall.SockaddrInet4{Port: Port}); err != nil {
		_ = syscall.Close(fd)
		return nil, err
	}
	mreq := &syscall.IPMreq{Multiaddr: [4]byte{239, 255, 77, 70}}
	if err := syscall.SetsockoptIPMreq(fd, syscall.IPPROTO_IP, syscall.IP_ADD_MEMBERSHIP, mreq); err != nil {
		_ = syscall.Close(fd)
		return nil, err
	}
	tv := syscall.NsecToTimeval(int64(time.Second))
	if err := syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv); err != nil {
		_ = syscall.Close(fd)
		return nil, err
	}
	return &Socket{fd: fd}, nil
}

// Close releases the socket.
func (s *Socket) Close() error { return syscall.Close(s.fd) }

func send(fd int, data []byte, to *syscall.SockaddrInet4) error {
	return syscall.Sendto(fd, data, 0, to)
}

// Send announces this site to the group.
func (s *Socket) Send(p Packet) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return send(s.fd, data, groupAddr())
}

// Reply answers a question at the address it came from.
func (s *Socket) Reply(to *Addr, p Packet) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if to == nil {
		return syscall.EINVAL
	}
	return send(s.fd, data, to)
}

// Recv waits for one packet; a timeout is reported as EAGAIN so the caller can check
// whether it should stop.
func (s *Socket) Recv(buf []byte) (Packet, *Addr, error) {
	n, from, err := syscall.Recvfrom(s.fd, buf, 0)
	if err != nil {
		return Packet{}, nil, err
	}
	addr, _ := from.(*syscall.SockaddrInet4)
	var p Packet
	if err := json.Unmarshal(buf[:n], &p); err != nil {
		return Packet{}, addr, err
	}
	return p, addr, nil
}

// Query asks the LAN for a site's address and waits for one answer.
func Query(workspace, peer string, wait time.Duration) (string, error) {
	fd, err := socket()
	if err != nil {
		return "", err
	}
	defer syscall.Close(fd)
	if err := syscall.Bind(fd, &syscall.SockaddrInet4{}); err != nil {
		return "", err
	}
	tv := syscall.NsecToTimeval(int64(wait))
	if err := syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &tv); err != nil {
		return "", err
	}
	data, err := json.Marshal(Packet{Workspace: workspace, Peer: peer, Query: true, Sent: time.Now().UTC().Format(time.RFC3339)})
	if err != nil {
		return "", err
	}
	if err := send(fd, data, groupAddr()); err != nil {
		return "", err
	}
	buf := make([]byte, 4096)
	for {
		n, _, err := syscall.Recvfrom(fd, buf, 0)
		if err != nil {
			return "", err
		}
		var a Packet
		if json.Unmarshal(buf[:n], &a) != nil {
			continue
		}
		if a.Workspace != workspace || a.Query || a.Site != peer || a.Reach == "" {
			continue
		}
		return a.Reach, nil
	}
}
