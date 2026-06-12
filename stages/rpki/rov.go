package rpki

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/VictoriaMetrics/metrics"
	"github.com/bgpfix/bgpfix/attrs"
	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/nlri"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpfix/rpki"
	"github.com/bgpfix/bgpipe/core"
	"github.com/go-chi/chi/v5"
)

// Rov validates UPDATE messages using RPKI Route Origin Validation (RFC 6811)
type Rov struct {
	*core.StageBase
	cache *rpki.Cache // shared RPKI cache, maintained by the core
	split *pipe.Input // used for --invalid=split

	act    int    // action for INVALID prefixes
	strict bool   // treat NOT_FOUND same as INVALID
	tag    bool   // add rov/ tags
	event  string // emit event on INVALID

	cnt_msg   *metrics.Counter
	cnt_valid *metrics.Counter
	cnt_inv   *metrics.Counter
	cnt_nf    *metrics.Counter
}

func NewRov(parent *core.StageBase) core.Stage {
	s := &Rov{StageBase: parent}

	o := &s.Options
	o.Descr = "validate route origins using RPKI ROV"
	o.FilterIn = true
	o.Bidir = true

	f := o.Flags
	f.String("invalid", "withdraw", "action for INVALID prefixes: withdraw|filter|drop|split|keep")
	f.Bool("strict", false, "treat NOT_FOUND same as INVALID")
	f.Bool("tag", true, "add rov/ validation status to message tags")
	f.String("event", "", "emit event on INVALID prefixes")
	f.Bool("no-wait", false, "start before the RPKI cache is ready")

	return s
}

func (s *Rov) Attach() error {
	k := s.K

	switch strings.ToLower(k.String("invalid")) {
	case "withdraw":
		s.act = act_withdraw
	case "drop":
		s.act = act_drop
	case "filter":
		s.act = act_filter
	case "split":
		s.act = act_split
	case "keep":
		s.act = act_keep
	default:
		return fmt.Errorf("--invalid must be withdraw, filter, drop, split or keep")
	}

	s.strict = k.Bool("strict")
	s.tag = k.Bool("tag")
	s.event = k.String("event")

	// use the shared RPKI cache, maintained by the bgpipe core
	s.cache = s.B.UseRpki()

	// prometheus metrics
	prefix := s.MetricPrefix()
	s.cnt_msg = metrics.GetOrCreateCounter(prefix + "messages_total")
	s.cnt_valid = metrics.GetOrCreateCounter(prefix + "valid_total")
	s.cnt_inv = metrics.GetOrCreateCounter(prefix + "invalid_total")
	s.cnt_nf = metrics.GetOrCreateCounter(prefix + "not_found_total")

	// subscribe to UPDATE messages in given direction
	s.P.OnMsg(s.validateMsg, s.Dir, msg.UPDATE)

	// need input for the split action?
	if s.act == act_split {
		s.split = s.P.AddInput(s.Dir)
	}

	return nil
}

func (s *Rov) Prepare() error {
	if !s.K.Bool("no-wait") {
		return s.cache.WaitReady(s.Ctx)
	}
	return nil
}

// validateMsg is the callback for UPDATE messages.
func (s *Rov) validateMsg(m *msg.Msg) bool {
	s.cnt_msg.Inc()

	u := &m.Update
	tags := pipe.UseTags(m)

	// current VRP snapshots
	v4, v6 := s.cache.VRPs()

	// origin AS from AS_PATH
	origin := u.AsPath().Origin()

	// check each reachable prefix, optionally deleting invalid ones
	var valid, invalid, not_found []nlri.Prefix
	do_delete := s.act == act_withdraw || s.act == act_filter || s.act == act_split
	check := func(p nlri.Prefix) bool {
		res := rpki.ValidateOrigin(v4, v6, p.Prefix, origin)
		if res == rpki.ROV_NOT_FOUND && s.strict {
			res = rpki.ROV_INVALID
		}
		switch res {
		case rpki.ROV_VALID:
			s.cnt_valid.Inc()
			valid = append(valid, p)
			if s.tag {
				tags["rov/"+p.String()] = "VALID"
			}
			return false

		case rpki.ROV_NOT_FOUND:
			s.cnt_nf.Inc()
			not_found = append(not_found, p)
			if s.tag {
				tags["rov/"+p.String()] = "NOT_FOUND"
			}
			return false

		case rpki.ROV_INVALID:
			s.cnt_inv.Inc()
			invalid = append(invalid, p)
			return do_delete
		}
		panic("unreachable")
	}

	u.Reach = slices.DeleteFunc(u.Reach, check)
	if mpp := u.ReachMP().Prefixes(); mpp != nil && mpp.Len() > 0 {
		mpp.Prefixes = slices.DeleteFunc(mpp.Prefixes, check)
	}

	// act on ROV results
	if len(invalid) > 0 {
		if s.tag || s.act != act_keep {
			m.Edit()
		}

		// split invalid prefixes into separate UPDATE?
		do_split := s.act == act_split && len(valid)+len(not_found) > 0
		m2, t2 := m, tags
		if do_split {
			m2 = s.P.GetMsg().Switch(msg.UPDATE)
			m2.Time = m.Time
			t2 = pipe.UseTags(m2)
			for k, v := range tags {
				if !strings.HasPrefix(k, "rov/") {
					t2[k] = v
				}
			}
		}

		if s.tag {
			t2["rov/status"] = "INVALID"
			for _, p := range invalid {
				t2["rov/"+p.String()] = "INVALID"
			}
		}

		if s.act == act_split || s.act == act_withdraw {
			m2.Update.AddUnreach(invalid...)
		}

		// drop attributes if no reachable prefixes left
		if do_delete && len(valid)+len(not_found) == 0 {
			m2.Update.Attrs.Filter(attrs.ATTR_MP_UNREACH)
		}

		if s.event != "" {
			s.Event(s.event, m2)
		}

		if s.act == act_drop {
			return false
		}

		if do_split {
			s.split.WriteMsg(m2)
			// tag the original (valid/not-found) message so downstream filters work
			if s.tag {
				switch {
				case len(valid) > 0:
					tags["rov/status"] = "VALID"
				case len(not_found) > 0:
					tags["rov/status"] = "NOT_FOUND"
				}
			}
		}

		if s.act != act_keep && !do_split && !u.HasReach() && !u.HasUnreach() {
			return false
		}
	} else if s.tag {
		switch {
		case len(not_found) > 0:
			tags["rov/status"] = "NOT_FOUND"
			m.Edit()
		case len(valid) > 0:
			tags["rov/status"] = "VALID"
			m.Edit()
		}
	}

	return true
}

func (s *Rov) RouteHTTP(r chi.Router) error {
	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		vrps4, vrps6, _ := s.cache.Sizes()
		resp := map[string]any{
			"vrps_ipv4": vrps4,
			"vrps_ipv6": vrps6,
			"metrics": map[string]uint64{
				"messages":  s.cnt_msg.Get(),
				"valid":     s.cnt_valid.Get(),
				"invalid":   s.cnt_inv.Get(),
				"not_found": s.cnt_nf.Get(),
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	return nil
}
