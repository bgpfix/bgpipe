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

	"github.com/VictoriaMetrics/metrics"
	"github.com/bgpfix/bgpfix/afi"
	"github.com/bgpfix/bgpfix/bmp"
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
	conns   map[net.Conn]*station // open connections
	nconns  atomic.Int32          // len(conns), read without the lock
	stopped bool
	ln      net.Listener              // with --listen
	first   *station                  // exporter: the connection made in Prepare
	gauges  map[string]*metrics.Gauge // statistics
	pool    sync.Pool                 // of *bmp.Bmp
}

// station is an exporter connection (empty in read mode)
type station struct {
	conn net.Conn
	ch   chan []byte // queued BMP messages
	w    *bmp.Writer // adds Initiation and Peer Up
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
	o.StopTimeout = 6 * time.Second // NB: exporters get 5s to drain on Stop
	o.Events = map[string]string{
		"peer-up":   "station: a monitored peer is up (router, peer IP, peer AS)",
		"peer-down": "station: a monitored peer is down (router, peer IP, peer AS)",
	}

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
	s.conns = make(map[net.Conn]*station)
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

// Prepare connects an exporter before the pipeline starts, so it gets every UPDATE
// (eg. in read file.mrt -- bmp --write)
func (s *Bmp) Prepare() error {
	if s.listen || !s.write {
		return nil
	}
	conn, err := util.DialRetry(s.StageBase, nil, "tcp", s.addr)
	if err != nil {
		return err
	}
	s.first, _ = s.add(conn)
	return nil
}

func (s *Bmp) Run() error {
	// client mode: one connection at a time, reconnect on --retry
	for !s.listen {
		st := s.first // NB: may already be stopped, still drain it
		s.first = nil
		var conn net.Conn
		if st != nil {
			conn = st.conn
		} else if c, err := util.DialRetry(s.StageBase, nil, "tcp", s.addr); err != nil {
			return err
		} else {
			conn = c
		}
		err := s.serve(conn, st)
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
	s.mu.Lock()
	s.ln = l
	stopped := s.stopped
	s.mu.Unlock()
	if stopped {
		l.Close()
	}
	s.Info().Msgf("listening on %s", l.Addr())
	if len(s.peers) == 0 {
		s.Warn().Msg("no --peer given: any host can connect")
	}

	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		conn, err := l.Accept()
		if err != nil {
			if s.Ctx.Err() != nil || s.isStopped() {
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
			if err := s.serve(conn, nil); err != nil {
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
	if s.ln != nil {
		s.ln.Close() // NB: --stop does not cancel s.Ctx
	}
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

// serve handles one BMP connection, until it ends; st is its station, if registered already
func (s *Bmp) serve(conn net.Conn, st *station) error {
	defer conn.Close()
	s.Info().Msgf("connected %s -> %s", conn.LocalAddr(), conn.RemoteAddr())

	if st == nil {
		var ok bool
		if st, ok = s.add(conn); !ok {
			return nil // stopped
		}
	}
	defer s.drop(conn)

	// collect: every connection has its own parser state
	if !s.write {
		rd := bmp.NewReader(s.P, s.in)
		ip := conn.RemoteAddr().String()
		if ap, err := netip.ParseAddrPort(ip); err == nil {
			ip = ap.Addr().Unmap().String()
		}
		rd.Router = ip // NB: sysName replaces it, if sent
		rd.OnBmp = func(bm *bmp.Bmp) { s.onBmp(bm, rd.Router, ip) }
		_, err := rd.ReadFrom(conn)
		s.Info().Msgf("%s: %d BMP messages, %d peers up, %d down", conn.RemoteAddr(),
			rd.Stats.Parsed, rd.Stats.PeerUp, rd.Stats.PeerDown)
		if s.Ctx.Err() != nil || s.isStopped() {
			return nil // NB: Stop closed conn
		}
		return err
	}

	// export: Initiation, then a Peer Up ahead of the first message of each peer
	go func() { // NB: a BMP station sends nothing; this only detects that it left
		conn.Read(make([]byte, 1))
		s.drop(conn)
	}()
	bw := bufio.NewWriterSize(conn, 64*1024)
	s.mu.Lock()
	bw.Write(st.w.Start(nil)) // NB: empty if onMsg queued it already
	s.mu.Unlock()
	if err := bw.Flush(); err != nil {
		return err
	}
	for buf := range st.ch {
		_, err := bw.Write(buf)
		if err == nil && len(st.ch) == 0 {
			err = bw.Flush()
		}
		if err != nil {
			return err
		}
	}

	bm := bmp.NewBmp()
	bm.SetTermination(0) // administratively closed
	if err := bm.Marshal(); err != nil {
		return err
	}
	bw.Write(bm.Bytes())
	return bw.Flush()
}

// add registers conn, unless already registered; returns false if stopped
func (s *Bmp) add(conn net.Conn) (st *station, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return nil, false
	} else if st, ok = s.conns[conn]; ok {
		return st, true
	}
	st = &station{conn: conn}
	if s.write {
		st.ch = make(chan []byte, s.K.Int("buffer"))
		st.w = &bmp.Writer{SysName: s.K.String("sys-name"), SysDescr: "bgpipe BMP export", PeerUp: s.peerUp}
	}
	s.conns[conn] = st
	s.nconns.Store(int32(len(s.conns)))
	return st, true
}

// drop forgets conn and makes its serve return
func (s *Bmp) drop(conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropLocked(conn, 0)
}

// dropLocked is drop with s.mu held. A writer gets up to wait to finish writing.
func (s *Bmp) dropLocked(conn net.Conn, wait time.Duration) {
	st, ok := s.conns[conn]
	if !ok {
		return
	}
	delete(s.conns, conn)
	s.nconns.Store(int32(len(s.conns)))
	if st.ch == nil {
		conn.Close() // reader: unblock it
	} else {
		close(st.ch)
		conn.SetWriteDeadline(time.Now().Add(wait)) // NB: a stuck station must not block us
	}
}

// peerUp returns the Peer Up for b, made from m: if its peer is the live BGP
// neighbor, use the real OPENs; else return nil, to have it synthesized.
func (s *Bmp) peerUp(b *bmp.Bmp, m *msg.Msg) *bmp.PeerUp {
	if _, ok := pipe.GetTags(m)["PEER_IP"]; ok {
		return nil // not a live session
	}

	// NB: LineFor(dir).Open is the OPEN that travels in dir, ie. the peer's
	dir := m.Dir
	pu := &bmp.PeerUp{
		RcvdOpen: openBytes(s.P.LineFor(dir).Open.Load()),
		SentOpen: openBytes(s.P.LineFor(dir.Flip()).Open.Load()),
	}
	if pu.RcvdOpen == nil || pu.SentOpen == nil {
		return nil
	}
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
	// NB: one buffer for all stations, unless one needs Initiation or Peer Up first
	var shared []byte
	s.mu.Lock()
	defer s.mu.Unlock()
	for conn, st := range s.conns {
		buf := st.w.Prepend(nil, bm, m, s.P.Caps)
		if len(buf) > 0 {
			buf = append(buf, bm.Bytes()...)
		} else if shared != nil {
			buf = shared
		} else {
			shared = append([]byte(nil), bm.Bytes()...)
			buf = shared
		}
		select {
		case st.ch <- buf:
		default: // NB: a slow station must not stall the pipe
			s.Warn().Msgf("%s: too slow, dropping the connection", conn.RemoteAddr())
			s.dropLocked(conn, 0)
		}
	}
	return true
}

// onBmp handles BMP messages other than Route Monitoring from router (at ip)
func (s *Bmp) onBmp(bm *bmp.Bmp, router, ip string) {
	switch bm.Type {
	case bmp.MSG_PEER_UP:
		s.Event("peer-up", router, bm.Peer.Address.String(), bm.Peer.AS)
	case bmp.MSG_PEER_DOWN:
		s.Event("peer-down", router, bm.Peer.Address.String(), bm.Peer.AS)
	case bmp.MSG_STATISTICS_REPORT:
		// NB: labels from the connection, not the router-supplied sysName
		name := fmt.Sprintf(`%sstats{router=%q,peer=%q,type="%%d"`, s.MetricPrefix(), ip, bm.Peer.Address.String())
		bm.Stats(func(typ uint16, as afi.AS, val uint64) {
			n := fmt.Sprintf(name, typ)
			if as != 0 {
				n += fmt.Sprintf(`,af="%s/%s"`, as.Afi(), as.Safi())
			}
			if g := s.gauge(n + "}"); g != nil {
				g.Set(float64(val))
			}
		})
	}
}

// MAX_GAUGES bounds the statistics series, as routers choose the labels
const MAX_GAUGES = 10000

// gauge returns the statistics gauge named n, or nil if there are too many already
func (s *Bmp) gauge(n string) *metrics.Gauge {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.gauges[n]
	if g == nil && len(s.gauges) < MAX_GAUGES {
		if s.gauges == nil {
			s.gauges = make(map[string]*metrics.Gauge)
		}
		g = metrics.GetOrCreateGauge(n, nil)
		s.gauges[n] = g
	}
	return g
}

func msbUint32(a netip.Addr) uint32 {
	if !a.Is4() {
		return 0
	}
	b := a.As4()
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}
