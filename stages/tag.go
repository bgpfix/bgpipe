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

	drop []string          // tags to drop
	add  map[string]string // tags to add
	src  bool              // add SRC tag with source stage name
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
	f.Bool("src", false, "add SRC tag with source stage name")

	return s
}

func (s *Tag) Attach() (err error) {
	k := s.K

	s.src = k.Bool("src")

	// parse tags to drop
	s.drop = k.Strings("drop")
	if slices.Contains(s.drop, "*") {
		s.drop = []string{"*"}
	}

	// parse tags to add
	for i, tag := range k.Strings("add") {
		if i == 0 {
			s.add = map[string]string{}
		}

		key, val, found := strings.Cut(tag, "=")
		if !found {
			return fmt.Errorf("--add %s: invalid format, need key=value", tag)
		}
		s.add[key] = val
	}

	// register callback
	s.P.OnMsg(s.onmsg, s.Dir)
	return nil
}

func (s *Tag) onmsg(m *msg.Msg) bool {
	t := &m.Tags
	if len(s.drop) > 0 && s.drop[0] == "*" {
		t.Reset()
	} else {
		for _, tag := range s.drop {
			t.Set(tag, "")
		}
	}
	if mx := pipe.GetContext(m); s.src && mx != nil && mx.Input != nil {
		t.Src = mx.Input.Name
	}
	for k, v := range s.add {
		t.Set(k, v)
	}
	m.DropJSON() // NB: tags only, keep the wire as-is
	return true
}
