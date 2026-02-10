package stages

import (
	"fmt"

	"github.com/bgpfix/bgpfix/filter"
	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpipe/core"
)

type Grep struct {
	*core.StageBase
	invert bool
	filter *filter.Filter
	eval   *filter.Eval

	keep        bool
	event_match string
	event_fail  string
	kill_match  bool
	kill_fail   bool
}

func NewGrep(parent *core.StageBase) core.Stage {
	s := &Grep{StageBase: parent}
	s.eval = filter.NewEval(false)

	o := &s.Options
	o.Bidir = true
	o.FilterIn = true
	o.Args = []string{"filter"}
	if s.Cmd == "drop" {
		s.invert = true
		o.Usage = "drop FILTER"
		o.Descr = "drop messages that match a filter"
	} else {
		o.Usage = "grep FILTER"
		o.Descr = "drop messages that DO NOT match a filter"
	}

	f := o.Flags
	f.Bool("keep", false, "keep the message: run the filter but do not drop")
	f.String("event-match", "", "emit event on match success")
	f.String("event-fail", "", "emit event on match fail")
	f.Bool("kill-match", false, "kill the process on match success")
	f.Bool("kill-fail", false, "kill the process on match fail")

	return s
}

func (s *Grep) Attach() (err error) {
	k := s.K

	// fetch flag values
	s.keep = k.Bool("keep")
	s.event_match = k.String("event-match")
	s.event_fail = k.String("event-fail")
	s.kill_match = k.Bool("kill-match")
	s.kill_fail = k.Bool("kill-fail")

	// check filter
	s.filter, err = filter.NewFilter(k.String("filter"))
	if err != nil {
		return err
	}

	// --keep makes sense?
	if s.keep && s.event_match == "" && s.event_fail == "" {
		return fmt.Errorf("--keep requires at least one --event-* option")
	}

	// register callback
	s.P.OnMsg(s.check, s.Dir)
	return nil
}

func (s *Grep) check(m *msg.Msg) bool {
	// evaluate the filter
	mx := pipe.UseContext(m)
	s.eval.Set(m, mx.Pipe.KV, mx.Pipe.Caps, mx.GetTags())
	res := s.eval.Run(s.filter)

	// emit events / kill the session?
	if res {
		if s.kill_match {
			s.Fatal().Stringer("msg", m).Msg("filter match, killing process")
		} else if s.event_match != "" {
			s.Event(s.event_match, m)
		}
	} else { // !res
		if s.kill_fail {
			s.Fatal().Stringer("msg", m).Msg("filter fail, killing process")
		} else if s.event_fail != "" {
			s.Event(s.event_fail, m)
		}
	}

	// keep the message?
	if s.keep {
		return true
	} else if s.invert {
		return !res
	} else {
		return res
	}
}
