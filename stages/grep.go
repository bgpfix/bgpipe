package stages

import (
	"fmt"
	"math"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"github.com/bgpfix/bgpfix/afi"
	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/nlri"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpipe/core"
)

type Grep struct {
	*core.StageBase

	opt_if_type    []msg.Type
	opt_if_reach   bool
	opt_if_unreach bool

	opt_invert      bool
	opt_or          bool
	opt_strict      bool
	opt_fail_accept string
	opt_fail_event  string
	opt_fail_kill   bool
	opt_parse       bool

	enabled_matches int // number of different checks we must do for each message
	opt_type        []msg.Type
	opt_reach       bool
	opt_unreach     bool
	opt_af          []afi.AS
	opt_asn         []uint32
	opt_origin      []uint32
	opt_prefix      []nlri.NLRI
	opt_prefix_len  [129]bool
	opt_prefix_lens int // if >0, opt_prefix_len is enabled
	opt_nexthop     []netip.Prefix
	opt_tag         map[string]string
}

func NewGrep(parent *core.StageBase) core.Stage {
	var (
		s = &Grep{StageBase: parent}
		o = &s.Options
	)

	o.Usage = "grep"
	o.Descr = "drop messages that do not match"
	o.Bidir = true

	f := o.Flags

	f.StringSlice("if-type", nil, "run only for messages of the specified type(s)")
	f.Bool("if-reach", false, "run only if the message announces a reachable prefix")
	f.Bool("if-unreach", false, "run only if the message withdraws an unreachable prefix")

	f.String("fail-event", "", "on match failure, emit given event and DROP the message")
	f.String("fail-accept", "", "on match failure, emit given event and ACCEPT the message")
	f.Bool("fail-kill", false, "on match failure, kill the session")

	f.BoolP("invert", "v", false, "invert the final result: drop messages that matched successfully")
	f.BoolP("or", "o", false, "require any match type (default: require ALL match types)")
	f.BoolP("strict", "s", false, "require all values if possible (default: require ANY value)")

	f.StringSlice("type", nil, "require message type(s)")
	f.Bool("parse", false, "require the messages to parse properly, do not report message parsing errors")
	f.Bool("reach", false, "require announcement of reachable prefixes")
	f.Bool("unreach", false, "require withdrawal of unreachable prefixes")
	f.StringSlice("af", nil, "require UPDATE for given address family (format: AFI/SAFI)")
	f.Bool("ipv4", false, "shortcut: --af IPV4/UNICAST")
	f.Bool("ipv6", false, "shortcut: --af IPV6/UNICAST")
	f.Bool("flowspec", false, "shortcut: --af IPV4/FLOWSPEC,IPV6/FLOWSPEC")
	f.IntSlice("asn", nil, "require ASNs in the AS_PATH")
	f.IntSlice("origin", nil, "require origin ASN")
	f.StringSlice("prefix", nil, "require any of the given IP prefixes (or their subnets)")
	f.StringSlice("prefix-len", nil, "require prefix length (use eg. 0-8 for ranges)")
	f.StringSlice("nexthop", nil, "require NEXT_HOP inside given prefix(es)")
	f.StringSlice("tag", nil, "require context tag values (format: key=value)")

	return s
}

func (s *Grep) Attach() error {
	k := s.K

	// logic switches?
	s.opt_invert = k.Bool("invert")
	s.opt_or = k.Bool("or")
	s.opt_strict = k.Bool("strict")

	// events?
	s.opt_fail_event = k.String("fail-event")
	s.opt_fail_accept = k.String("fail-accept")
	s.opt_fail_kill = k.Bool("fail-kill")
	if s.opt_fail_accept != "" && s.opt_fail_event != "" {
		return fmt.Errorf("--fail-event and --fail-accept must not be used together")
	} else if s.opt_fail_kill && s.opt_fail_accept != s.opt_fail_event {
		return fmt.Errorf("--fail-kill must not be used together with other --fail-* options")
	}

	s.opt_parse = k.Bool("parse")

	// ---------------------

	// run-if type
	var err error
	s.opt_if_type, err = core.ParseTypes(k.Strings("if-type"), nil)
	if err != nil {
		return fmt.Errorf("--if-type: %w", err)
	}

	// run-if reach/unreach
	s.opt_if_reach = k.Bool("if-reach")
	s.opt_if_unreach = k.Bool("if-unreach")
	if s.opt_if_reach || s.opt_if_unreach {
		s.opt_if_type = append(s.opt_if_type, msg.UPDATE) // --if-type UPDATE
	}

	// dedup opt_if_type
	slices.Sort(s.opt_if_type)
	s.opt_if_type = slices.Compact(s.opt_if_type)

	// ---------------------

	// parse tags
	for i, tag := range k.Strings("tag") {
		if i == 0 {
			s.opt_tag = map[string]string{}
		}

		key, val, found := strings.Cut(tag, "=")
		if found {
			s.opt_tag[key] = val
		} else {
			return fmt.Errorf("--tag %s: invalid format, need key=value", tag)
		}
	}

	// types
	s.opt_type, err = core.ParseTypes(k.Strings("type"), nil)
	if err != nil {
		return fmt.Errorf("--type: %w", err)
	}

	// reach / unreach?
	s.opt_reach = k.Bool("reach")
	s.opt_unreach = k.Bool("unreach")
	if s.opt_reach || s.opt_unreach {
		s.opt_type = append(s.opt_type, msg.UPDATE) // --type UPDATE
	}

	// parse asns
	for _, asn := range k.Ints("asn") {
		if asn < 0 || asn > math.MaxUint32 {
			return fmt.Errorf("--asn %d: invalid value (must be uint32)", asn)
		}
		s.opt_asn = append(s.opt_asn, uint32(asn))
	}
	if len(s.opt_asn) > 0 {
		s.opt_type = append(s.opt_type, msg.UPDATE) // --type UPDATE
		slices.Sort(s.opt_asn)
		s.opt_asn = slices.Compact(s.opt_asn)
	}

	// parse origins
	for _, asn := range k.Ints("origin") {
		if asn < 0 || asn > math.MaxUint32 {
			return fmt.Errorf("--origin %d: invalid value (must be uint32)", asn)
		}
		s.opt_origin = append(s.opt_origin, uint32(asn))
	}
	if len(s.opt_origin) > 0 {
		s.opt_type = append(s.opt_type, msg.UPDATE) // --type UPDATE
		slices.Sort(s.opt_origin)
		s.opt_origin = slices.Compact(s.opt_origin)
	}

	// parse AF
	for _, afs := range k.Strings("af") {
		var as afi.AS
		if err := as.FromJSON([]byte(afs)); err != nil {
			return fmt.Errorf("--af %s: %w", afs, err)
		}
		s.opt_af = append(s.opt_af, as)
	}
	if k.Bool("ipv4") {
		s.opt_af = append(s.opt_af, afi.AS_IPV4_UNICAST)
	}
	if k.Bool("ipv6") {
		s.opt_af = append(s.opt_af, afi.AS_IPV6_UNICAST)
	}
	if len(s.opt_af) > 0 {
		s.opt_type = append(s.opt_type, msg.UPDATE) // --type UPDATE
		slices.Sort(s.opt_af)
		s.opt_af = slices.Compact(s.opt_af)
	}

	// parse NEXT_HOP
	for _, nhs := range k.Strings("nexthop") {
		if strings.IndexByte(nhs, '/') > 0 {
			p, err := netip.ParsePrefix(nhs)
			if err != nil {
				return fmt.Errorf("--nexthop %s: %w", nhs, err)
			}
			s.opt_nexthop = append(s.opt_nexthop, p)
		} else {
			a, err := netip.ParseAddr(nhs)
			if err != nil {
				return fmt.Errorf("--nexthop %s: %w", nhs, err)
			}
			p := netip.PrefixFrom(a, a.BitLen())
			s.opt_nexthop = append(s.opt_nexthop, p)
		}
	}
	// require UPDATEs if --nexthop used
	if len(s.opt_nexthop) > 0 {
		s.opt_type = append(s.opt_type, msg.UPDATE) // --type UPDATE
	}

	// parse IP prefixes
	for _, ps := range k.Strings("prefix") {
		p, err := netip.ParsePrefix(ps)
		if err != nil {
			return fmt.Errorf("--prefix %s: %w", ps, err)
		}
		s.opt_prefix = append(s.opt_prefix, nlri.FromPrefix(p))
	}
	// require UPDATEs if --prefix used
	if len(s.opt_prefix) > 0 {
		s.opt_type = append(s.opt_type, msg.UPDATE) // --type UPDATE
	}

	// prefix lengths
	for _, pl := range k.Strings("prefix-len") {
		before, after, ok := strings.Cut(pl, "-")

		// parse the first (or the only)
		v1, err := strconv.Atoi(before)
		if err != nil {
			return fmt.Errorf("--prefix-len %s: %w", pl, err)
		} else if v1 < 0 || v1 > 128 {
			return fmt.Errorf("--prefix-len %s: invalid value", pl)
		}

		// handle ranges
		v2 := v1
		if ok {
			v2, err = strconv.Atoi(after)
			if err != nil {
				return fmt.Errorf("--prefix-len %s: %w", pl, err)
			} else if v2 < 0 || v2 > 128 || v2 < v1 {
				return fmt.Errorf("--prefix-len %s: invalid value", pl)
			}
		}

		// add
		for l := v1; l <= v2; l++ {
			s.opt_prefix_len[l] = true
			s.opt_prefix_lens++
		}
	}
	// require UPDATEs if --prefix-len used
	if s.opt_prefix_lens > 0 {
		s.opt_type = append(s.opt_type, msg.UPDATE) // --type UPDATE
	}

	// dedup --type
	slices.Sort(s.opt_type)
	s.opt_type = slices.Compact(s.opt_type)

	// ------------------------------------------

	// count how many checks we need to do
	if s.opt_parse {
		s.enabled_matches++
	}
	if len(s.opt_type) > 0 {
		s.enabled_matches++
	}
	if s.opt_reach {
		s.enabled_matches++
	}
	if s.opt_unreach {
		s.enabled_matches++
	}
	if len(s.opt_af) > 0 {
		s.enabled_matches++
	}
	if len(s.opt_asn) > 0 {
		s.enabled_matches++
	}
	if len(s.opt_origin) > 0 {
		s.enabled_matches++
	}
	if len(s.opt_nexthop) > 0 {
		s.enabled_matches++
	}
	if len(s.opt_prefix) > 0 {
		s.enabled_matches++
	}
	if len(s.opt_tag) > 0 {
		s.enabled_matches++
	}
	if s.opt_prefix_lens > 0 {
		s.enabled_matches++
	}
	if s.enabled_matches == 0 {
		return fmt.Errorf("nothing to do: no matches specified")
	}

	// register a raw callback
	cb := s.P.OnMsg(s.check, s.Dir, s.opt_if_type...)
	cb.Raw = true // prevent parsing if possible

	return nil
}

// parse parses message m and returns true on success,
// or logs the error and returns false otherwise (unless opt_parse is true)
// NB: an event can be generated by bgpipe in case of parse error as well
func (s *Grep) parse(m *msg.Msg) bool {
	// optimization: already done?
	if m.Upper != msg.INVALID {
		return true
	}

	// silent parse errors?
	if s.opt_parse {
		err := m.Parse(s.P.Caps)
		return err == nil
	}

	// standard path, emit event on parse errors
	err := s.P.ParseMsg(m)
	if err == nil {
		return true // success
	} else {
		s.Err(err).Msg("could not check the message: parse error")
		return false
	}
}

// should_run returns: 1 if we should run the stage for message m, -1 if not, or 0 means abort
func (s *Grep) should_run_stage(m *msg.Msg) int {
	// --if-type handled using bgpfix callback mechanism

	// --if-reach
	if s.opt_if_reach {
		if !s.parse(m) {
			return 0
		}
		if !s.check_reach(m) {
			return -1
		}
	}

	// --if-unreach
	if s.opt_if_unreach {
		if !s.parse(m) {
			return 0
		}
		if !s.check_unreach(m) {
			return -1
		}
	}

	// all --if-* checks good
	return 1
}

func (s *Grep) check(m *msg.Msg) (accept_message bool) {
	// check if we should run all of this at all
	switch s.should_run_stage(m) {
	case 1:
		break // yeah, run below checks
	case -1:
		return true // ignore the message, we should not run below checks
	default:
		return false // drop the message as-is, something is really broken
	}

	// defer does the final processing of the result
	abort := false // emergency stop
	defer func() {
		// unconditional drop?
		if abort {
			accept_message = false
			return
		}

		// invert the match result?
		if s.opt_invert {
			accept_message = !accept_message
		}

		// if message accepted, we're done
		if accept_message {
			return
		}

		// handle --fail-* options
		switch {
		case s.opt_fail_kill:
			s.Fatal().Stringer("msg", m).Msg("message did not match")
		case s.opt_fail_event != "":
			s.Event(s.opt_fail_event, m)
		case s.opt_fail_accept != "":
			s.Event(s.opt_fail_accept, m)
			accept_message = true // accept the message
		}
	}()

	// run_match calls given check function iff needed, and interprets its results
	todo := s.enabled_matches // how many match types remaining?
	run_match := func(check func(*msg.Msg) bool, enabled bool) (keep_going bool) {
		// is this match type enabled at all?
		if !enabled {
			return true // no, keep going
		} else {
			todo--
		}

		// interpret the result respecting the --or-match flag
		switch success := check(m); {
		case s.opt_or: // OR logic, require any match type
			if success {
				accept_message = true // accept as-is
				return false          // stop here
			} else if todo <= 0 {
				accept_message = false // that was the last match type, no success = drop as-is
				return false           // stop here
			} else {
				return true // no result yet, keep going
			}
		default: // AND logic, require all match types
			if !success {
				accept_message = false // drop as-is
				return false           // stop here
			} else if todo <= 0 {
				accept_message = true // that was the last match type, all good = accept as-is
				return false          // stop here
			} else {
				return true // no result yet, keep going
			}
		}
	}

	// does it parse properly?
	if !run_match(s.parse, s.opt_parse) {
		return
	}

	// check message type
	if !run_match(s.check_type, len(s.opt_type) > 0) {
		return
	}

	// check tags
	if !run_match(s.check_tag, len(s.opt_tag) > 0) {
		return
	}

	// -------------------------------
	// past this point we must have a parsed message - lets ensure this
	if !s.parse(m) {
		abort = true
		return
	}
	// -------------------------------

	// require reach / unreach?
	if !run_match(s.check_reach, s.opt_reach) {
		return
	}
	if !run_match(s.check_unreach, s.opt_unreach) {
		return
	}

	// require AF?
	if !run_match(s.check_af, len(s.opt_af) > 0) {
		return
	}

	// check AS_PATH contents?
	if !run_match(s.check_asn, len(s.opt_asn) > 0) {
		return
	}

	// check AS_PATH origin?
	if !run_match(s.check_origin, len(s.opt_origin) > 0) {
		return
	}

	// check nexthop?
	if !run_match(s.check_nexthop, len(s.opt_nexthop) > 0) {
		return
	}

	// check prefix?
	if !run_match(s.check_prefix, len(s.opt_prefix) > 0) {
		return
	}

	// check prefix length?
	if !run_match(s.check_prefix_len, s.opt_prefix_lens > 0) {
		return
	}

	// if AND, no failures so far is a success
	// if OR, no successes so far is a failure
	return !s.opt_or
}

// returns: 1 stop with success, -1 stop with failure, 0 keep going
func (s *Grep) check_if(cond bool) int {
	switch {
	case s.opt_strict: // AND logic, require all values to match
		if !cond {
			return -1 // drop as-is, stop here
		} else {
			return 0 // no result yet, keep going
		}
	default: // OR logic, require any value to match
		if cond {
			return 1 // accept as-is, stop here
		} else {
			return 0 // no result yet, keep running
		}
	}
}

func (s *Grep) check_type(m *msg.Msg) bool {
	_, found := slices.BinarySearch(s.opt_type, m.Type)
	return found
}

func (s *Grep) check_tag(m *msg.Msg) bool {
	if !pipe.HasTags(m) {
		return false
	}

	mtags := pipe.MsgTags(m)
	for key, val := range s.opt_tag {
		ok := mtags[key] == val
		switch s.check_if(ok) {
		case 1:
			return true
		case -1:
			return false
		}
	}

	return s.opt_strict
}

func (s *Grep) check_reach(m *msg.Msg) bool {
	return m.Update.HasReach()
}

func (s *Grep) check_unreach(m *msg.Msg) bool {
	return m.Update.HasUnreach()
}

func (s *Grep) check_af(m *msg.Msg) bool {
	val := m.Update.AS()
	if val == afi.AS_INVALID {
		return false
	}

	_, found := slices.BinarySearch(s.opt_af, val)
	return found
}

func (s *Grep) check_asn(m *msg.Msg) bool {
	aspath := m.Update.AsPath()
	if aspath == nil {
		return false
	}

	// check anywhere in AS_PATH, including AS_SETs
	for _, asn := range s.opt_asn {
		ok := aspath.HasAsn(asn, 0)
		switch s.check_if(ok) {
		case 1:
			return true
		case -1:
			return false
		}
	}

	return s.opt_strict
}

func (s *Grep) check_origin(m *msg.Msg) bool {
	aspath := m.Update.AsPath()
	if aspath == nil {
		return false
	}

	// check at AS_PATH origin
	for _, asn := range s.opt_origin {
		ok := aspath.HasOrigin(asn, 0)
		switch s.check_if(ok) {
		case 1:
			return true
		case -1:
			return false
		}
	}

	return s.opt_strict
}

func (s *Grep) check_nexthop(m *msg.Msg) bool {
	nh := m.Update.NextHop()
	if !nh.IsValid() {
		return false
	}

	for _, p := range s.opt_nexthop {
		ok := p.Contains(nh)
		switch s.check_if(ok) {
		case 1:
			return true
		case -1:
			return false
		}
	}

	return s.opt_strict
}

func (s *Grep) check_prefix(m *msg.Msg) bool {
	// collect all prefixes in message
	var inmsg []nlri.NLRI
	inmsg = m.Update.GetReach(inmsg)
	inmsg = m.Update.GetUnreach(inmsg)

	for i := range inmsg {
		// is the prefix covered by any --prefix value?
		ok := inmsg[i].FindParent(s.opt_prefix) >= 0

		switch s.check_if(ok) {
		case 1:
			return true
		case -1:
			return false
		}
	}

	return s.opt_strict
}

func (s *Grep) check_prefix_len(m *msg.Msg) bool {
	// collect all prefixes in message
	var inmsg []nlri.NLRI
	inmsg = m.Update.GetReach(inmsg)
	inmsg = m.Update.GetUnreach(inmsg)

	for i := range inmsg {
		pl := inmsg[i].Bits()
		ok := pl >= 0 && pl <= 128 && s.opt_prefix_len[pl]
		switch s.check_if(ok) {
		case 1:
			return true
		case -1:
			return false
		}
	}

	return s.opt_strict
}
