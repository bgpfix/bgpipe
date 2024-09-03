package stages

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpipe/core"
)

type Grep struct {
	*core.StageBase

	opt_type_apply []msg.Type
	// opt_and_event  string
	// opt_or_event   string
	opt_invert bool
	opt_any    bool

	// opt_type []msg.Type
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
	f.StringSlice("type-apply", []string{"UPDATE"},
		"apply the stage only for messages of the specified type(s)")

	// f.String("and-event", "", "on match failure, emit given event and drop the message")
	// f.String("or-event", "", "on match failure, emit given event instead of dropping the message")

	f.BoolP("invert", "v", false, "invert the logic: drop messages that DO match")
	f.BoolP("any", "a", false, "require success from ANY of the selected matchers, instead of all")

	// f.StringSlice("type", nil, "match message type(s)")
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

	// TODO
	// parse --type-apply
	for _, t := range k.Strings("type-apply") {
		// skip empty types
		if len(t) == 0 {
			continue
		}

		// canonical name?
		typ, err := msg.TypeString(t)
		if err == nil {
			s.opt_type_apply = append(s.opt_type_apply, typ)
			continue
		}

		// a plain integer?
		tnum, err2 := strconv.Atoi(t)
		if err2 == nil && tnum >= 0 && tnum <= 0xff {
			s.opt_type_apply = append(s.opt_type_apply, msg.Type(tnum))
			continue
		}

		return fmt.Errorf("--type-apply %s: %w", t, err)
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
	case len(s.opt_tag) > 0:
		break
	default:
		return fmt.Errorf("nothing to do (no filters specified)")
	}

	// register a raw callback
	cb := s.P.OnMsg(s.check, s.Dir, s.opt_type_apply...)
	cb.Raw = true

	return nil
}

func (s *Grep) check(m *msg.Msg) (keep_message bool) {
	// invert the final result, whatever it is?
	if s.opt_invert {
		defer func() { keep_message = !keep_message }()
	}

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

	// should check tags?
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
