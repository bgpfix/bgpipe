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
	mx := pipe.UseContext(m)
	n := len(mx.Tags)

	// drop tags
	if len(s.drop) > 0 && s.drop[0] == "*" {
		mx.Tags = mx.Tags[:0]
	} else {
		for _, tag := range s.drop {
			mx.Tags.Set(tag, "")
		}
	}
	mod := len(mx.Tags) != n

	// add SRC tag with source stage name
	if s.src && mx.Input != nil && mx.Input.Name != "" {
		mod = true
		mx.Tags.Set("SRC", mx.Input.Name)
	}

	// add tags
	for k, v := range s.add {
		mod = true
		mx.Tags.Set(k, v)
	}

	if mod {
		m.DropJSON() // NB: tags only, keep the wire as-is
	}
	return true
}
