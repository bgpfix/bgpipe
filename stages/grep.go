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

	opt_type   []msg.Type // --type
	opt_invert bool
	opt_and    bool
	opt_tag    map[string]string
}

func NewGrep(parent *core.StageBase) core.Stage {
	var (
		s = &Grep{StageBase: parent}
		o = &s.Options
	)

	o.Usage = "grep"
	o.Descr = "filter messages by their contents"
	o.Bidir = true

	f := o.Flags
	f.StringSlice("type", []string{},
		"apply only to messages of specified type(s)")
	f.BoolP("invert", "v", false,
		"invert the logic: allow only the messages that would have been dropped")
	f.Bool("and", false,
		"AND instead of OR: require all matches instead of any match")
	f.StringSlice("tag", nil, "match message context tags (key=value)")

	return s
}

func (s *Grep) Attach() error {
	k := s.K

	s.opt_invert = k.Bool("invert")
	s.opt_and = k.Bool("and")

	// parse --type
	for _, t := range k.Strings("type") {
		// skip empty types
		if len(t) == 0 {
			continue
		}

		// canonical name?
		typ, err := msg.TypeString(t)
		if err == nil {
			s.opt_type = append(s.opt_type, typ)
			continue
		}

		// a plain integer?
		tnum, err2 := strconv.Atoi(t)
		if err2 == nil && tnum >= 0 && tnum <= 0xff {
			s.opt_type = append(s.opt_type, msg.Type(tnum))
			continue
		}

		return fmt.Errorf("--type %s: %w", t, err)
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
	cb := s.P.OnMsg(s.check, s.Dir, s.opt_type...)
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
		case s.opt_and: // AND
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
