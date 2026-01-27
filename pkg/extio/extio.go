package extio

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/bgpfix/bgpfix/bmp"
	"github.com/bgpfix/bgpfix/dir"
	"github.com/bgpfix/bgpfix/exa"
	"github.com/bgpfix/bgpfix/mrt"
	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpipe/core"
	"github.com/bgpfix/bgpipe/pkg/util"
	"github.com/valyala/bytebufferpool"
)

var bbpool bytebufferpool.Pool

// Extio helps in I/O with external processes eg. a background JSON filter,
// or a remote websocket processor.
// You must read Output and return disposed buffers using Put().
type Extio struct {
	*core.StageBase

	mode     Mode // extio mode of operation
	Detected bool // auto-detected the data format?

	opt_type   map[msg.Type]bool // --type
	opt_skip   map[msg.Type]bool // --skip
	opt_raw    bool              // --format=raw
	opt_mrt    bool              // --format=mrt
	opt_exa    bool              // --format=exa
	opt_bmp    bool              // --format=bmp
	opt_obmp   bool              // --format=openbmp
	opt_copy   bool              // --copy
	opt_noseq  bool              // --no-seq
	opt_notime bool              // --no-time
	opt_notags bool              // --no-tags
	opt_pardon bool              // --pardon

	mrt *mrt.Reader  // MRT reader
	bmp *bmp.Reader  // BMP reader
	buf bytes.Buffer // for ReadBuf()

	Callback   *pipe.Callback // our callback for capturing bgpipe output
	InputL     *pipe.Input    // our L input to bgpipe
	InputR     *pipe.Input    // our R input to bgpipe
	InputD     *pipe.Input    // default input if data doesn't specify the direction
	drop_input atomic.Bool    // if true, drop all input from the process

	Output chan *bytebufferpool.ByteBuffer // output ready to be sent to the process
	Pool   *bytebufferpool.Pool            // pool of byte buffers
}

type Mode = int

const (
	MODE_DEFAULT      = 0    // accept both input and output to/from bgpipe
	MODE_READ    Mode = 0x01 // no output from bgpipe
	MODE_WRITE   Mode = 0x02 // no input to bgpipe
	MODE_COPY    Mode = 0x10 // copy from pipe (mirror), don't consume the messages
)

// NewExtio creates a new object for given stage.
func NewExtio(parent *core.StageBase, mode Mode, autodetect bool) *Extio {
	eio := &Extio{
		StageBase: parent,
		Output:    make(chan *bytebufferpool.ByteBuffer, 100),
		Pool:      &bbpool,
		mode:      mode,
	}

	// add CLI options iff needed
	f := eio.Options.Flags
	if f.Lookup("format") == nil {
		if autodetect {
			f.String("format", "auto", "data format (json/raw/mrt/exa/bmp/obmp/auto)")
		} else {
			f.String("format", "json", "data format (json/raw/mrt/exa/bmp/obmp)")
		}
		f.StringSlice("type", []string{}, "skip messages NOT of specified type(s)")
		f.StringSlice("skip", []string{}, "skip messages of specified type(s)")

		if mode&(MODE_READ|MODE_WRITE) == 0 {
			f.Bool("read", false, "read-only mode (no output from bgpipe)")
			f.Bool("write", false, "write-only mode (no input to bgpipe)")
		}

		if mode&MODE_READ == 0 && mode&MODE_COPY == 0 {
			f.Bool("copy", false, "copy messages instead of filtering (mirror)")
		}

		if mode&MODE_WRITE == 0 {
			f.Bool("pardon", false, "ignore input errors")
			f.Bool("no-seq", false, "overwrite input message sequence number")
			f.Bool("no-time", false, "overwrite input message time")
			f.Bool("no-tags", false, "drop input message tags")
		}
	}

	return eio
}

// DetectNeeded returns true iff data format detection is (still) needed
func (eio *Extio) DetectNeeded() bool {
	return !eio.Detected && eio.K.String("format") == "auto"
}

// DetectPath tries to detect data format from given path.
// Returns true if format was successfully detected.
func (eio *Extio) DetectPath(path string) (success bool) {
	defer func() {
		if success {
			eio.Detected = true
		}
	}()

	fname := strings.ToLower(filepath.Base(path))

	// looks like RouteViews / RIPE RIS MRT dumps?
	if strings.HasPrefix(fname, "updates.2") || fname == "latest-update.gz" {
		eio.opt_mrt = true
		return true
	}

	// drop compression suffixes
	switch ext := filepath.Ext(fname); ext {
	case ".gz", ".zstd", ".zst", ".bz2":
		fname = strings.TrimSuffix(fname, ext)
	}

	// detect by extension
	switch filepath.Ext(fname) {
	case ".mrt":
		eio.opt_mrt = true
		return true
	case ".exa":
		eio.opt_exa = true
		return true
	case ".bmp":
		eio.opt_bmp = true
		return true
	case ".obmp":
		eio.opt_bmp = true
		eio.opt_obmp = true
		return true
	case ".json", ".jsonl", ".txt":
		// default JSON
		return true
	case ".raw", ".bin", ".bgp":
		eio.opt_raw = true
		return true
	}

	return false
}

// DetectSample tries to detect data format by peeking at the buffered reader.
// Returns true if format was successfully detected.
func (eio *Extio) DetectSample(br *bufio.Reader) (success bool) {
	buf, err := br.Peek(1)
	if err != nil {
		return false
	}
	defer func() {
		if success {
			eio.Detected = true
		}
	}()

	// looks like JSON?
	if buf[0] == '[' || buf[0] == '{' {
		// default JSON
		return true
	}

	// looks like exabgp?
	buf, err = br.Peek(8)
	if err != nil {
		return false
	} else if exa.IsExaBytes(buf) {
		eio.opt_exa = true
		return true
	}

	// looks like raw BGP?
	buf, err = br.Peek(16)
	if err != nil {
		return false
	} else if bytes.HasPrefix(buf, msg.BgpMarker) {
		eio.opt_raw = true
		return true
	}

	// looks like OpenBMP? (starts with "OBMP")
	buf, err = br.Peek(4)
	if err != nil {
		return false
	} else if string(buf) == bmp.OPENBMP_MAGIC {
		eio.opt_bmp = true
		eio.opt_obmp = true
		return true
	}

	// looks like raw BMP? (version 3, check common header pattern)
	buf, err = br.Peek(6)
	if err != nil {
		return false
	} else if buf[0] == bmp.VERSION && buf[5] <= 6 { // version 3, valid msg type
		eio.opt_bmp = true
		return true
	}

	// wild shot: looks like MRT BGP4MP_MESSAGE?
	// peek max. size for MRT ET + BGP4MP AS4 IPv6 + BGP marker
	buf, err = br.Peek(mrt.HEADLEN + 4 + 3*4 + 16*2 + msg.MARKLEN)
	if err != nil {
		return false
	} else if bytes.Contains(buf, msg.BgpMarker) {
		eio.opt_mrt = true
		return true
	}

	return false
}

// Attach must be called from the parent stage attach
func (eio *Extio) Attach() error {
	p := eio.P
	k := eio.K

	// options
	opt_read := k.Bool("read")
	opt_write := k.Bool("write")
	eio.opt_copy = k.Bool("copy")
	eio.opt_noseq = k.Bool("no-seq")
	eio.opt_notime = k.Bool("no-time")
	eio.opt_notags = k.Bool("no-tags")
	eio.opt_pardon = k.Bool("pardon")

	// data format
	switch strings.ToLower(k.String("format")) {
	case "json":
		// default
	case "raw":
		eio.opt_raw = true
	case "mrt":
		eio.opt_mrt = true
	case "exa":
		eio.opt_exa = true
	case "bmp":
		eio.opt_bmp = true
	case "obmp":
		eio.opt_bmp = true
		eio.opt_obmp = true
	case "auto":
		// handled elsewhere
	default:
		return fmt.Errorf("invalid --format '%s'", k.String("format"))
	}

	// overrides
	if eio.mode&MODE_READ != 0 {
		opt_read = true
	}
	if eio.mode&MODE_WRITE != 0 {
		opt_write = true
	}
	if eio.mode&MODE_COPY != 0 {
		eio.opt_copy = true
	}

	// parse --type and --skip
	if types, err := core.ParseTypes(k.Strings("type"), nil); err != nil {
		return fmt.Errorf("--type: %w", err)
	} else if len(types) > 0 {
		eio.opt_type = make(map[msg.Type]bool, len(types))
		for _, t := range types {
			eio.opt_type[t] = true
		}
	}
	if types, err := core.ParseTypes(k.Strings("skip"), nil); err != nil {
		return fmt.Errorf("--skip: %w", err)
	} else if len(types) > 0 {
		eio.opt_skip = make(map[msg.Type]bool, len(types))
		for _, t := range types {
			eio.opt_skip[t] = true
		}
	}
	for t := range eio.opt_skip {
		delete(eio.opt_type, t)
	}

	// check options
	if opt_read || opt_write {
		if opt_read && opt_write {
			return fmt.Errorf("--read and --write: must not use both at the same time")
		} else {
			eio.opt_copy = true // read/write-only doesn't make sense without --copy
		}
	}

	// not write-only? produce input to bgpipe
	if !opt_write {
		if eio.IsBidir {
			eio.InputL = p.AddInput(dir.DIR_L)
			eio.InputR = p.AddInput(dir.DIR_R)
			if eio.IsLast {
				eio.InputD = eio.InputL
			} else {
				eio.InputD = eio.InputR
			}
		} else if eio.IsLeft {
			eio.InputL = p.AddInput(dir.DIR_L)
			eio.InputR = eio.InputL // redirect R messages to L
			eio.InputD = eio.InputL
		} else {
			eio.InputR = p.AddInput(dir.DIR_R)
			eio.InputL = eio.InputR // redirect L messages to R
			eio.InputD = eio.InputR
		}

		eio.mrt = mrt.NewReader(p, eio.InputD)
		eio.mrt.NoTags = eio.opt_notags
		eio.bmp = bmp.NewReader(p, eio.InputD)
		eio.bmp.NoTags = eio.opt_notags
		eio.bmp.OpenBMP = eio.opt_obmp
	} else {
		eio.Options.IsProducer = false
	}

	// not read-only? capture bgpipe output to eio.Output
	if !opt_read {
		eio.Callback = p.OnMsg(eio.SendMsg, eio.Dir)

		// override capture direction?
		cb := eio.Callback
		if eio.IsLast {
			cb.Dir = dir.DIR_R
		} else if eio.IsFirst {
			cb.Dir = dir.DIR_L
		}

		// override message types?
		for t := range eio.opt_type {
			cb.Types = append(cb.Types, t)
		}
	}

	return nil
}

// ReadSingle reads single message from the process, as bytes in buf.
// Does not keep a reference to buf (copies buf if needed).
// Can be used concurrently. cb may be nil.
func (eio *Extio) ReadSingle(buf []byte, cb pipe.CallbackFunc) (read_err error) {
	// drop on the floor?
	if eio.drop_input.Load() {
		return nil
	}

	// use callback?
	check := eio.checkMsg
	if cb != nil {
		check = func(m *msg.Msg) bool {
			return eio.checkMsg(m) && cb(m)
		}
	}

	// parse
	m := eio.P.GetMsg()
	if eio.opt_raw { // raw message
		switch n, err := m.FromBytes(buf); {
		case err != nil:
			read_err = err // parse error
		case n != len(buf):
			read_err = ErrLength // dangling bytes after msg?
		}

	} else if eio.opt_mrt { // MRT message
		switch n, err := eio.mrt.FromBytes(buf, m, nil); {
		case err == mrt.ErrSub:
			eio.P.PutMsg(m)
			return nil // silent skip, BGP4MP but not a message
		case err != nil:
			read_err = err // parse error
		case n != len(buf):
			read_err = ErrLength // dangling bytes after msg?
		}

	} else if eio.opt_bmp { // BMP message
		eio.bmp.OpenBMP = eio.opt_obmp
		switch n, err := eio.bmp.FromBytes(buf, m); {
		case err == bmp.ErrNotRouteMonitoring:
			eio.P.PutMsg(m)
			return nil // silent skip, not a Route Monitoring message
		case err != nil:
			read_err = err // parse error
		case n != len(buf):
			read_err = ErrLength // dangling bytes after msg?
		}

	} else { // parse text in buf into m
		buf = bytes.TrimSpace(buf)
		switch {
		case len(buf) == 0 || buf[0] == '#': // comment
			eio.P.PutMsg(m)
			return nil
		case buf[0] == '[': // a BGP message
			// TODO: optimize unmarshal (lookup cache of recently marshaled msgs)
			read_err = m.FromJSON(buf)

			// convenience
			if read_err == nil && m.Type == msg.INVALID {
				m.Switch(msg.KEEPALIVE)
				// m.Marshal(caps.Caps{}) // empty Data TODO: needed?
			}
		case buf[0] == '{': // an UPDATE
			read_err = m.Switch(msg.UPDATE).Update.FromJSON(buf)
		case eio.opt_exa && exa.IsExaBytes(buf): // exabgp
			x, err := exa.NewExaLine(string(buf))
			if err != nil {
				read_err = fmt.Errorf("exa: %w", err)
				break
			}
			read_err = x.ToMsg(m)

		default:
			read_err = ErrFormat
		}
	}

	// parse error?
	if read_err != nil {
		if eio.opt_pardon {
			read_err = nil
		} else if eio.opt_raw {
			eio.Err(read_err).Hex("input", buf).Msg("input read single error")
		} else {
			eio.Err(read_err).Bytes("input", buf).Msg("input read single error")
		}
		eio.P.PutMsg(m)
		return read_err
	}

	// pre-process
	if !check(m) {
		eio.P.PutMsg(m)
		return nil
	}

	// sail!
	m.CopyData()
	switch m.Dir {
	case dir.DIR_L:
		return eio.InputL.WriteMsg(m)
	case dir.DIR_R:
		return eio.InputR.WriteMsg(m)
	default:
		return eio.InputD.WriteMsg(m)
	}
}

// ReadBuf reads all messages from the process, as bytes in buf, buffering if needed.
// Must not be used concurrently. cb may be nil.
func (eio *Extio) ReadBuf(buf []byte, cb pipe.CallbackFunc) (read_err error) {
	// drop on the floor?
	if eio.drop_input.Load() {
		return nil
	}

	check := eio.checkMsg
	if cb != nil {
		check = func(m *msg.Msg) bool {
			return eio.checkMsg(m) && cb(m)
		}
	}

	// raw message?
	if eio.opt_raw { // raw message(s)
		_, err := eio.InputD.WriteFunc(buf, check)
		switch err {
		case nil:
			break // success
		case io.ErrUnexpectedEOF:
			return nil // wait for more
		default:
			read_err = err
		}
	} else if eio.opt_mrt { // MRT message(s)
		_, err := eio.mrt.WriteFunc(buf, check)
		switch err {
		case nil:
			break // success
		case io.ErrUnexpectedEOF:
			return nil // wait for more
		default:
			read_err = err
		}
	} else if eio.opt_bmp { // BMP message(s)
		eio.bmp.OpenBMP = eio.opt_obmp
		_, err := eio.bmp.WriteFunc(buf, check)
		switch err {
		case nil:
			break // success
		case bmp.ErrShort:
			return nil // wait for more
		default:
			read_err = err
		}
	} else { // buffer and parse all lines in buf so far
		eio.buf.Write(buf)
		for {
			i := bytes.IndexByte(eio.buf.Bytes(), '\n')
			if i < 0 {
				break
			}
			err := eio.ReadSingle(eio.buf.Next(i+1), cb)
			if err != nil {
				return err
			}
		}
	}

	// parse error?
	if read_err != nil && !eio.opt_pardon {
		eio.Err(read_err).Msg("input read stream error")
		return read_err
	}

	return nil
}

// ReadStream is a ReadBuf wrapper that reads from an io.Reader.
// Must not be used concurrently. cb may be nil.
func (eio *Extio) ReadStream(rd io.Reader, cb pipe.CallbackFunc) (read_err error) {
	buf := make([]byte, 64*1024)
	for {
		// block on read, try parsing
		n, err := rd.Read(buf)
		if n > 0 {
			read_err = eio.ReadBuf(buf[:n], cb)
		}

		// should stop here?
		switch {
		case read_err != nil:
			return read_err
		case err == io.EOF:
			return nil
		case err != nil:
			return err
		}

		// grow buffer?
		if l := len(buf); n > l/2 && l <= 4*1024*1024 {
			buf = make([]byte, l*2)
		}
	}
}

func (eio *Extio) checkMsg(m *msg.Msg) bool {
	// filter message type?
	if eio.opt_type != nil && !eio.opt_type[m.Type] {
		return false
	}
	if eio.opt_skip != nil && eio.opt_skip[m.Type] {
		return false
	}

	// overwrite message metadata?
	if eio.opt_noseq {
		m.Seq = 0
	}
	if eio.opt_notime {
		m.Time = time.Now().UTC()
	}
	if eio.opt_notags {
		pipe.UseContext(m).DropTags()
	}

	// take it
	return true
}

// SendMsg queues BGP message to the process. Can be used concurrently.
func (eio *Extio) SendMsg(m *msg.Msg) bool {
	// should skip message type? (NB: --type already handled by the callback manager)
	if eio.opt_skip != nil && eio.opt_skip[m.Type] {
		return true
	}

	// filter the message?
	mx := pipe.UseContext(m)
	if !eio.opt_copy {
		// TODO: if borrow not set already, add the flag and keep m for later re-use (if enabled)
		//       NB: in such a case, we won't be able to re-use m easily?
		mx.Action.Drop()
	}

	// copy to a bytes buffer
	var err error
	bb := eio.Pool.Get()
	switch {
	case eio.opt_raw:
		err = m.Marshal(eio.P.Caps)
		if err != nil {
			break
		}
		_, err = m.WriteTo(bb)
	case eio.opt_mrt:
		mr := mrt.NewMrt().Switch(mrt.BGP4MP_ET)

		// marshal into BGP4MP
		err = mr.Bgp4.FromMsg(m)
		if err != nil {
			break
		}

		// marshal into MRT
		err = mr.Marshal()
		if err != nil {
			break
		}

		_, err = mr.WriteTo(bb)
	case eio.opt_exa:
		x := exa.NewExa()
		for range x.IterMsg(m) {
			bb.WriteString(x.String() + "\n") // NB: no error possible
		}

	default:
		bb.Write(m.GetJSON()) // NB: no error possible
	}
	if err != nil {
		eio.Warn().Err(err).Msg("extio write error")
		return true
	}

	// try writing, don't panic on channel closed [1]
	if !util.Send(eio.Output, bb) {
		mx.Callback.Drop()
		return true
	}

	return true
}

// WriteStream rewrites eio.Output to w.
func (eio *Extio) WriteStream(w io.Writer) error {
	for bb := range eio.Output {
		_, err := bb.WriteTo(w)
		eio.Pool.Put(bb)
		if err != nil {
			eio.OutputClose()
			return err
		}
	}
	return nil
}

// Put puts a byte buffer back to pool
func (eio *Extio) Put(bb *bytebufferpool.ByteBuffer) {
	if bb != nil {
		eio.Pool.Put(bb)
	}
}

// OutputClose closes eio.Output, stopping the flow from bgpipe to the process
func (eio *Extio) OutputClose() error {
	eio.Callback.Drop()
	util.Close(eio.Output)
	return nil
}

// InputClose closes all stage inputs, stopping the flow from the process to bgpipe
func (eio *Extio) InputClose() error {
	eio.drop_input.Store(true)
	eio.InputL.Close()
	eio.InputR.Close()
	return nil
}
