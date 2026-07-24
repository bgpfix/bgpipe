package core

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	vmmetrics "github.com/VictoriaMetrics/metrics"
	"github.com/bgpfix/bgpfix/rpki"
	"github.com/bgpfix/bgpfix/rtr"
)

// Rpki maintains the bgpipe-wide RPKI cache, shared by stages (eg. rov and aspa).
// It is created on the first UseRpki call, and maintained by the bgpipe core:
// fed either from an RTR server, a URL, or a local file, as given in --rpki.
type Rpki struct {
	b        *Bgpipe
	Cache    *rpki.Cache // the shared RPKI cache
	source   string      // --rpki flag value
	WantASPA bool        // an aspa stage is attached (warn if the source cannot provide ASPA data)

	// RTR session state (accessed only from the RTR client goroutine)
	serial uint32 // last applied serial
	sessid uint16 // last applied session ID
	has    bool   // true once first EndOfData received

	// file/URL watcher state
	file_mod    time.Time
	data_hash   [32]byte
	url_etag    string
	url_lastmod string
}

// UseRpki returns the shared RPKI cache, creating it if needed.
// Stages should call it in Attach(); the bgpipe core will maintain the cache
// for the whole pipeline lifetime, per the global --rpki* flags.
func (b *Bgpipe) UseRpki() *rpki.Cache {
	if b.Rpki == nil {
		b.Rpki = &Rpki{
			b:     b,
			Cache: rpki.NewCache(&b.Logger),
		}
	}
	return b.Rpki.Cache
}

// Start starts maintaining the RPKI cache in background, iff it is in use.
// The source goroutines stop when the bgpipe context is cancelled.
func (r *Rpki) Start() error {
	if r == nil {
		return nil // not in use
	}

	b := r.b
	r.source = b.K.String("rpki")
	if r.source == "" {
		return fmt.Errorf("RPKI cache in use but --rpki is empty")
	}

	// cache metrics; NB: GetOrCreate to stay idempotent (eg. tests)
	vmmetrics.GetOrCreateGauge("bgpipe_rpki_vrps_ipv4", func() float64 {
		v4, _, _ := r.Cache.Sizes()
		return float64(v4)
	})
	vmmetrics.GetOrCreateGauge("bgpipe_rpki_vrps_ipv6", func() float64 {
		_, v6, _ := r.Cache.Sizes()
		return float64(v6)
	})
	vmmetrics.GetOrCreateGauge("bgpipe_rpki_aspa_entries", func() float64 {
		_, _, aspas := r.Cache.Sizes()
		return float64(aspas)
	})

	// an RTR server, a URL, or a local file?
	switch {
	case strings.HasPrefix(r.source, "tls://"):
		go r.rtrRun(strings.TrimPrefix(r.source, "tls://"), true)
	case strings.HasPrefix(r.source, "http://"), strings.HasPrefix(r.source, "https://"):
		// NB: fetch synchronously, so a broken URL is a fatal config error
		if err := r.urlFetch(); err != nil {
			return fmt.Errorf("could not fetch RPKI data: %w", err)
		}
		go r.poll(b.K.Duration("rpki-refresh"), r.urlFetch)
	default:
		// NB: classify syntactically: a valid host:port means an RTR server,
		// so that a mistyped file path fails fast instead of dialing it
		if _, _, err := net.SplitHostPort(r.source); err == nil {
			go r.rtrRun(r.source, false)
		} else {
			// NB: load synchronously, so a broken file is a fatal config error
			if err := r.fileLoad(); err != nil {
				return fmt.Errorf("could not load RPKI data file: %w", err)
			}
			go r.poll(10*time.Second, r.fileLoad)
		}
	}

	return nil
}

// poll re-runs load every interval, until the bgpipe context is done.
func (r *Rpki) poll(interval time.Duration, load func() error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := load(); err != nil {
				r.b.Warn().Err(err).Str("rpki", r.source).Msg("failed to re-load RPKI data, keeping old")
			}
		case <-r.b.Ctx.Done():
			return
		}
	}
}

// applyData parses and publishes VRP/ASPA data, unless it is unchanged.
func (r *Rpki) applyData(data []byte) error {
	hash := sha256.Sum256(data)
	if hash == r.data_hash {
		r.b.Debug().Str("rpki", r.source).Msg("RPKI data unchanged")
		return nil
	}

	cache := r.Cache
	cache.Flush()
	if err := cache.Parse(data); err != nil {
		return err
	}
	cache.Apply()

	r.data_hash = hash
	return nil
}

// rtrRun manages the RTR client connection loop with reconnection.
func (r *Rpki) rtrRun(addr string, usetls bool) {
	b := r.b
	cache := r.Cache

	var client *rtr.Client
	aspa_warned := false // warn once per connection

	client = rtr.NewClient(&rtr.Options{
		Logger:  &b.Logger,
		Version: rtr.VersionAuto,

		OnROA:  cache.AddVRP,
		OnASPA: cache.AddASPA,

		OnEndOfData: func(sessid uint16, serial uint32) {
			changed := !r.has || r.serial != serial || r.sessid != sessid
			r.serial, r.sessid, r.has = serial, sessid, true
			if changed {
				cache.Apply()
			}

			// NB: pre-v2 servers can not provide ASPA data at all
			if v := client.Version(); r.WantASPA && !aspa_warned && v < rtr.VersionV2 {
				aspa_warned = true
				b.Warn().Str("rpki", addr).Msgf("ASPA validation enabled, but the RTR server negotiated protocol v%d (ASPA needs v2): no ASPA data", v)
			}
		},

		OnCacheReset: func() {
			cache.Flush()
			r.has = false
		},

		OnError: func(code uint16, text string) {
			if code != rtr.ErrNoData {
				b.Warn().Uint16("code", code).Str("text", text).Msg("RTR error")
			} else {
				b.Debug().Msg("RTR no data available yet")
			}
		},
	})

	// periodic refresh; NB: SendSerial is a no-op until the first EndOfData
	go func() {
		ticker := time.NewTicker(b.K.Duration("rpki-refresh"))
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if client.SendSerial() {
					b.Debug().Msg("RTR periodic refresh")
				}
			case <-b.Ctx.Done():
				return
			}
		}
	}()

	for b.Ctx.Err() == nil {
		retry := time.Now().Add(b.K.Duration("rpki-retry"))

		conn, err := r.rtrDial(addr, usetls)
		if err == nil {
			cache.Flush()
			r.has = false
			aspa_warned = false
			err = client.Run(b.Ctx, conn) // NB: Run always closes conn
		}
		if b.Ctx.Err() != nil {
			return
		}

		// version negotiation: reconnect immediately with the lower version
		if errors.Is(err, rtr.ErrDowngrade) {
			continue
		}

		if sleep := time.Until(retry); sleep > time.Second {
			b.Warn().Err(err).Str("rpki", addr).Msgf("RTR connection failed, retrying in %s", sleep.Round(time.Second))
			select {
			case <-time.After(sleep):
			case <-b.Ctx.Done():
			}
		} else {
			b.Warn().Err(err).Str("rpki", addr).Msg("RTR connection failed, retrying now")
		}
	}
}

// rtrDial connects to the RTR server at addr, with a 15s timeout.
// NB: can't use util.DialRetry here (import cycle); the outer rtrRun loop
// handles retries anyway.
func (r *Rpki) rtrDial(addr string, usetls bool) (net.Conn, error) {
	b := r.b
	b.Info().Str("rpki", addr).Bool("tls", usetls).Msg("connecting to the RTR server")

	ctx, cancel := context.WithTimeout(b.Ctx, 15*time.Second)
	defer cancel()

	if usetls {
		dialer := &tls.Dialer{
			Config: &tls.Config{
				InsecureSkipVerify: b.K.Bool("rpki-insecure"),
			},
		}
		return dialer.DialContext(ctx, "tcp", addr)
	}

	var dialer net.Dialer
	return dialer.DialContext(ctx, "tcp", addr)
}

// urlFetch downloads and parses VRP/ASPA data from the URL, unless unchanged.
func (r *Rpki) urlFetch() error {
	b := r.b
	b.Info().Str("rpki", r.source).Msg("fetching RPKI data")

	ctx, cancel := context.WithTimeout(b.Ctx, 5*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.source, nil)
	if err != nil {
		return err
	}

	// conditional GET: skip the download if the server says it is unchanged
	if r.url_etag != "" {
		req.Header.Set("If-None-Match", r.url_etag)
	}
	if r.url_lastmod != "" {
		req.Header.Set("If-Modified-Since", r.url_lastmod)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		b.Debug().Str("rpki", r.source).Msg("RPKI data not modified")
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP status %s", resp.Status)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if err := r.applyData(data); err != nil {
		return err
	}

	r.url_etag = resp.Header.Get("Etag")
	r.url_lastmod = resp.Header.Get("Last-Modified")
	return nil
}

// fileLoad loads VRP/ASPA data from the file, unless already loaded.
func (r *Rpki) fileLoad() error {
	fi, err := os.Stat(r.source)
	if err != nil {
		return err
	}
	if !fi.ModTime().After(r.file_mod) {
		return nil
	}

	data, err := os.ReadFile(r.source)
	if err != nil {
		return err
	}
	if err := r.applyData(data); err != nil {
		return err
	}

	r.file_mod = fi.ModTime()
	return nil
}
