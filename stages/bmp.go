package stages

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/bgpfix/bgpfix/afi"
	"github.com/bgpfix/bgpfix/bmp"
	"github.com/bgpfix/bgpfix/caps"
	"github.com/bgpfix/bgpfix/meta"
	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpipe/core"
	"github.com/bgpfix/bgpipe/pkg/util"
)

// Bmp speaks the BGP Monitoring Protocol (RFC 7854) over TCP: it either collects
// BMP feeds from routers, or exports the messages in the pipe as a BMP feed
type Bmp struct {
	*core.StageBase
	in *pipe.Input

	addr   string
	listen bool
	write  bool
	peers  []netip.Prefix // --peer allow-list
	tls    *tls.Config    // server TLS config, if any

	mu      sync.Mutex
	conns   map[net.Conn]chan []byte // open connections (the channel is nil in read mode)
	stopped bool
}

func NewBmp(parent *core.StageBase) core.Stage {
	s := &Bmp{StageBase: parent}

	o := &s.Options
	o.Descr = "collect or export a BMP feed (RFC 7854) over TCP"
	o.IsProducer = true
	o.FilterIn = true
	o.FilterOut = true
	o.Bidir = true
	o.Args = []string{"addr"}

	f := o.Flags
	f.Bool("listen", false, "listen on addr instead of connecting to it")
	f.Bool("write", false, "export: send UPDATEs as a BMP feed instead of reading one")
	f.StringSlice("peer", nil, "with --listen: accept only these IPs/prefixes (CSV, repeatable)")
	f.String("cert", "", "with --listen: TLS certificate path (enables TLS)")
	f.String("key", "", "with --listen: TLS private key path")
	f.Bool("tls", false, "connect over TLS")
	f.Bool("insecure", false, "do not validate TLS certificates")
	f.Duration("timeout", 15*time.Second, "TCP connect timeout (0 means off)")
	f.Duration("keepalive", 15*time.Second, "TCP keepalive period (-1 means off)")
	f.Bool("retry", false, "retry and reconnect on connection errors")
	f.Int("retry-max", 0, "maximum number of connection retries (0 means unlimited)")
	f.String("sys-name", "bgpipe", "sysName to send in export mode")
	f.Int("buffer", 100_000, "export: max. queued messages per client before it is dropped")

	return s
}

func (s *Bmp) Attach() error {
	k := s.K
	s.listen = k.Bool("listen")
	s.write = k.Bool("write")
	s.conns = make(map[net.Conn]chan []byte)

	s.addr = k.String("addr")
	if _, _, err := net.SplitHostPort(s.addr); err != nil {
		return fmt.Errorf("addr: %w", err)
	}
	if !s.listen {
		if len(k.Strings("peer")) > 0 || k.String("cert") != "" {
			return fmt.Errorf("--peer and --cert need --listen")
		}
	} else {
		if k.Bool("tls") {
			return fmt.Errorf("--tls is for connecting: use --cert and --key with --listen")
		}
		peers, err := parsePeers(k.Strings("peer"))
		if err != nil {
			return err
		}
		s.peers = peers
		if k.String("cert") != "" || k.String("key") != "" {
			cert, err := tls.LoadX509KeyPair(k.String("cert"), k.String("key"))
			if err != nil {
				return fmt.Errorf("--cert and --key: %w", err)
			}
			s.tls = &tls.Config{Certificates: []tls.Certificate{cert}}
		}
	}

	if s.write {
		s.Options.IsProducer = false
		s.Options.FilterOut = false
		cb := s.P.OnMsg(s.onMsg, s.Dir, msg.UPDATE) // NB: Route Monitoring carries UPDATEs only
		if s.IsLast {
			cb.Dir = meta.DIR_R // NB: as in write: the last stage takes what flows to it
		} else if s.IsFirst {
			cb.Dir = meta.DIR_L
		}
	} else {
		s.Options.FilterIn = false
		s.in = s.P.AddInput(s.Dir)
	}
	return nil
}

func (s *Bmp) Run() error {
	// client mode: one connection at a time
	for !s.listen {
		conn, err := util.DialRetry(s.StageBase, nil, "tcp", s.addr)
		if err != nil {
			return err
		}
		err = s.serve(conn)
		if !s.K.Bool("retry") || s.Ctx.Err() != nil || s.isStopped() {
			return err
		}
		s.Warn().Err(err).Msg("connection closed, reconnecting")
		select {
		case <-s.Ctx.Done():
			return nil
		case <-time.After(time.Second):
		}
	}

	// server mode: any number of connections
	var lc net.ListenConfig
	if v := s.K.Duration("keepalive"); v != 0 {
		lc.KeepAlive = v
	}
	l, err := lc.Listen(s.Ctx, "tcp", s.addr)
	if err != nil {
		return err
	}
	if s.tls != nil {
		l = tls.NewListener(l, s.tls)
	}
	go func() { <-s.Ctx.Done(); l.Close() }()
	s.Info().Msgf("listening on %s", l.Addr())

	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		conn, err := l.Accept()
		if err != nil {
			if s.Ctx.Err() != nil {
				return nil
			}
			return err
		}
		if !peerAllowed(s.peers, conn.RemoteAddr()) {
			s.Warn().Msgf("%s: not in --peer, rejected", conn.RemoteAddr())
			conn.Close()
			continue
		}
		wg.Go(func() {
			if err := s.serve(conn); err != nil {
				s.Warn().Err(err).Msgf("%s: connection closed", conn.RemoteAddr())
			}
		})
	}
}

// Stop lets the writers drain their queues, and unblocks the readers
func (s *Bmp) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
	for conn, ch := range s.conns {
		if ch != nil {
			close(ch)
		} else {
			conn.Close()
		}
		delete(s.conns, conn)
	}
	return nil
}

func (s *Bmp) isStopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped
}

// serve handles one BMP connection, until it ends
func (s *Bmp) serve(conn net.Conn) error {
	defer conn.Close()
	s.Info().Msgf("connected %s -> %s", conn.LocalAddr(), conn.RemoteAddr())

	var ch chan []byte
	if s.write {
		ch = make(chan []byte, s.K.Int("buffer"))
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return nil
	}
	s.conns[conn] = ch
	s.mu.Unlock()
	defer s.drop(conn)

	// collect: every connection has its own parser state
	if !s.write {
		rd := bmp.NewReader(s.P, s.in)
		if ap, err := netip.ParseAddrPort(conn.RemoteAddr().String()); err == nil {
			rd.Router = ap.Addr().Unmap().String() // NB: sysName replaces it, if sent
		}
		_, err := rd.ReadFrom(conn)
		s.Info().Msgf("%s: %d BMP messages, %d peers up, %d down", conn.RemoteAddr(),
			rd.Stats.Parsed, rd.Stats.PeerUp, rd.Stats.PeerDown)
		if s.Ctx.Err() != nil {
			return nil
		}
		return err
	}

	// export: Initiation, then a Peer Up ahead of the first message of each peer
	go func() { // NB: a BMP station sends nothing; this only detects that it left
		conn.Read(make([]byte, 1))
		s.drop(conn)
	}()
	bw := bufio.NewWriterSize(conn, 64*1024)
	bm := bmp.NewBmp()
	bm.SetInitiation(s.K.String("sys-name"), "bgpipe BMP export")
	if err := bm.Marshal(); err != nil {
		return err
	}
	bw.Write(bm.Bytes())

	type peerKey [25]byte // peer type, distinguisher, and address
	seen := make(map[peerKey]bool)
	for buf := range ch {
		key := peerKey(append(buf[6:7:7], buf[8:32]...))
		if !seen[key] {
			seen[key] = true
			bm.Reset()
			if _, err := bm.Peer.FromBytes(buf[bmp.HEADLEN:]); err != nil {
				return err
			}
			bm.SetPeerUp(s.peerUp(&bm.Peer))
			if err := bm.Marshal(); err != nil {
				return err
			}
			bw.Write(bm.Bytes())
		}
		_, err := bw.Write(buf)
		if err == nil && len(ch) == 0 {
			err = bw.Flush()
		}
		if err != nil {
			return err
		}
	}

	bm.Reset().SetTermination(0) // administratively closed
	if err := bm.Marshal(); err != nil {
		return err
	}
	bw.Write(bm.Bytes())
	return bw.Flush()
}

// drop forgets conn and makes its serve return
func (s *Bmp) drop(conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ch, ok := s.conns[conn]; ok {
		delete(s.conns, conn)
		if ch != nil {
			close(ch)
		} else {
			conn.Close()
		}
	}
}

// peerUp synthesizes the Peer Up body for peer: we did not see its real OPENs
func (s *Bmp) peerUp(peer *bmp.Peer) *bmp.PeerUp {
	mkopen := func(asn uint32, id netip.Addr) []byte {
		m := msg.NewMsg().Switch(msg.OPEN)
		o := &m.Open
		o.Caps.Init()
		o.HoldTime = msg.OPEN_HOLDTIME
		o.Identifier = id
		if peer.Is2ByteAS() {
			o.ASN = uint16(asn)
		} else {
			o.SetASN(asn)
		}
		if mp, ok := o.Caps.Use(caps.CAP_MP).(*caps.MP); ok {
			mp.Add(afi.AFI_IPV4, afi.SAFI_UNICAST)
			mp.Add(afi.AFI_IPV6, afi.SAFI_UNICAST)
			if pmp, ok := s.P.Caps.Get(caps.CAP_MP).(*caps.MP); ok {
				for _, as := range pmp.Sorted() {
					mp.Add(as.Afi(), as.Safi())
				}
			}
		}
		if m.Marshal(caps.Caps{}) != nil {
			return nil
		}
		var bb bytes.Buffer
		m.WriteTo(&bb)
		return bb.Bytes()
	}

	id := netip.AddrFrom4([4]byte{byte(peer.ID >> 24), byte(peer.ID >> 16), byte(peer.ID >> 8), byte(peer.ID)})
	local := netip.IPv4Unspecified()
	if peer.IsIPv6() {
		local = netip.IPv6Unspecified()
	}
	return &bmp.PeerUp{
		LocalAddr:  local,
		LocalPort:  179,
		RemotePort: 179,
		SentOpen:   mkopen(peer.AS, netip.AddrFrom4([4]byte{0, 0, 0, 1})), // NB: must differ from the peer's
		RcvdOpen:   mkopen(peer.AS, id),
	}
}

// onMsg queues m to all connected BMP stations
func (s *Bmp) onMsg(m *msg.Msg) bool {
	if m.Marshal(s.P.Caps) != nil {
		return true
	}
	bm := bmp.NewBmp()
	if bm.FromMsg(m, s.P.Caps) != nil {
		return true
	}

	// no peer in message tags? it is a live BGP session: take the neighbor that sent m
	peer := &bm.Peer
	if !peer.Address.IsValid() {
		side := "L_REMOTE" // NB: m flowing to R was sent by L
		if m.Dir == meta.DIR_L {
			side = "R_REMOTE"
		}
		if open := s.P.LineFor(m.Dir).Open.Load(); open != nil {
			peer.AS = open.GetASN()
			peer.Address = open.Identifier
			peer.ID = msbUint32(open.Identifier)
		}
		if ap, ok := s.P.KV.Load(side); ok {
			if ap, ok := ap.(netip.AddrPort); ok {
				peer.Address = ap.Addr().Unmap()
			}
		}
		if peer.Address.Is6() {
			peer.Flags |= bmp.PEER_FLAG_V6
		}
	}
	if peer.ID == 0 && peer.Address.Is4() {
		peer.ID = msbUint32(peer.Address) // NB: stations key peers on the BGP ID, too
	}
	if peer.Time.IsZero() {
		peer.Time = time.Now().UTC()
	}
	if bm.Marshal() != nil {
		return true
	}
	buf := append([]byte(nil), bm.Bytes()...)

	s.mu.Lock()
	defer s.mu.Unlock()
	for conn, ch := range s.conns {
		select {
		case ch <- buf:
		default: // NB: a slow station must not stall the pipe
			s.Warn().Msgf("%s: too slow, dropping the connection", conn.RemoteAddr())
			delete(s.conns, conn)
			close(ch)
		}
	}
	return true
}

func msbUint32(a netip.Addr) uint32 {
	if !a.Is4() {
		return 0
	}
	b := a.As4()
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}
