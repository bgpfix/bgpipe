module github.com/bgpfix/bgpipe

go 1.25.0

toolchain go1.26.0

require (
	github.com/VictoriaMetrics/metrics v1.44.0
	github.com/bgpfix/bgpfix v0.20.3-0.20260715104035-a20950673c0f
	github.com/buger/jsonparser v1.2.0
	github.com/dsnet/compress v0.0.2-0.20230904184137-39efe44ab707
	github.com/go-chi/chi/v5 v5.3.1
	github.com/gorilla/websocket v1.5.3
	github.com/klauspost/compress v1.19.0
	github.com/knadh/koanf/providers/posflag v1.0.1
	github.com/knadh/koanf/v2 v2.3.5
	github.com/puzpuzpuz/xsync/v4 v4.5.0
	github.com/rs/zerolog v1.35.1
	github.com/spf13/pflag v1.0.10
	github.com/stretchr/testify v1.11.1
	github.com/twmb/franz-go v1.21.5
	github.com/twmb/franz-go/pkg/kadm v1.18.0
	github.com/valyala/bytebufferpool v1.0.0
	golang.org/x/sys v0.47.0
	golang.org/x/time v0.15.0
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/itlightning/dateparse v0.2.1 // indirect
	github.com/knadh/koanf/maps v0.1.2 // indirect
	github.com/mattn/go-colorable v0.1.15 // indirect
	github.com/mattn/go-isatty v0.0.22 // indirect
	github.com/mitchellh/copystructure v1.2.0 // indirect
	github.com/mitchellh/reflectwalk v1.0.2 // indirect
	github.com/pierrec/lz4/v4 v4.1.27 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/twmb/franz-go/pkg/kmsg v1.13.1 // indirect
	github.com/valyala/fastrand v1.1.0 // indirect
	github.com/valyala/histogram v1.2.0 // indirect
	golang.org/x/crypto v0.54.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

// for dev: use the latest code in ../bgpfix
// replace github.com/bgpfix/bgpfix => ../bgpfix
