package stages

import (
	"net/http"
	"strings"

	"github.com/VictoriaMetrics/metrics"
	"github.com/bgpfix/bgpfix/dir"
	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpipe/core"
	"github.com/go-chi/chi/v5"
)

type Metrics struct {
	*core.StageBase

	cMsgTotal     *metrics.Counter
	cMsgLeft      *metrics.Counter
	cMsgRight     *metrics.Counter
	cMsgType      map[msg.Type]*metrics.Counter
	cReach        *metrics.Counter
	cUnreach      *metrics.Counter
	cReachLeft    *metrics.Counter
	cReachRight   *metrics.Counter
	cUnreachLeft  *metrics.Counter
	cUnreachRight *metrics.Counter
}

func NewMetrics(parent *core.StageBase) core.Stage {
	s := &Metrics{StageBase: parent}

	o := &s.Options
	o.Descr = "expose basic BGP Prometheus metrics"
	o.Bidir = true
	o.FilterIn = true

	return s
}

func (s *Metrics) Attach() error {
	prefix := strings.ToLower("bgpipe_stage_" + s.Name)
	prefix = strings.ReplaceAll(prefix, "@", "")
	prefix = strings.ReplaceAll(prefix, "-", "_")
	prefix = strings.ReplaceAll(prefix, ".", "_")

	s.cMsgTotal = metrics.GetOrCreateCounter(prefix + "_messages_total")
	s.cMsgLeft = metrics.GetOrCreateCounter(prefix + "_messages_left_total")
	s.cMsgRight = metrics.GetOrCreateCounter(prefix + "_messages_right_total")

	s.cMsgType = make(map[msg.Type]*metrics.Counter, 6)
	for _, t := range msg.TypeValues() {
		name := strings.ToLower(t.String())
		s.cMsgType[t] = metrics.GetOrCreateCounter(prefix + "_messages_type_" + name + "_total")
	}

	s.cReach = metrics.GetOrCreateCounter(prefix + "_update_reach_total")
	s.cUnreach = metrics.GetOrCreateCounter(prefix + "_update_unreach_total")
	s.cReachLeft = metrics.GetOrCreateCounter(prefix + "_update_reach_left_total")
	s.cReachRight = metrics.GetOrCreateCounter(prefix + "_update_reach_right_total")
	s.cUnreachLeft = metrics.GetOrCreateCounter(prefix + "_update_unreach_left_total")
	s.cUnreachRight = metrics.GetOrCreateCounter(prefix + "_update_unreach_right_total")

	s.P.OnMsg(s.onMsg, s.Dir)
	return nil
}

func (s *Metrics) onMsg(m *msg.Msg) bool {
	s.cMsgTotal.Inc()

	switch m.Dir {
	case dir.DIR_L:
		s.cMsgLeft.Inc()
	case dir.DIR_R:
		s.cMsgRight.Inc()
	}

	if c := s.cMsgType[m.Type]; c != nil {
		c.Inc()
	}

	if m.Type == msg.UPDATE {
		reach := len(m.Update.Reach)
		unreach := len(m.Update.Unreach)
		if reach > 0 {
			s.cReach.Add(reach)
			switch m.Dir {
			case dir.DIR_L:
				s.cReachLeft.Add(reach)
			case dir.DIR_R:
				s.cReachRight.Add(reach)
			}
		}
		if unreach > 0 {
			s.cUnreach.Add(unreach)
			switch m.Dir {
			case dir.DIR_L:
				s.cUnreachLeft.Add(unreach)
			case dir.DIR_R:
				s.cUnreachRight.Add(unreach)
			}
		}
	}

	return true
}

func (s *Metrics) RouteHTTP(r chi.Router) error {
	r.Get("/", s.httpRoot)
	r.Get("/metrics", s.httpMetrics)
	return nil
}

func (s *Metrics) httpRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func (s *Metrics) httpMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	metrics.WritePrometheus(w, true)
}
