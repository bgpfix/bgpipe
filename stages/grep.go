package stages

import (
	"fmt"
	"math"
	"net/netip"
	"slices"
	"strings"

	"github.com/bgpfix/bgpfix/af"
	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpipe/core"
)

type Grep struct {
	*core.StageBase

	opt_apply       []msg.Type
	opt_fail_accept string
	opt_fail_event  string
	opt_invert      bool
	opt_any         bool

	check_count int // number of different checks we must do for each message
	opt_type    []msg.Type
	opt_reach   bool
	opt_unreach bool
	opt_af      []af.AF
	opt_asn     []uint32
	opt_origin  []uint32
	// opt_prefix  []netip.Prefix
	// opt_prefix_strict  []netip.Prefix
	opt_nexthop []netip.Prefix
	opt_tag     map[string]string
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
	f.StringSlice("apply", []string{"UPDATE"},
		"apply the stage only to messages of the specified type(s)")

	f.String("fail-event", "", "on match failure, emit given event and DROP the message")
	f.String("fail-accept", "", "on match failure, emit given event and ACCEPT the message")

	f.BoolP("invert", "v", false, "invert the logic: drop messages that DO match")
	f.BoolP("any", "a", false, "require ANY successful match instead of all")

	f.StringSlice("type", nil, "require message type(s)")
	f.Bool("reach", false, "require UPDATEs that announce reachable prefixes")
	f.Bool("unreach", false, "require UPDATEs that withdraw unreachable prefixes")
	f.StringSlice("af", nil, "require UPDATEs for given address family (format: AFI/SAFI)")
	f.Bool("ipv4", false, "add IPV4/UNICAST to --af")
	f.Bool("ipv6", false, "add IPV6/UNICAST to --af")
	f.IntSlice("asn", nil, "require ASNs in the AS_PATH")
	f.IntSlice("origin", nil, "require origin ASN")
	// f.StringSlice("prefix", nil, "drop non-matching prefixes, or the whole message if nothing left")
	// f.StringSlice("prefix-strict", nil, "require match on ALL message prefixes")
	f.StringSlice("nexthop", nil, "require NEXT_HOP inside given prefix(es)")
	f.StringSlice("tag", nil, "require context tag values (format: key=value)")

	return s
}

func (s *Grep) Attach() error {
	k := s.K

	s.opt_invert = k.Bool("invert")
	s.opt_any = k.Bool("any")

	// events?
	s.opt_fail_event = k.String("fail-event")
	s.opt_fail_accept = k.String("fail-accept")
	if s.opt_fail_accept != "" && s.opt_fail_event != "" {
		return fmt.Errorf("--fail-event and --fail-accept must not be used together")
	}

	// types
	var err error
	s.opt_type, err = core.ParseTypes(k.Strings("type"), nil)
	if err != nil {
		return fmt.Errorf("--type: %w", err)
	}
	if !s.Options.Flags.Changed("type") || s.Options.Flags.Changed("apply") {
		s.opt_apply, err = core.ParseTypes(k.Strings("apply"), nil)
		if err != nil {
			return fmt.Errorf("--apply: %w", err)
		}
		slices.Sort(s.opt_apply)
		s.opt_apply = slices.Compact(s.opt_apply)
	}

	// reach / unreach?
	s.opt_reach = k.Bool("reach")
	s.opt_unreach = k.Bool("unreach")
	if (s.opt_reach || s.opt_unreach) && s.Options.Flags.Changed("type") {
		return fmt.Errorf("--type must not be used with --reach or --unreach")
	} else {
		s.opt_type = append(s.opt_type, msg.UPDATE)
	}

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

	// parse asns
	for _, asn := range k.Ints("asn") {
		if asn < 0 || asn > math.MaxUint32 {
			return fmt.Errorf("--asn %d: invalid value (must be uint32)", asn)
		}
		s.opt_asn = append(s.opt_asn, uint32(asn))
	}
	for _, asn := range k.Ints("origin") {
		if asn < 0 || asn > math.MaxUint32 {
			return fmt.Errorf("--origin %d: invalid value (must be uint32)", asn)
		}
		s.opt_origin = append(s.opt_origin, uint32(asn))
	}

	// parse AF
	for _, afs := range k.Strings("af") {
		var af af.AF
		if err := af.FromJSON([]byte(afs)); err != nil {
			return fmt.Errorf("--af %s: %w", afs, err)
		}
		s.opt_af = append(s.opt_af, af)
	}
	if k.Bool("ipv4") {
		s.opt_af = append(s.opt_af, af.AF_IPV4_UNICAST)
	}
	if k.Bool("ipv6") {
		s.opt_af = append(s.opt_af, af.AF_IPV6_UNICAST)
	}

	// require UPDATEs if --af used
	if len(s.opt_af) > 0 && s.Options.Flags.Changed("type") {
		return fmt.Errorf("--type must not be used with --af")
	} else {
		s.opt_type = append(s.opt_type, msg.UPDATE)
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
	if len(s.opt_nexthop) > 0 && s.Options.Flags.Changed("type") {
		return fmt.Errorf("--type must not be used with --nexthop")
	} else {
		s.opt_type = append(s.opt_type, msg.UPDATE)
	}

	// final check of --type
	if len(s.opt_apply) > 0 {
		slices.Sort(s.opt_type)
		s.opt_type = slices.Compact(s.opt_type)
		for _, typ := range s.opt_type {
			if _, found := slices.BinarySearch(s.opt_apply, typ); !found {
				return fmt.Errorf("--type %s not found in the --apply set: %v", typ, s.opt_apply)
			}
		}
	}

	// count how many checks we need to do
	if len(s.opt_type) > 0 {
		s.check_count++
	}
	if len(s.opt_tag) > 0 {
		s.check_count++
	}
	if s.opt_reach {
		s.check_count++
	}
	if s.opt_unreach {
		s.check_count++
	}
	if len(s.opt_asn) > 0 {
		s.check_count++
	}
	if len(s.opt_origin) > 0 {
		s.check_count++
	}
	if len(s.opt_af) > 0 {
		s.check_count++
	}
	if len(s.opt_nexthop) > 0 {
		s.check_count++
	}
	if s.check_count == 0 {
		return fmt.Errorf("nothing to do - no filters specified")
	}

	// register a raw callback
	cb := s.P.OnMsg(s.check, s.Dir, s.opt_apply...)
	cb.Raw = true // prevent parsing if possible

	return nil
}

func (s *Grep) check(m *msg.Msg) (keep_message bool) {
	// defer does the final processing of the result
	var abort bool
	defer func() {
		// unconditional drop?
		if abort {
			keep_message = false
			return
		}

		// invert the match result?
		if s.opt_invert {
			keep_message = !keep_message
		}

		// message accepted, we're done?
		if keep_message {
			return
		}

		// fire an event?
		if s.opt_fail_event != "" {
			s.Event(s.opt_fail_event, m)
		} else if s.opt_fail_accept != "" {
			s.Event(s.opt_fail_accept, m)
			keep_message = true // accept the message
		}
	}()

	// stop returns true iff keep_message already has the result
	// (which still may be inverted in the defer, though)
	stop := func(result bool) (stop_here bool) {
		switch {
		case s.opt_any: // OR
			if !result {
				return false // keep going
			} else {
				keep_message = true // looks good
				return true         // stop here
			}
		default: // AND
			if !result {
				keep_message = false // any fail = drop
				return true          // stop here
			} else {
				keep_message = true // looks good
				return false        // keep going
			}
		}
	}

	// start checks
	todo := s.check_count

	// check type?
	if len(s.opt_type) > 0 {
		_, found := slices.BinarySearch(s.opt_type, m.Type)
		if stop(found) {
			return
		}
		if todo = todo - 1; todo <= 0 {
			return
		}
	}

	// check tags?
	if len(s.opt_tag) > 0 {
		if !pipe.HasTags(m) {
			return false
		}

		mtags := pipe.MsgTags(m)
		for key, val := range s.opt_tag {
			if stop(mtags[key] == val) {
				return
			}
		}
		if todo = todo - 1; todo <= 0 {
			return
		}
	}

	// past this point we need a parsed message - lets ensure that
	err := s.P.ParseMsg(m)
	if err != nil {
		s.Err(err).Msg("could not check the message: parse error")
		abort = true
		return false
	}

	// require reach / unreach?
	if s.opt_reach {
		if stop(m.Update.HasReach()) {
			return
		}
	}
	if s.opt_unreach {
		if stop(m.Update.HasUnreach()) {
			return
		}
	}

	// require AF?
	if len(s.opt_af) > 0 {
		af := m.Update.AF()
		_, found := slices.BinarySearch(s.opt_af, af)
		if stop(found) {
			return
		}
	}

	// announces stuff?
	if m.Update.HasReach() {
		// check AS_PATH?
		if len(s.opt_asn)+len(s.opt_origin) > 0 {
			aspath := m.Update.AsPath()

			// check anywhere in AS_PATH
			for _, asn := range s.opt_asn {
				if stop(aspath.HasAsn(asn)) {
					return
				}
			}

			// check AS_PATH origin
			for _, asn := range s.opt_origin {
				if stop(aspath.HasOrigin(asn)) {
					return
				}
			}
		}

		// check NEXT_HOP?
		if len(s.opt_nexthop) > 0 {
			nh := m.Update.NextHop()
			if nh == nil {
				return false
			}

			for _, p := range s.opt_nexthop {
				if stop(p.Contains(*nh)) {
					return
				}
			}
		}
	}

	return
}
