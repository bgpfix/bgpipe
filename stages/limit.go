package stages

import bgpipe "github.com/bgpfix/bgpipe/core"

type Limit struct {
	*bgpipe.StageBase
}

func NewLimit(parent *bgpipe.StageBase) bgpipe.Stage {
	var (
		s  = &Limit{StageBase: parent}
		so = &s.Options
		sf = so.Flags
	)

	so.Descr = "limit prefix lengths and counts"
	so.Events = map[string]string{
		"length":  "too long prefix announced",
		"session": "too many prefixes for the whole session",
		"origin":  "too many prefixes for a single AS origin",
		"block":   "too many prefixes for a single IP block",
	}

	sf.IntP("length", "l", 0, "prefix length limit (0 = /24 for v4, or /48 for v6)")
	sf.IntP("session", "s", 0, "session limit (0 = no limit)")
	sf.IntP("origin", "o", 0, "per-AS origin limit (0 = no limit)")
	sf.IntP("block", "b", 0, "per-IP block limit (0 = no limit)")
	sf.IntP("block-length", "B", 0, "IP block length (0 = /8 for v4, or /32 for v6)")

	// TODO: export to global?
	sf.BoolP("ipv4", "4", false, "operate on IPv4")
	sf.BoolP("ipv6", "6", false, "operate on IPv6")
	sf.StringSliceP("kill", "k", []string{"session"}, "kill the session on these events")

	return s
}

func (s *Limit) Attach() error {
	k := s.K

	s.Info().Interface("k", k.Sprint())

	return nil
}
