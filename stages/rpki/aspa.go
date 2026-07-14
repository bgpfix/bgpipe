package rpki

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/VictoriaMetrics/metrics"
	"github.com/bgpfix/bgpfix/attrs"
	"github.com/bgpfix/bgpfix/caps"
	"github.com/bgpfix/bgpfix/meta"
	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpfix/rpki"
	"github.com/bgpfix/bgpipe/core"
	"github.com/go-chi/chi/v5"
)

// Aspa validates UPDATE AS_PATHs using RPKI ASPA
// (draft-ietf-sidrops-aspa-verification)
type Aspa struct {
	*core.StageBase
	cache *rpki.Cache // shared RPKI cache, maintained by the core

	act       int    // action for INVALID paths
	tag       bool   // add aspa/ tags
	event     string // emit event on INVALID
	role      byte   // parsed --role (valid iff !role_auto)
	role_auto bool   // --role auto: detect from the OPEN message
	peer_tag  string // --peer-tag flag value
	first_hop bool   // run the first-hop check (path[0] == neighbor AS)?

	// resolved peer state, set once per direction on first UPDATE
	peer [2]aspaPeer

	cnt_msg   *metrics.Counter
	cnt_valid *metrics.Counter
	cnt_unk   *metrics.Counter
	cnt_inv   *metrics.Counter
}

// aspaPeer holds per-direction peer info resolved from --role and the OPEN message
type aspaPeer struct {
	once sync.Once
	ok   bool   // role resolved successfully?
	down bool   // peer is our provider (downstream path)?
	rs   bool   // peer is a route server?
	asn  uint32 // peer ASN from its OPEN (0 = unknown, first-hop check disabled)
}

func NewAspa(parent *core.StageBase) core.Stage {
	s := &Aspa{StageBase: parent}

	o := &s.Options
	o.Descr = "validate AS paths using RPKI ASPA"
	o.FilterIn = true
	o.Bidir = true

	f := o.Flags
	f.String("invalid", "withdraw", "action for INVALID paths: withdraw|drop|keep")
	f.Bool("tag", true, "add aspa/ validation status to message tags")
	f.String("event", "", "emit event on INVALID paths")
	f.String("role", "auto", "peer BGP role: auto|provider|customer|peer|rs|rs-client")
	f.String("peer-tag", "", "read peer ASN from given message tag (eg. PEER_AS) instead of OPEN")
	f.Bool("first-hop", true, "check path[0] == neighbor AS; disable for collector feeds (RFC 7947)")
	f.Bool("no-wait", false, "start before the RPKI cache is ready")

	return s
}

func (s *Aspa) Attach() error {
	k := s.K

	switch strings.ToLower(k.String("invalid")) {
	case "withdraw":
		s.act = act_withdraw
	case "drop":
		s.act = act_drop
	case "keep":
		s.act = act_keep
	default:
		return fmt.Errorf("--invalid must be withdraw, drop or keep")
	}

	s.tag = k.Bool("tag")
	s.event = k.String("event")
	s.first_hop = k.Bool("first-hop")

	if role := k.String("role"); strings.EqualFold(role, "auto") {
		s.role_auto = true
	} else if r, ok := parseRoleName(role); ok {
		s.role = r
	} else {
		return fmt.Errorf("--role must be auto, provider, customer, peer, rs or rs-client")
	}

	// NB: a single --role cannot describe two different peers
	if s.IsBidir && !s.role_auto {
		return fmt.Errorf("explicit --role does not work in -LR mode")
	}

	// NB: --peer-tag targets multi-peer feeds (eg. ris-live), where there is
	// no OPEN message to auto-detect the role from either
	s.peer_tag = k.String("peer-tag")
	if s.peer_tag != "" && s.role_auto {
		return fmt.Errorf("--peer-tag requires an explicit --role")
	}

	// use the shared RPKI cache, maintained by the bgpipe core
	s.cache = s.B.UseRpki()

	// prometheus metrics
	prefix := s.MetricPrefix()
	s.cnt_msg = metrics.GetOrCreateCounter(prefix + "messages_total")
	s.cnt_valid = metrics.GetOrCreateCounter(prefix + "valid_total")
	s.cnt_unk = metrics.GetOrCreateCounter(prefix + "unknown_total")
	s.cnt_inv = metrics.GetOrCreateCounter(prefix + "invalid_total")

	// subscribe to UPDATE messages in given direction
	s.P.OnMsg(s.validateMsg, s.Dir, msg.UPDATE)

	return nil
}

func (s *Aspa) Prepare() error {
	if !s.K.Bool("no-wait") {
		return s.cache.WaitReady(s.Ctx)
	}
	return nil
}

// parseRoleName converts a --role flag string to a caps.ROLE_* constant.
func parseRoleName(name string) (byte, bool) {
	switch strings.ToLower(name) {
	case "provider":
		return caps.ROLE_PROVIDER, true
	case "rs":
		return caps.ROLE_RS, true
	case "rs-client":
		return caps.ROLE_RS_CLIENT, true
	case "customer":
		return caps.ROLE_CUSTOMER, true
	case "peer":
		return caps.ROLE_PEER, true
	default:
		return 0, false
	}
}

// peerASN returns the peer's ASN from its OPEN message, or 0 if unavailable.
func peerASN(p *pipe.Pipe, d meta.Dir) uint32 {
	om := p.LineFor(d).Open.Load()
	if om == nil {
		return 0
	}
	return uint32(om.GetASN())
}

// peerRole reads the BGP Role capability from the peer's OPEN message.
func peerRole(p *pipe.Pipe, d meta.Dir) (byte, bool) {
	om := p.LineFor(d).Open.Load()
	if om == nil {
		return 0, false
	}
	c, ok := om.Caps.Get(caps.CAP_ROLE).(*caps.Role)
	if !ok || c == nil {
		return 0, false
	}
	return c.Role, true
}

// resolvePeer resolves the peer state for direction d, once.
//
// NB: BGP guarantees OPEN is exchanged before any UPDATE, so both the role
// and the peer ASN are stable by the time the first UPDATE arrives. If --role
// is auto and the peer didn't send the BGP Role capability, ASPA validation
// is permanently skipped for this direction.
// NB: in -LR mode only --role auto is allowed (checked in Attach), as a
// single role cannot describe two different peers.
func (s *Aspa) resolvePeer(d meta.Dir) *aspaPeer {
	p := &s.peer[d&1] // direction index: 0=R, 1=L
	p.once.Do(func() {
		role := s.role
		if s.role_auto {
			var ok bool
			role, ok = peerRole(s.P, d)
			if !ok {
				s.Warn().Stringer("dir", d).Msg("ASPA: peer did not send the BGP Role capability, skipping (use --role to override)")
				return
			}
			s.Info().Int("role", int(role)).Stringer("dir", d).Msg("ASPA: peer role detected")
		} else {
			s.Info().Int("role", int(role)).Stringer("dir", d).Msg("ASPA: peer role set via --role")
		}

		p.ok = true

		// NB: per draft-ietf-sidrops-aspa-verification-24 section 5.5, the downstream
		// procedure applies only when the route is received from a Provider;
		// RS-client receiving from RS uses upstream per section 5.4.
		p.down = role == caps.ROLE_PROVIDER
		p.rs = role == caps.ROLE_RS

		// NB: with --peer-tag, the peer ASN comes per-message from tags instead
		if s.peer_tag == "" {
			p.asn = peerASN(s.P, d)
			if !p.rs && p.asn == 0 {
				s.Warn().Stringer("dir", d).Msg("ASPA: peer ASN unknown, first-hop check disabled")
			}
		}
	})
	return p
}

// validateMsg is the callback for UPDATE messages.
// Returns false to drop, true to keep.
func (s *Aspa) validateMsg(m *msg.Msg) bool {
	s.cnt_msg.Inc()

	aspa := s.cache.ASPAs()
	if len(aspa) == 0 {
		return true // no ASPA data
	}

	u := &m.Update
	tags := pipe.UseTags(m)

	if !u.HasReach() {
		return true // withdrawal-only, no AS_PATH to validate
	}
	aspath := u.AsPath()
	if aspath == nil || aspath.Len() == 0 {
		return true // iBGP or locally-originated
	}

	// resolve peer role and ASN for this direction
	peer := s.resolvePeer(m.Dir)
	if !peer.ok {
		return true
	}

	// peer ASN from a message tag? (multi-peer feeds, eg. ris-live)
	asn := peer.asn
	if s.peer_tag != "" {
		asn = 0
		if v, err := strconv.ParseUint(tags[s.peer_tag], 10, 32); err == nil {
			asn = uint32(v)
		}
	}

	// verify path
	flat := aspath.Unique()
	var result int
	var failCAS, failPAS uint32
	switch {
	case flat == nil:
		result = rpki.ASPA_INVALID // AS_SET present -> invalid per ASPA spec section 3
	case len(flat) == 1:
		result = rpki.ASPA_VALID // single-hop
	case s.first_hop && !peer.rs && asn != 0 && flat[0] != asn:
		// NB: per draft section 5.4/5.5 step 2, path[0] must equal the neighbor AS.
		// RS peers don't prepend their ASN (RFC 7947); --first-hop=false
		// disables this check entirely for multiplexed collector feeds.
		result = rpki.ASPA_INVALID
	default:
		result, failCAS, failPAS = rpki.VerifyPath(aspa, flat, peer.down)
	}

	// metrics and tags
	switch result {
	case rpki.ASPA_VALID:
		s.cnt_valid.Inc()
		if s.tag {
			tags["aspa/status"] = "VALID"
		}
	case rpki.ASPA_UNKNOWN:
		s.cnt_unk.Inc()
		if s.tag {
			tags["aspa/status"] = "UNKNOWN"
		}
	case rpki.ASPA_INVALID:
		s.cnt_inv.Inc()
		if s.tag {
			tags["aspa/status"] = "INVALID"
			if failCAS != 0 {
				tags["aspa/invalid-hop"] = fmt.Sprintf("%d %d", failCAS, failPAS)
			}
		}
	}
	if s.tag {
		m.Edit()
	}

	if result != rpki.ASPA_INVALID {
		return true
	}

	// event
	if s.event != "" {
		s.Event(s.event, m)
	}

	// action: ASPA condemns the entire path, not individual prefixes
	switch s.act {
	case act_drop:
		return false
	case act_withdraw:
		// move all reachable prefixes to withdrawn
		reach := slices.Clone(u.Reach)
		u.Reach = nil
		if mpp := u.ReachMP().Prefixes(); mpp != nil {
			reach = append(reach, mpp.Prefixes...)
			mpp.Prefixes = nil
		}
		if len(reach) > 0 {
			u.AddUnreach(reach...)
		}
		// NB: pure withdrawal must not carry path attributes (RFC 4271 section 4.3)
		if !u.HasReach() {
			u.Attrs.Filter(attrs.ATTR_MP_UNREACH)
		}
		m.Edit()
	}

	return true
}

func (s *Aspa) RouteHTTP(r chi.Router) error {
	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		_, _, aspas := s.cache.Sizes()
		resp := map[string]any{
			"aspa_entries": aspas,
			"metrics": map[string]uint64{
				"messages": s.cnt_msg.Get(),
				"valid":    s.cnt_valid.Get(),
				"unknown":  s.cnt_unk.Get(),
				"invalid":  s.cnt_inv.Get(),
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	return nil
}
