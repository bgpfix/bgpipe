package stages

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/VictoriaMetrics/metrics"
	"github.com/bgpfix/bgpfix/dir"
	"github.com/bgpfix/bgpfix/filter"
	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpipe/core"
	"github.com/go-chi/chi/v5"
)

// msgKey identifies a (direction, type) pair for per-combination counters
type msgKey struct {
	d dir.Dir
	t msg.Type
}

type Metrics struct {
	*core.StageBase
	eval   *filter.Eval
	rules  []metricsRule
	output string // --output file path

	// generic labeled counters
	cTotal   *metrics.Counter            // messages_total (no labels)
	cDirType map[msgKey]*metrics.Counter // messages_total{dir=...,type=...}
}

type metricsRule struct {
	filter  *filter.Filter
	label   string
	counter *metrics.Counter
}

func NewMetrics(parent *core.StageBase) core.Stage {
	s := &Metrics{StageBase: parent}
	s.eval = filter.NewEval(false)

	o := &s.Options
	o.Descr = "count messages matching filters (Prometheus metrics)"
	o.Bidir = true
	o.FilterIn = true

	f := o.Flags
	f.String("output", "", "write final metrics to file on exit (Prometheus text format)")

	return s
}

func (s *Metrics) Attach() error {
	s.output = s.K.String("output")
	prefix := s.MetricPrefix()

	// total (no labels) + per-(dir,type) labeled counters
	s.cTotal = metrics.GetOrCreateCounter(prefix + "messages_total")

	dirNames := map[dir.Dir]string{
		dir.DIR_L: "left",
		dir.DIR_R: "right",
	}
	s.cDirType = make(map[msgKey]*metrics.Counter, len(dirNames)*len(msg.TypeValues()))
	for d, dname := range dirNames {
		for _, t := range msg.TypeValues() {
			tname := strings.ToLower(t.String())
			s.cDirType[msgKey{d, t}] = metrics.GetOrCreateCounter(
				fmt.Sprintf(`%smessages_total{dir=%q,type=%q}`, prefix, dname, tname),
			)
		}
	}

	// parse positional args as [LABEL: ]FILTER
	args := s.K.Strings("args")
	for _, arg := range args {
		var label, expr string
		if l, e, ok := strings.Cut(arg, ": "); ok {
			label = l
			expr = e
		} else {
			label = arg
			expr = arg
		}

		label = core.SanitizeMetricLabel(label)

		f, err := filter.NewFilter(expr)
		if err != nil {
			return fmt.Errorf("invalid filter %q: %w", arg, err)
		}

		counter := metrics.GetOrCreateCounter(
			fmt.Sprintf(`%smatch{filter=%q}`, prefix, label),
		)

		s.rules = append(s.rules, metricsRule{
			filter:  f,
			label:   label,
			counter: counter,
		})
	}

	s.P.OnMsg(s.onMsg, s.Dir)
	return nil
}

func (s *Metrics) onMsg(m *msg.Msg) bool {
	s.cTotal.Inc()

	if c := s.cDirType[msgKey{m.Dir, m.Type}]; c != nil {
		c.Inc()
	}

	// evaluate filter rules
	if len(s.rules) > 0 {
		mx := pipe.UseContext(m)
		s.eval.Set(m, mx.Pipe.KV, mx.Pipe.Caps, mx.GetTags())
		for i := range s.rules {
			if s.eval.Run(s.rules[i].filter) {
				s.rules[i].counter.Inc()
			}
		}
	}

	return true
}

func (s *Metrics) Run() error {
	<-s.Ctx.Done()

	// write final metrics to file?
	if s.output != "" {
		f, err := os.Create(s.output)
		if err != nil {
			s.Err(err).Str("file", s.output).Msg("could not write final metrics")
		} else {
			metrics.WritePrometheus(f, false)
			f.Close()
			s.Info().Str("file", s.output).Msg("final metrics written")
		}
	}

	return nil
}

func (s *Metrics) RouteHTTP(r chi.Router) error {
	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		summary := map[string]any{
			"total": s.cTotal.Get(),
		}
		if len(s.rules) > 0 {
			rules := make(map[string]uint64, len(s.rules))
			for i := range s.rules {
				rules[s.rules[i].label] = s.rules[i].counter.Get()
			}
			summary["rules"] = rules
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(summary)
	})
	return nil
}
