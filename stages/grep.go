package stages

import (
	"fmt"
	"slices"
	"strings"

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

	opt_type []msg.Type
	// opt_af      []af.AF
	// opt_asn     []int32
	// opt_origin  []int32
	// opt_prefix  []netip.Prefix
	// opt_prefix_strict  []netip.Prefix
	// opt_nexthop []netip.Prefix
	opt_tag map[string]string
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
	f.BoolP("any", "a", false, "require success from ANY of the selected matchers, instead of all")

	f.StringSlice("type", nil, "match message type(s)")
	// f.StringSlice("af", nil, "match address families (format: AFI/SAFI)")
	// f.Int32Slice("asn", nil, "match ASNs in the AS_PATH")
	// f.Int32Slice("origin", nil, "match origin ASNs")
	// f.StringSlice("prefix", nil, "match if given prefixes cover ANY message prefix, drop not covered")
	// f.StringSlice("prefix-strict", nil, "match if given prefixes cover ALL message prefixes")
	// f.StringSlice("nexthop", nil, "match if given prefixes contain the NEXT_HOP attribute")
	f.StringSlice("tag", nil, "match message context tag values (format: key=value)")

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
	slices.Sort(s.opt_type)

	if len(s.opt_type) == 0 || s.Options.Flags.Changed("apply") {
		s.opt_apply, err = core.ParseTypes(k.Strings("apply"), nil)
		if err != nil {
			return fmt.Errorf("--apply: %w", err)
		}
		slices.Sort(s.opt_apply)
	}

	// is --type a proper subset of --apply?
	if len(s.opt_apply) > 0 {
		for _, typ := range s.opt_type {
			if _, found := slices.BinarySearch(s.opt_apply, typ); !found {
				return fmt.Errorf("--type %s not found in the --apply set: %v", typ, s.opt_apply)
			}
		}
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

	// check if anything to do?
	switch {
	case len(s.opt_type) > 0:
	case len(s.opt_tag) > 0:
	default:
		return fmt.Errorf("nothing to do (no filters specified)")
	}

	// register a raw callback
	cb := s.P.OnMsg(s.check, s.Dir, s.opt_apply...)
	cb.Raw = true // prevent parsing if possible

	return nil
}

func (s *Grep) check(m *msg.Msg) (keep_message bool) {
	defer func() {
		// invert the final result?
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
	stop := func(result bool) bool {
		switch {
		case !s.opt_any: // AND
			if !result {
				keep_message = false // any fail = drop
				return true          // stop here
			} else {
				keep_message = true // looks good
				return false        // keep going
			}
		default: // OR
			if !result {
				return false // keep going
			} else {
				keep_message = true // looks good
				return true         // stop here
			}
		}
	}

	// check type?
	if len(s.opt_type) > 0 {
		_, found := slices.BinarySearch(s.opt_type, m.Type)
		if stop(found) {
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
	}

	return
}
