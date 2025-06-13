module github.com/bgpfix/bgpipe

go 1.24

toolchain go1.24.3

require (
	github.com/bgpfix/bgpfix v0.0.0-00010101000000-000000000000
	github.com/gorilla/websocket v1.5.3
	github.com/knadh/koanf/providers/posflag v1.0.1
	github.com/knadh/koanf/v2 v2.2.1
	github.com/puzpuzpuz/xsync/v4 v4.1.0
	github.com/rs/zerolog v1.34.0
	github.com/spf13/pflag v1.0.6
	github.com/valyala/bytebufferpool v1.0.0
	golang.org/x/sys v0.33.0
	github.com/klauspost/compress v1.18.0
)

require (
	github.com/buger/jsonparser v1.1.1 // indirect
	github.com/go-viper/mapstructure/v2 v2.2.1 // indirect
	github.com/knadh/koanf/maps v0.1.2 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mitchellh/copystructure v1.2.0 // indirect
	github.com/mitchellh/reflectwalk v1.0.2 // indirect
)

// for dev: use the latest code in ../bgpfix
replace github.com/bgpfix/bgpfix => ../bgpfix
