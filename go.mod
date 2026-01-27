module github.com/bgpfix/bgpipe

go 1.24.0

toolchain go1.24.3

require (
	github.com/bgp/stayrtr v0.6.3
	github.com/bgpfix/bgpfix v0.16.0
	github.com/buger/jsonparser v1.1.1
	github.com/dsnet/compress v0.0.2-0.20230904184137-39efe44ab707
	github.com/gorilla/websocket v1.5.3
	github.com/klauspost/compress v1.18.3
	github.com/knadh/koanf/providers/posflag v1.0.1
	github.com/knadh/koanf/v2 v2.3.2
	github.com/puzpuzpuz/xsync/v4 v4.4.0
	github.com/rs/zerolog v1.34.0
	github.com/spf13/pflag v1.0.10
	github.com/twmb/franz-go v1.20.6
	github.com/twmb/franz-go/pkg/kadm v1.17.2
	github.com/valyala/bytebufferpool v1.0.0
	golang.org/x/sys v0.40.0
	golang.org/x/time v0.14.0
)

require (
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/knadh/koanf/maps v0.1.2 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mitchellh/copystructure v1.2.0 // indirect
	github.com/mitchellh/reflectwalk v1.0.2 // indirect
	github.com/pierrec/lz4/v4 v4.1.25 // indirect
	github.com/twmb/franz-go/pkg/kmsg v1.12.0 // indirect
	golang.org/x/crypto v0.47.0 // indirect
	golang.org/x/sync v0.19.0 // indirect
)

// for dev: use the latest code in ../bgpfix
// replace github.com/bgpfix/bgpfix => ./.src/bgpfix
