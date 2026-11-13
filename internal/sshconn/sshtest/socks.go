package sshtest

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"
)

// SOCKS5Options configure a SOCKS5 proxy.
type SOCKS5Options struct {
	// Username and Password, when Username is set, are the only sign-in
	// the proxy accepts (RFC 1929); otherwise it needs none.
	Username, Password string
	// Hosts maps host names only the proxy can resolve, such as
	// "dev-box.internal", to IP addresses. Other names dial as given.
	Hosts map[string]string
}

// SOCKS5 is a running SOCKS5 proxy (RFC 1928) on 127.0.0.1 for tests. It
// serves CONNECT to IPv4, IPv6, and domain-name addresses.
type SOCKS5 struct {
	Host string // "127.0.0.1"
	Port int

	opts     SOCKS5Options
	listener net.Listener

	mu      sync.Mutex
	conns   map[net.Conn]bool
	targets []string
	wg      sync.WaitGroup
}

// StartSOCKS5 starts a proxy, stopped when the test ends.
func StartSOCKS5(tb testing.TB, opts SOCKS5Options) *SOCKS5 {
	tb.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		tb.Fatal(err)
	}
	p := &SOCKS5{Host: "127.0.0.1", Port: l.Addr().(*net.TCPAddr).Port, opts: opts, listener: l,
		conns: map[net.Conn]bool{}}
	p.wg.Add(1)
	go p.serve()
	tb.Cleanup(p.Close)
	return p
}

// Targets returns the host:port of every CONNECT request, as clients sent
// them (names unresolved).
func (p *SOCKS5) Targets() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.targets...)
}

// Close stops the proxy and closes its connections.
func (p *SOCKS5) Close() {
	p.listener.Close()
	p.mu.Lock()
	for c := range p.conns {
		c.Close()
	}
	p.mu.Unlock()
	p.wg.Wait()
}

func (p *SOCKS5) track(c net.Conn, on bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if on {
		p.conns[c] = true
	} else {
		delete(p.conns, c)
	}
}

func (p *SOCKS5) serve() {
	defer p.wg.Done()
	for {
		c, err := p.listener.Accept()
		if err != nil {
			return
		}
		p.track(c, true)
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			defer func() {
				c.Close()
				p.track(c, false)
			}()
			p.handle(c)
		}()
	}
}

// Replies of RFC 1928.
const (
	repSucceeded        = 0x00
	repRefused          = 0x05
	repCmdNotSupported  = 0x07
	repAddrNotSupported = 0x08
)

func (p *SOCKS5) handle(c net.Conn) {
	if err := p.signIn(c); err != nil {
		return
	}
	// VER CMD RSV ATYP
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(c, hdr); err != nil || hdr[0] != 5 {
		return
	}
	host, err := readAddr(c, hdr[3])
	if err != nil {
		reply(c, repAddrNotSupported)
		return
	}
	var port [2]byte
	if _, err := io.ReadFull(c, port[:]); err != nil {
		return
	}
	if hdr[1] != 1 { // CONNECT
		reply(c, repCmdNotSupported)
		return
	}
	portStr := strconv.Itoa(int(binary.BigEndian.Uint16(port[:])))
	p.mu.Lock()
	p.targets = append(p.targets, net.JoinHostPort(host, portStr))
	p.mu.Unlock()
	if ip, ok := p.opts.Hosts[host]; ok {
		host = ip
	}
	up, err := net.DialTimeout("tcp", net.JoinHostPort(host, portStr), 5*time.Second)
	if err != nil {
		reply(c, repRefused)
		return
	}
	p.track(up, true)
	defer func() {
		up.Close()
		p.track(up, false)
	}()
	if reply(c, repSucceeded) != nil {
		return
	}
	done := make(chan struct{}, 2)
	pipe := func(dst, src net.Conn) {
		io.Copy(dst, src)
		// Unblock the other direction.
		dst.Close()
		src.Close()
		done <- struct{}{}
	}
	go pipe(up, c)
	go pipe(c, up)
	<-done
	<-done
}

// signIn negotiates the method, and checks the username and password when
// the proxy has one.
func (p *SOCKS5) signIn(c net.Conn) error {
	hdr := make([]byte, 2) // VER NMETHODS
	if _, err := io.ReadFull(c, hdr); err != nil || hdr[0] != 5 {
		return errors.New("bad greeting")
	}
	methods := make([]byte, hdr[1])
	if _, err := io.ReadFull(c, methods); err != nil {
		return err
	}
	want := byte(0x00) // no authentication
	if p.opts.Username != "" {
		want = 0x02 // username/password
	}
	offered := false
	for _, m := range methods {
		offered = offered || m == want
	}
	if !offered {
		c.Write([]byte{5, 0xff}) // no acceptable methods
		return errors.New("no acceptable method")
	}
	if _, err := c.Write([]byte{5, want}); err != nil {
		return err
	}
	if want == 0x00 {
		return nil
	}
	// VER ULEN UNAME PLEN PASSWD
	ver := make([]byte, 2)
	if _, err := io.ReadFull(c, ver); err != nil || ver[0] != 1 {
		return errors.New("bad sign-in")
	}
	user := make([]byte, ver[1])
	if _, err := io.ReadFull(c, user); err != nil {
		return err
	}
	var plen [1]byte
	if _, err := io.ReadFull(c, plen[:]); err != nil {
		return err
	}
	pass := make([]byte, plen[0])
	if _, err := io.ReadFull(c, pass); err != nil {
		return err
	}
	if string(user) != p.opts.Username || string(pass) != p.opts.Password {
		c.Write([]byte{1, 1})
		return errors.New("wrong credentials")
	}
	_, err := c.Write([]byte{1, 0})
	return err
}

func readAddr(r io.Reader, atyp byte) (string, error) {
	switch atyp {
	case 1, 4: // IPv4, IPv6
		ip := make([]byte, map[byte]int{1: 4, 4: 16}[atyp])
		if _, err := io.ReadFull(r, ip); err != nil {
			return "", err
		}
		return net.IP(ip).String(), nil
	case 3: // domain name
		var n [1]byte
		if _, err := io.ReadFull(r, n[:]); err != nil {
			return "", err
		}
		name := make([]byte, n[0])
		if _, err := io.ReadFull(r, name); err != nil {
			return "", err
		}
		return string(name), nil
	}
	return "", errors.New("unknown address type")
}

// reply sends a reply with the bound address 0.0.0.0:0.
func reply(c net.Conn, rep byte) error {
	_, err := c.Write([]byte{5, rep, 0, 1, 0, 0, 0, 0, 0, 0})
	return err
}
