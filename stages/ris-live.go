package stages

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"math"
	"math/rand"
	"net"
	"net/http"
	"time"

	"github.com/bgpfix/bgpfix/json"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpipe/core"
	"github.com/buger/jsonparser"
)

// RisLive reads BGP updates from RIPE RIS Live streaming endpoint
type RisLive struct {
	*core.StageBase
	in *pipe.Input

	url          string
	subscribe    string
	timeout      time.Duration
	timeout_read time.Duration
	retry        bool
	retry_max    int
	delay_err    time.Duration

	raw []byte // reusable buffer for hex decoding
}

func NewRisLive(parent *core.StageBase) core.Stage {
	var (
		s = &RisLive{StageBase: parent}
		o = &s.Options
		f = o.Flags
	)

	o.Descr = "read BGP updates from RIPE RIS Live"
	o.IsProducer = true
	o.FilterOut = true

	f.String("url", "https://ris-live.ripe.net/v1/stream/?format=json&client=bgpipe",
		"RIS Live streaming endpoint URL")
	f.String("sub", "", "X-RIS-Subscribe header, see https://ris-live.ripe.net/manual/#ris_subscribe")
	f.Duration("timeout", 10*time.Second, "connect timeout (0 means none)")
	f.Duration("read-timeout", 10*time.Second, "stream read timeout (max time between messages)")
	f.Bool("retry", true, "retry connection on errors")
	f.Int("retry-max", 0, "maximum number of connection retries (0 means unlimited)")
	f.Duration("delay-err", 3*time.Minute, "treat too old messages as connection errors (0 to disable)")

	return s
}

func (s *RisLive) Attach() error {
	k := s.K
	s.url = k.String("url")
	s.subscribe = k.String("sub")
	s.timeout = k.Duration("timeout")
	s.timeout_read = k.Duration("read-timeout")
	s.retry = k.Bool("retry")
	s.retry_max = k.Int("retry-max")
	s.delay_err = k.Duration("delay-err")

	// ensure --subscribe includes includeRaw=true
	if sl := len(s.subscribe); sl > 0 {
		val, err := jsonparser.GetBoolean(json.B(s.subscribe), "includeRaw")
		if err != nil {
			s.subscribe = s.subscribe[:sl-1] + `, "includeRaw": true }` // best-effort add includeRaw
			val, err = jsonparser.GetBoolean(json.B(s.subscribe), "includeRaw")
		}
		if err != nil || !val {
			return fmt.Errorf("invalid --subscribe: must be JSON object with includeRaw=true")
		}
	}

	s.in = s.P.AddInput(s.Dir)
	return nil
}

func (s *RisLive) Run() error {
	defer s.in.Close()

	// HTTP client with connect timeout
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout: s.timeout,
			}).DialContext,
			TLSHandshakeTimeout:   s.timeout,
			ResponseHeaderTimeout: s.timeout,
		},
	}

	last_try := time.Now()
	for try := 1; s.Ctx.Err() == nil; try++ {
		// check retry limit
		if time.Since(last_try) > time.Hour {
			try = 1 // reset try count after long wait
		}
		if s.retry_max > 0 && try > s.retry_max {
			return fmt.Errorf("max retries (%d) exceeded", s.retry_max)
		}
		last_try = time.Now()

		// backoff before retry (skip on first try)
		if try > 1 {
			sec := min(60, (try-1)*(try-1)) + rand.Intn(try)
			s.Info().Int("try", try).Int("wait_sec", sec).Msg("waiting before reconnect")
			select {
			case <-time.After(time.Duration(sec) * time.Second):
			case <-s.Ctx.Done():
				return context.Cause(s.Ctx)
			}
		}

		// create HTTP request
		req, err := http.NewRequestWithContext(s.Ctx, "GET", s.url, nil)
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}
		if len(s.subscribe) > 0 {
			req.Header.Set("X-RIS-Subscribe", s.subscribe)
		}

		// make request
		s.Debug().Str("url", s.url).Msg("connecting")
		resp, err := client.Do(req)
		if err != nil {
			if !s.retry {
				return fmt.Errorf("connection failed: %w", err)
			} else if nerr, ok := err.(net.Error); ok && nerr.Timeout() {
				s.Warn().Err(err).Msg("connection failed")
				continue
			} else {
				return fmt.Errorf("connection not possible: %w", err)
			}
		}

		// check status code
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			if !s.retry {
				return fmt.Errorf("HTTP %s", resp.Status)
			} else {
				s.Warn().Msgf("bad HTTP status: %s", resp.Status)
				continue
			}
		}

		// stream messages
		s.Info().Str("url", s.url).Msg("connected")
		err = s.stream(resp)
		if !s.retry {
			return fmt.Errorf("stream error: %w", err)
		} else {
			s.Warn().Err(err).Msg("stream ended")
		}
	}

	return context.Cause(s.Ctx)
}

func (s *RisLive) stream(resp *http.Response) error {
	defer resp.Body.Close()

	// read timeout
	rt := time.AfterFunc(s.timeout_read, func() {
		s.Error().Msg("read timeout")
		resp.Body.Close()
	})
	defer rt.Stop()

	// prepare scanner, use 1MiB buffer
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(nil, 1024*1024)

	// read lines
	var last_ts int64
	for (rt.Reset(s.timeout_read) || true) && scanner.Scan() {
		// skip empty lines
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		// stop the timer to avoid closing while processing
		rt.Stop()

		// parse and process message
		ts, err := s.process(line)
		if s.Ctx.Err() != nil || err == pipe.ErrInClosed {
			return nil // stopping
		} else if err != nil {
			s.Warn().Err(err).Msgf("input error: %s", line)
			continue
		} else if ts.IsZero() {
			continue // skipped
		}

		// need to check the timestamp?
		if s.delay_err > 0 && ts.Unix() > last_ts {
			last_ts = ts.Unix()
			if delay := time.Since(ts); delay > s.delay_err {
				return fmt.Errorf("message delay too high: %s", delay)
			}
		}
	}
	return scanner.Err()
}

// RIS Live JSON paths to extract
var risPaths = [][]string{
	{"type"},
	{"data", "timestamp"},
	{"data", "peer"},
	{"data", "peer_asn"},
	{"data", "id"},
	{"data", "host"},
	{"data", "type"},
	{"data", "raw"},
}

// process parses buf and writes the resulting BGP message to the pipe,
// returning the message timestamp.
func (s *RisLive) process(buf []byte) (ts time.Time, _ error) {
	// sanity check
	if l := len(buf); l < 10 || buf[0] != '{' || buf[l-1] != '}' {
		return ts, fmt.Errorf("invalid JSON")
	}

	// parse JSON
	var (
		ris_type string
		msg_type string
		peer_ip  string
		peer_asn string
		id       string
		host     string
		raw_hex  []byte
	)
	if err := json.ObjectPaths(buf, func(pid int, val []byte, typ json.Type) error {
		switch pid {
		case 0: // type
			ris_type = json.S(val) // NB: do not store
		case 1: // data.timestamp
			v, err := jsonparser.ParseFloat(val)
			if err != nil {
				return fmt.Errorf("invalid timestamp: %w", err)
			}
			sec, nsec := math.Modf(v)
			ts = time.Unix(int64(sec), int64(nsec*1e9)).UTC()
		case 2: // data.peer
			peer_ip = string(val)
		case 3: // data.peer_asn
			peer_asn = string(val)
		case 4: // data.id
			id = string(val)
		case 5: // data.host
			host = string(val)
		case 6: // data.type
			msg_type = json.S(val) // NB: do not store
		case 7: // data.raw
			raw_hex = val
		}
		return nil
	}, risPaths...); err != nil {
		return ts, err
	}

	// sanity checks
	switch {
	case ris_type != "ris_message":
		return ts, nil // NB: silently skip
	case msg_type == "STATE":
		return ts, nil // NB: silently skip
	case ts.IsZero():
		return ts, fmt.Errorf("missing timestamp")
	case len(peer_ip) == 0:
		return ts, fmt.Errorf("missing peer")
	case len(peer_asn) == 0:
		return ts, fmt.Errorf("missing peer_asn")
	case len(host) == 0:
		return ts, fmt.Errorf("missing host")
	case len(raw_hex) == 0:
		return ts, fmt.Errorf("missing raw hex data")
	}

	// decode hex to bytes
	if rl := hex.DecodedLen(len(raw_hex)); cap(s.raw) < rl {
		s.raw = make([]byte, rl)
	} else {
		s.raw = s.raw[:rl]
	}
	if n, err := hex.Decode(s.raw, raw_hex); err != nil {
		return ts, fmt.Errorf("invalid raw hex: %w", err)
	} else {
		s.raw = s.raw[:n]
	}

	// parse s.raw as a new msg
	P := s.P
	msg := P.GetMsg()
	switch n, err := msg.FromBytes(s.raw); {
	case err != nil:
		P.PutMsg(msg)
		return ts, fmt.Errorf("BGP parse error: %w", err)
	case n != len(s.raw):
		P.PutMsg(msg)
		return ts, fmt.Errorf("dangling bytes after BGP message: %d/%d", n, len(s.raw))
	}

	// add metadata
	msg.Time = ts
	tags := pipe.UseContext(msg).UseTags()
	tags["PEER_IP"] = peer_ip
	tags["PEER_AS"] = peer_asn
	tags["RIS_ID"] = id
	tags["RIS_HOST"] = host

	// write to pipe
	msg.CopyData()
	return ts, s.in.WriteMsg(msg)
}

func (s *RisLive) Stop() error {
	s.in.Close()
	return nil
}
