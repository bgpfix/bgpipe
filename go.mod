module github.com/bgpfix/bgpipe

go 1.26.0

// NB: Go listens with MPTCP by default on Linux, but TCP-MD5, TTL and transparent mode are per-socket TCP options
godebug multipathtcp=0

require (
	github.com/VictoriaMetrics/metrics v1.44.1
	github.com/bgpfix/bgpfix v0.23.0
	github.com/dsnet/compress v0.0.2-0.20230904184137-39efe44ab707
	github.com/go-chi/chi/v5 v5.5.0
	github.com/gorilla/websocket v1.5.3
	github.com/klauspost/compress v1.20.3
	github.com/knadh/koanf/providers/posflag v1.0.2
	github.com/knadh/koanf/v2 v2.4.1
	github.com/puzpuzpuz/xsync/v4 v4.5.0
	github.com/rs/zerolog v1.35.1
	github.com/spf13/pflag v1.0.10
	github.com/stretchr/testify v1.12.1
	github.com/twmb/franz-go v1.22.2
	github.com/twmb/franz-go/pkg/kadm v1.19.0
	github.com/valyala/bytebufferpool v1.0.0
	golang.org/x/sys v0.49.0
	golang.org/x/time v0.16.0
)

require (
	github.com/buger/jsonparser v1.6.1 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/itlightning/dateparse v0.2.1 // indirect
	github.com/knadh/koanf/maps v0.1.3 // indirect
	github.com/mattn/go-colorable v0.1.16 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/mitchellh/copystructure v1.2.0 // indirect
	github.com/mitchellh/reflectwalk v1.0.2 // indirect
	github.com/pierrec/lz4/v4 v4.1.33 // indirect
	github.com/twmb/franz-go/pkg/kmsg v1.15.0 // indirect
	github.com/valyala/fastrand v1.1.0 // indirect
	github.com/valyala/histogram v1.2.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
)

// for dev: use the latest code in ../bgpfix
// replace github.com/bgpfix/bgpfix => ../bgpfix
