package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"net"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpfix/speaker"
	"github.com/bgpfix/bgpfix/util"
	"github.com/bgpfix/bgpipe/pkg/bgpipe"
	"github.com/rs/zerolog/log"
)

var (
	opt_active     = flag.Bool("active", false, "send the OPEN message first")
	opt_asn        = flag.Int("asn", -1, "local ASN, -1 means use remote ASN")
	opt_id         = flag.String("id", "", "local router ID, empty means use remote-1")
	opt_hold       = flag.Int("hold", msg.OPEN_HOLDTIME, "local hold time; 0=disable")
	opt_stdin_wait = flag.Bool("stdin.wait", true, "wait with reading stdin until session is established")
	opt_stdin_dir  = flag.String("stdin.dir", "TX", "set message direction for stdin; 0=disable")
	opt_stdin_time = flag.Bool("stdin.time", false, "use message time from stdin")
	opt_stdin_seq  = flag.Bool("stdin.seq", false, "use sequence numbers from stdin")
)

func main() {

	b := bgpipe.NewBgpipe(context.Background())
	b.Run()

	return

	flag.Parse()

	// has arguments?
	args := flag.Args()
	if len(args) < 1 {
		log.Fatal().Msg("usage: bgp2json <target:port> [<proxy-to:port>]")
	}

	// create a BGP pipe
	p := pipe.NewPipe(context.Background())
	po := &p.Options
	po.Logger = log.Logger
	po.Tstamp = true

	// dial the first target (LHS)
	lhs := args[0]
	if strings.IndexByte(lhs, ':') < 0 {
		lhs += ":179"
	}
	log.Info().Msgf("LHS: dialing %s", lhs)
	conn1, err := net.Dial("tcp", lhs)
	if err != nil {
		log.Fatal().Err(err).Msg("could not dial target")
	}
	log.Info().Msgf("LHS: connected")

	// dial the proxy-to target? (RHS)
	var conn2 net.Conn
	var rhs string
	if len(args) > 1 {
		rhs = args[1]
		if strings.IndexByte(rhs, ':') < 0 {
			rhs += ":179"
		}

		log.Info().Msgf("RHS: dialing %s", rhs)
		conn2, err = net.Dial("tcp", rhs)
		if err != nil {
			log.Fatal().Err(err).Msg("could not dial target")
		}

		log.Info().Msgf("RHS: connected")

		// po.OnTxRx(func(p *pipe.Pipe, m *msg.Msg) {
		// 	asn := *opt_asn
		// 	if m.Dir == msg.TX {
		// 		asn = 65000
		// 	}

		// 	o := &m.Open
		// 	log.Info().Msgf("%s: changing AS%d to AS%d", m.Dir, o.ASN, asn)

		// 	o.SetASN(asn)
		// 	// o.Caps.Drop(msg.CAP_AS4)
		// 	m.Dirty = true
		// }, msg.OPEN)

		// seen_eor := 0
		// po.OnTxRx(func(p *pipe.Pipe, m *msg.Msg) {
		// 	u := &m.Update

		// 	switch mp := u.AttrReach().(type) {
		// 	case nil:
		// 		if len(u.Reach) == 0 && len(u.Unreach) == 0 {
		// 			p.Info().Msg("seen EOR")
		// 			seen_eor++
		// 		}
		// 	case *msg.AttrMPPrefixes:
		// 		if len(mp.Prefixes) == 0 {
		// 			p.Info().Msg("seen EOR")
		// 			seen_eor++
		// 		}
		// 	case *msg.AttrMPFlow:
		// 		seen_eor = 2

		// 		mp.NextHop = netip.Addr{}
		// 		for _, rule := range mp.Rules {
		// 			delete(rule, msg.FLOW_PORT_DST)
		// 			delete(rule, msg.FLOW_PROTO)
		// 			// rule.AddDst(netip.MustParsePrefix("192.168.16.0/24"))
		// 		}

		// 		if xc, ok := u.Attrs.Use(msg.ATTR_EXT_COMMUNITY, p.Caps).(*msg.AttrExtCom); ok {
		// 			xc.SetFlowRateBytes(0)
		// 		}
		// 	}

		// 	// drop historical data
		// 	if seen_eor < 2 {
		// 		m.Action |= pipe.ACTION_DROP
		// 		return
		// 	}

		// 	m.Dirty = true // re-marshal
		// }, msg.UPDATE)

	} else {
		spk := speaker.NewSpeaker(context.Background())
		so := &spk.Options
		so.Logger = log.Logger
		so.Passive = !*opt_active
		so.LocalHoldTime = *opt_hold
		so.LocalASN = *opt_asn
		if len(*opt_id) > 0 {
			so.LocalId = netip.MustParseAddr(*opt_id)
		} else if so.Passive {
			so.LocalId = netip.Addr{}
		} else {
			so.LocalId = netip.MustParseAddr("0.0.0.1")
		}

		err := spk.Attach(p)
		if err != nil {
			log.Fatal().Err(err).Msg("could not attach local speaker")
		}

		log.Info().Interface("cfg", so).Msgf("RHS: attached local speaker")
	}

	// react to events
	po.OnEvent(event)

	// read stdin?
	if !*opt_stdin_wait {
		go reader(p)
	}

	// copy all TCP<->BGP and print
	po.OnTxRxLast(print)
	lhsb, rhsb, err := util.CopyThrough(p, conn1, conn2)
	log.Info().
		Err(err).
		Ints("LHS TX/RX bytes", lhsb).
		Ints("RHS TX/RX bytes", rhsb).
		Interface("proc RX stats", p.Rx.Stats()).
		Interface("proc TX stats", p.Tx.Stats()).
		Msgf("done (LHS=%s / RHS=%s)", lhs, rhs)
}

func event(p *pipe.Pipe, ev *pipe.Event) bool {
	switch ev.Type {
	case pipe.EVENT_OPEN:
		log.Info().Interface("capabilities", &p.Caps).Msg("OPEN sent and received")

		// read stdin?
		if *opt_stdin_wait {
			go reader(p)
		}

	case pipe.EVENT_PARSE:
		err, _ := ev.Value.(error)
		log.Info().Err(err).Stringer("msg", ev.Msg).Msg("message parse error")
		// default:
		// log.Info().Interface("ev", ev).Msg("EVENT")
	}
	return true
}

var printbuf []byte

func print(p *pipe.Pipe, m *msg.Msg) {
	printbuf = m.ToJSON(printbuf[:0])
	printbuf = append(printbuf, '\n')
	os.Stdout.Write(printbuf)
}

func reader(p *pipe.Pipe) {
	open_prefix := []byte(`{"bgp":`)
	keepalive := []byte(`null`)
	err_format := errors.New("invalid format")

	var dir msg.Dir
	switch v := strings.ToUpper(*opt_stdin_dir); v {
	case "TX":
		dir = msg.TX
	case "RX":
		dir = msg.RX
	}

	scanner := bufio.NewScanner(os.Stdin)
	for {
		// get new msg
		m := p.Get()

		// read line, trim it
		if !scanner.Scan() {
			break
		}
		buf := bytes.TrimSpace(scanner.Bytes())

		// detect the format
		var err error
		switch {
		case len(buf) == 0 || buf[0] == '#':
			continue

		case buf[0] == '[': // full message
			err = m.FromJSON(buf)

		case buf[0] == '{': // infer upper type
			if bytes.HasPrefix(buf, open_prefix) {
				m.SetUp(msg.OPEN)
				err = m.Open.FromJSON(buf)
			} else {
				m.SetUp(msg.UPDATE)
				err = m.Update.FromJSON(buf)
			}

		case bytes.Equal(buf, keepalive):
			m.SetUp(msg.KEEPALIVE)

		default:
			err = err_format
		}

		if err != nil {
			log.Error().Bytes("input", buf).Err(err).Msg("stdin parse error")
			continue
		}

		// overwrite?
		if dir != 0 {
			m.Dir = dir
		}
		if !*opt_stdin_seq {
			m.Seq = 0
		}
		if !*opt_stdin_time {
			m.Time = time.Time{}
		}

		// sail
		switch m.Dir {
		case msg.RX:
			p.Rx.In <- m
		case msg.TX:
			p.Tx.In <- m
		default:
			log.Error().Bytes("input", buf).Uint8("dir", uint8(m.Dir)).Msg("invalid message direction")
		}
	}
}
