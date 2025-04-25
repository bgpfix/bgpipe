package stages

import (
	"fmt"
	"slices"
	"strings"

	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpipe/core"
)

type Tag struct {
	*core.StageBase

	opt_drop []string          // tags to drop
	opt_add  map[string]string // tags to add
}

func NewTag(parent *core.StageBase) core.Stage {
	s := &Tag{StageBase: parent}

	o := &s.Options
	o.Descr = "add or drop message tags"
	o.Bidir = true
	o.FilterIn = true

	f := o.Flags
	f.StringSlice("drop", nil, "drop given tags (* means all)")
	f.StringSlice("add", nil, "add given tags (key=value)")

	return s
}

func (s *Tag) Attach() (err error) {
	k := s.K

	// parse tags to drop
	s.opt_drop = k.Strings("drop")
	if slices.Contains(s.opt_drop, "*") {
		s.opt_drop = []string{"*"}
	}

	// parse tags to add
	for i, tag := range k.Strings("add") {
		if i == 0 {
			s.opt_add = map[string]string{}
		}

		key, val, found := strings.Cut(tag, "=")
		if !found {
			return fmt.Errorf("--add %s: invalid format, need key=value", tag)
		}
		s.opt_add[key] = val
	}

	// register callback
	s.P.OnMsg(s.onmsg, s.Dir)
	return nil
}

func (s *Tag) onmsg(m *msg.Msg) bool {
	modified := false

	// get message context
	mx := pipe.GetContext(m) // NB: can be nil!

	// drop tags
	if len(s.opt_drop) > 0 && mx.HasTags() {
		if s.opt_drop[0] == "*" {
			mx.DropTags()
		} else {
			for _, tag := range s.opt_drop {
				modified = modified || mx.DropTag(tag)
			}
		}
	}

	// add tags
	if len(s.opt_add) > 0 {
		if mx == nil {
			mx = pipe.UseContext(m) // create context if needed
		}
		for key, val := range s.opt_add {
			mx.SetTag(key, val)
		}
		modified = true
	}

	if modified {
		m.Modified()
	}

	return true
}
