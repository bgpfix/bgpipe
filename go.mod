module github.com/bgpfix/bgpipe

go 1.21

require (
	github.com/bgpfix/bgpfix v0.0.0-00010101000000-000000000000
	github.com/knadh/koanf/providers/posflag v0.1.0
	github.com/knadh/koanf/v2 v2.1.0
	github.com/puzpuzpuz/xsync/v3 v3.1.0
	github.com/rs/zerolog v1.32.0
	github.com/spf13/pflag v1.0.5
	github.com/valyala/bytebufferpool v1.0.0
	golang.org/x/sys v0.17.0
	github.com/gorilla/websocket v1.5.1
)

require (
	github.com/buger/jsonparser v1.1.1 // indirect
	github.com/go-viper/mapstructure/v2 v2.0.0-alpha.1 // indirect
	github.com/klauspost/cpuid/v2 v2.2.7 // indirect
	github.com/knadh/koanf/maps v0.1.1 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mitchellh/copystructure v1.2.0 // indirect
	github.com/mitchellh/reflectwalk v1.0.2 // indirect
	github.com/zeebo/xxh3 v1.0.2 // indirect
	golang.org/x/net v0.21.0 // indirect
)

replace github.com/bgpfix/bgpfix => ../bgpfix
