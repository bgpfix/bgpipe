package extio

import (
	"bytes"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/bgpfix/bgpfix/caps"
	"github.com/bgpfix/bgpfix/dir"
	"github.com/bgpfix/bgpfix/mrt"
	"github.com/bgpfix/bgpfix/msg"
	"github.com/bgpfix/bgpfix/pipe"
	"github.com/bgpfix/bgpipe/core"
	"github.com/valyala/bytebufferpool"
)

var bbpool bytebufferpool.Pool

// Extio helps in I/O with external processes eg. a background JSON filter,
// or a remote websocket processor.
// You must read Output and return disposed buffers using Put().
type Extio struct {
	*core.StageBase

	mode       Mode
	opt_type   []msg.Type // --type
	opt_raw    bool       // --raw
	opt_mrt    bool       // --mrt
	opt_read   bool       // --read
	opt_write  bool       // --write
	opt_copy   bool       // --copy
	opt_noseq  bool       // --no-seq
	opt_notime bool       // --no-time
	opt_notags bool       // --no-tags
	opt_pardon bool       // --pardon

	mrt *mrt.Reader  // MRT reader
	buf bytes.Buffer // for ReadBuf()

	Callback *pipe.Callback // our callback for capturing bgpipe output
	InputL   *pipe.Input    // our L input to bgpipe
	InputR   *pipe.Input    // our R input to bgpipe
	InputD   *pipe.Input    // default input if data doesn't specify the direction

	Output chan *bytebufferpool.ByteBuffer // output ready to be sent to the process
	Pool   *bytebufferpool.Pool            // pool of byte buffers
}

type Mode = int

const (
	MODE_DEFAULT      = 0
	MODE_READ    Mode = 0x01 // no output from bgpipe
	MODE_WRITE   Mode = 0x02 // no input to bgpipe
	MODE_COPY    Mode = 0x10 // copy from pipe, don't drop
)

// NewExtio creates a new object for given stage.
func NewExtio(parent *core.StageBase, mode Mode) *Extio {
	eio := &Extio{
		StageBase: parent,
		Output:    make(chan *bytebufferpool.ByteBuffer, 100),
		Pool:      &bbpool,
		mode:      mode,
	}

	// add CLI options iff needed
	f := eio.Options.Flags
	if f.Lookup("raw") == nil {
		f.Bool("raw", false, "speak raw BGP instead of JSON")
		f.Bool("mrt", false, "speak MRT-BGP4MP instead of JSON")
		f.StringSlice("type", []string{}, "skip if message is not of specified type(s)")

		if mode&(MODE_READ|MODE_WRITE) == 0 {
			f.Bool("read", false, "read-only mode (no output from bgpipe)")
			f.Bool("write", false, "write-only mode (no input to bgpipe)")
		}

		if mode&MODE_READ == 0 && mode&MODE_COPY == 0 {
			f.Bool("copy", false, "copy messages instead of filtering (mirror)")
		}

		if mode&MODE_WRITE == 0 {
			f.Bool("pardon", false, "ignore input parse errors")
			f.Bool("no-seq", false, "overwrite input message sequence number")
			f.Bool("no-time", false, "overwrite input message time")
			f.Bool("no-tags", false, "drop input message tags")
		}
	}

	return eio
}

// Attach must be called from the parent stage attach
func (eio *Extio) Attach() error {
	p := eio.P
	k := eio.K

	// options
	eio.opt_raw = k.Bool("raw")
	eio.opt_mrt = k.Bool("mrt")
	eio.opt_read = k.Bool("read")
	eio.opt_write = k.Bool("write")
	eio.opt_copy = k.Bool("copy")
	eio.opt_noseq = k.Bool("no-seq")
	eio.opt_notime = k.Bool("no-time")
	eio.opt_notags = k.Bool("no-tags")
	eio.opt_pardon = k.Bool("pardon")

	// overrides
	if eio.mode&MODE_READ != 0 {
		eio.opt_read = true
	}
	if eio.mode&MODE_WRITE != 0 {
		eio.opt_write = true
	}
	if eio.mode&MODE_COPY != 0 {
		eio.opt_copy = true
	}

	// parse --type
	var err error
	eio.opt_type, err = core.ParseTypes(k.Strings("type"), nil)
	if err != nil {
		return fmt.Errorf("--type: %w", err)
	}

	// check options
	if eio.opt_read || eio.opt_write {
		if eio.opt_read && eio.opt_write {
			return fmt.Errorf("--read and --write: must not use both at the same time")
		} else {
			eio.opt_copy = true // read/write-only doesn't make sense without --copy
		}
	}
	if eio.opt_raw && eio.opt_mrt {
		return fmt.Errorf("--raw and --mrt: must not use both at the same time")
	}

	// not write-only? read input to bgpipe
	if !eio.opt_write {
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
	}

	// not read-only? write bgpipe output
	if !eio.opt_read {
		eio.Callback = p.OnMsg(eio.SendMsg, eio.Dir, eio.opt_type...)
	}

	return nil
}

// ReadSingle reads single message from the process, as bytes in buf.
// Does not keep a reference to buf (copies buf if needed).
// Can be used concurrently. cb may be nil.
func (eio *Extio) ReadSingle(buf []byte, cb pipe.CallbackFunc) (parse_err error) {
	// write-only to process?
	if eio.opt_write {
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
			parse_err = err // parse error
		case n != len(buf):
			parse_err = ErrLength // dangling bytes after msg?
		}

	} else if eio.opt_mrt { // MRT message
		switch n, err := eio.mrt.FromBytes(buf, m, nil); {
		case err == mrt.ErrSub:
			eio.P.PutMsg(m)
			return nil // silent skip, BGP4MP but not a message
		case err != nil:
			parse_err = err // parse error
		case n != len(buf):
			parse_err = ErrLength // dangling bytes after msg?
		}

	} else { // parse text in buf into m
		buf = bytes.TrimSpace(buf)
		switch {
		case len(buf) == 0 || buf[0] == '#': // comment
			eio.P.PutMsg(m)
			return nil
		case buf[0] == '[': // a BGP message
			// TODO: optimize unmarshal (lookup cache of recently marshaled msgs)
			parse_err = m.FromJSON(buf)

			// convenience
			if parse_err == nil && m.Type == msg.INVALID {
				m.Use(msg.KEEPALIVE)
				m.Marshal(caps.Caps{}) // empty Data
			}
		case buf[0] == '{': // an UPDATE
			m.Use(msg.UPDATE)
			parse_err = m.Update.FromJSON(buf)
		default:
			// TODO: add exabgp?
			parse_err = ErrFormat
		}
	}

	// parse error?
	if parse_err != nil {
		if eio.opt_pardon {
			parse_err = nil
		} else if eio.opt_raw {
			eio.Err(parse_err).Hex("input", buf).Msg("input read single error")
		} else {
			eio.Err(parse_err).Bytes("input", buf).Msg("input read single error")
		}
		eio.P.PutMsg(m)
		return parse_err
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
func (eio *Extio) ReadBuf(buf []byte, cb pipe.CallbackFunc) (parse_err error) {
	// write-only to process?
	if eio.opt_write {
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
			parse_err = err
		}
	} else if eio.opt_mrt { // MRT message(s)
		_, err := eio.mrt.WriteFunc(buf, check)
		switch err {
		case nil:
			break // success
		case io.ErrUnexpectedEOF:
			return nil // wait for more
		default:
			parse_err = err
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
	if parse_err != nil && !eio.opt_pardon {
		eio.Err(parse_err).Msg("input read stream error")
		return parse_err
	}

	return nil
}

// ReadStream is a ReadBuf wrapper that reads from an io.Reader.
// Must not be used concurrently. cb may be nil.
func (eio *Extio) ReadStream(rd io.Reader, cb pipe.CallbackFunc) (parse_err error) {
	buf := make([]byte, 64*1024)
	for {
		// block on read, try parsing
		n, err := rd.Read(buf)
		if n > 0 {
			parse_err = eio.ReadBuf(buf[:n], cb)
		}

		// should stop here?
		switch {
		case parse_err != nil:
			return parse_err
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
	// filter message types?
	if len(eio.opt_type) > 0 && slices.Index(eio.opt_type, m.Type) < 0 {
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
		pipe.MsgContext(m).DropTags()
	}

	// take it
	return true
}

// SendMsg queues BGP message to the process. Can be used concurrently.
func (eio *Extio) SendMsg(m *msg.Msg) bool {
	// read-only from process?
	if eio.opt_read {
		return true
	}

	// filter the message?
	mx := pipe.MsgContext(m)
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
		mr := mrt.NewMrt().Use(mrt.BGP4MP_ET)

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
	default:
		_, err = bb.Write(m.GetJSON())
	}
	if err != nil {
		eio.Warn().Err(err).Msg("extio write error")
		return true
	}

	// try writing, don't panic on channel closed [1]
	if !send_safe(eio.Output, bb) {
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
	eio.opt_read = true
	eio.Callback.Drop()
	close_safe(eio.Output)
	return nil
}

// InputClose closes all stage inputs, stopping the flow from the process to bgpipe
func (eio *Extio) InputClose() error {
	eio.opt_write = true
	eio.InputL.Close()
	eio.InputR.Close()
	return nil
}
