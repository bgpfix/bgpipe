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

	opt_keep        bool
	opt_event_match string
	opt_event_fail  string
	opt_kill_match  bool
	opt_kill_fail   bool
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
	f.BoolVar(&s.opt_keep, "keep", false, "keep the message: run the filter but do not drop")
	f.StringVar(&s.opt_event_match, "event-match", "", "emit event on match success")
	f.StringVar(&s.opt_event_fail, "event-fail", "", "emit event on match fail")
	f.BoolVar(&s.opt_kill_match, "kill-match", false, "kill the process on match success")
	f.BoolVar(&s.opt_kill_fail, "kill-fail", false, "kill the process on match fail")

	return s
}

func (s *Grep) Attach() (err error) {
	k := s.K

	// check filter
	s.filter, err = filter.NewFilter(k.String("filter"))
	if err != nil {
		return err
	}

	// --keep makes sense?
	if s.opt_keep && s.opt_event_match == "" && s.opt_event_fail == "" {
		return fmt.Errorf("--keep requires at least one --event-* option")
	}

	// register callback
	s.P.OnMsg(s.check, s.Dir)
	return nil
}

func (s *Grep) check(m *msg.Msg) bool {
	// evaluate the filter
	s.eval.SetMsg(m)
	if mx := pipe.GetContext(m); mx != nil {
		s.eval.SetPipe(mx.Pipe.KV, mx.Pipe.Caps, mx.GetTags())
	}
	res := s.eval.Run(s.filter)

	// emit events / kill the session?
	if res {
		if s.opt_kill_match {
			s.Fatal().Stringer("msg", m).Msg("filter match, killing process")
		} else if s.opt_event_match != "" {
			s.Event(s.opt_event_match, m)
		}
	} else { // !res
		if s.opt_kill_fail {
			s.Fatal().Stringer("msg", m).Msg("filter fail, killing process")
		} else if s.opt_event_fail != "" {
			s.Event(s.opt_event_fail, m)
		}
	}

	// keep the message?
	if s.opt_keep {
		return true
	} else if s.invert {
		return !res
	} else {
		return res
	}
}
