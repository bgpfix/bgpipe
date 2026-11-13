package stages

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
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
	conns   map[net.Conn]chan bmpOut // open connections (the channel is nil in read mode)
	nconns  atomic.Int32             // len(conns), read without the lock
	stopped bool
	pool    sync.Pool // of *bmp.Bmp
}

// bmpOut is a marshaled Route Monitoring message for the stations
type bmpOut struct {
	buf []byte   // the BMP message, shared by all stations
	dir meta.Dir // if not 0: the peer is the live BGP neighbor that sent the message in dir
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
	s.conns = make(map[net.Conn]chan bmpOut)
	s.pool.New = func() any { return bmp.NewBmp() }

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
	// client mode: one connection at a time, reconnect on --retry
	for !s.listen {
		conn, err := util.DialRetry(s.StageBase, nil, "tcp", s.addr)
		if err != nil {
			return err
		}
		err = s.serve(conn)
		if !s.K.Bool("retry") || s.Ctx.Err() != nil || s.isStopped() {
			return err
		}
		s.Warn().Err(err).Msg("connection ended, reconnecting")
		select { // NB: do not hammer a station that keeps closing on us
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
	if len(s.peers) == 0 {
		s.Warn().Msg("no --peer given: any host can connect")
	}

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
	for conn := range s.conns {
		s.dropLocked(conn, 5*time.Second) // NB: time to drain, then Termination
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

	var ch chan bmpOut
	if s.write {
		ch = make(chan bmpOut, s.K.Int("buffer"))
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return nil
	}
	s.conns[conn] = ch
	s.nconns.Store(int32(len(s.conns)))
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
	for out := range ch {
		buf := out.buf
		var key peerKey
		key[0] = buf[bmp.HEADLEN]                        // peer type
		copy(key[1:], buf[bmp.HEADLEN+2:bmp.HEADLEN+26]) // NB: skip the flags
		if !seen[key] {
			seen[key] = true
			bm.Reset()
			if _, err := bm.Peer.FromBytes(buf[bmp.HEADLEN:]); err != nil {
				return err
			}
			bm.SetPeerUp(s.peerUp(&bm.Peer, out.dir))
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
	s.dropLocked(conn, 0)
}

// dropLocked is drop with s.mu held. A writer gets up to wait to finish writing.
func (s *Bmp) dropLocked(conn net.Conn, wait time.Duration) {
	ch, ok := s.conns[conn]
	if !ok {
		return
	}
	delete(s.conns, conn)
	s.nconns.Store(int32(len(s.conns)))
	if ch == nil {
		conn.Close() // reader: unblock it
	} else {
		close(ch)
		conn.SetWriteDeadline(time.Now().Add(wait)) // NB: a stuck station must not block us
	}
}

// peerUp returns the Peer Up body for peer. If dir is not 0, the peer is the live
// BGP neighbor that sends in dir, so use the real OPENs; else synthesize them.
func (s *Bmp) peerUp(peer *bmp.Peer, dir meta.Dir) *bmp.PeerUp {
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
	pu := &bmp.PeerUp{
		LocalAddr:  local,
		LocalPort:  179,
		RemotePort: 179,
	}

	// a live session? NB: LineFor(dir).Open is the OPEN that travels in dir, ie. the peer's
	if dir != 0 {
		pu.RcvdOpen = openBytes(s.P.LineFor(dir).Open.Load())
		pu.SentOpen = openBytes(s.P.LineFor(dir.Flip()).Open.Load())
		side := "L_" // NB: the neighbor sending to R is on the L side
		if dir == meta.DIR_L {
			side = "R_"
		}
		if v, ok := s.P.KV.Load(side + "LOCAL"); ok {
			if ap, ok := v.(netip.AddrPort); ok {
				pu.LocalAddr, pu.LocalPort = ap.Addr().Unmap(), ap.Port()
			}
		}
		if v, ok := s.P.KV.Load(side + "REMOTE"); ok {
			if ap, ok := v.(netip.AddrPort); ok {
				pu.RemotePort = ap.Port()
			}
		}
	}
	if pu.SentOpen == nil || pu.RcvdOpen == nil {
		pu.SentOpen = mkopen(peer.AS, netip.AddrFrom4([4]byte{0, 0, 0, 1})) // NB: must differ from the peer's
		pu.RcvdOpen = mkopen(peer.AS, id)
	}
	return pu
}

// openBytes returns the wire format of o, or nil
func openBytes(o *msg.Open) []byte {
	if o == nil || o.Msg == nil {
		return nil
	}
	var bb bytes.Buffer
	if _, err := o.Msg.WriteTo(&bb); err != nil {
		return nil
	}
	return bb.Bytes()
}

// onMsg queues m to all connected BMP stations
func (s *Bmp) onMsg(m *msg.Msg) bool {
	if s.nconns.Load() == 0 {
		return true // NB: nobody to send to, skip the work
	} else if m.Marshal(s.P.Caps) != nil {
		return true
	}
	bm := s.pool.Get().(*bmp.Bmp)
	defer s.pool.Put(bm)
	if bm.Reset().FromMsg(m, s.P.Caps) != nil {
		return true
	}

	// no peer in message tags? it is a live BGP session: take the neighbor that sent m
	peer := &bm.Peer
	var dir meta.Dir
	if !peer.Address.IsValid() {
		dir = m.Dir
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
	out := bmpOut{append([]byte(nil), bm.Bytes()...), dir}

	s.mu.Lock()
	defer s.mu.Unlock()
	for conn, ch := range s.conns {
		select {
		case ch <- out:
		default: // NB: a slow station must not stall the pipe
			s.Warn().Msgf("%s: too slow, dropping the connection", conn.RemoteAddr())
			s.dropLocked(conn, 0)
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
