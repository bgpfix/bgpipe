package extio

import (
	"bytes"
	"fmt"
	"io"
	"slices"
	"strconv"
	"time"

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

	opt_type   []msg.Type // --type
	opt_read   bool       // --read
	opt_write  bool       // --write
	opt_copy   bool       // --copy
	opt_seq    bool       // --seq
	opt_time   bool       // --time
	opt_raw    bool       // --raw
	opt_pardon bool       // --pardon

	buf bytes.Buffer // for ReadBuf()

	Callback *pipe.Callback // our callback for capturing bgpipe output
	InputL   *pipe.Input    // our L input to bgpipe
	InputR   *pipe.Input    // our R input to bgpipe
	InputD   *pipe.Input    // default input if data doesn't specify the direction

	Output chan *bytebufferpool.ByteBuffer // output ready to be sent to the process
	Pool   *bytebufferpool.Pool            // pool of byte buffers
}

// NewExtio creates a new object for given stage.
// mode: 1=read-only (to pipe); 2=write-only (from pipe)
func NewExtio(parent *core.StageBase, mode int) *Extio {
	eio := &Extio{
		StageBase: parent,
		Output:    make(chan *bytebufferpool.ByteBuffer, 100),
		Pool:      &bbpool,
	}

	// add CLI options iff needed
	f := eio.Options.Flags
	if f.Lookup("seq") == nil {
		f.Bool("raw", false, "speak raw BGP instead of JSON")
		f.StringSlice("type", []string{}, "skip if message is not of specified type(s)")

		f.Bool("read", false, "read-only mode (no output from bgpipe)")
		f.Bool("write", false, "write-only mode (no input to bgpipe)")

		if mode == 1 {
			read, write := f.Lookup("read"), f.Lookup("write")
			read.Hidden, write.Hidden = true, true
			read.Value.Set("true")
		} else {
			f.Bool("copy", false, "copy messages instead of filtering (mirror)")
		}

		if mode == 2 {
			read, write := f.Lookup("read"), f.Lookup("write")
			read.Hidden, write.Hidden = true, true
			write.Value.Set("true")
		} else {
			f.Bool("seq", false, "overwrite input message sequence number")
			f.Bool("time", false, "overwrite input message time")
			f.Bool("pardon", false, "ignore input parse errors")
		}
	}

	return eio
}

// Attach must be called from the parent stage attach
func (eio *Extio) Attach() error {
	p := eio.P
	k := eio.K

	// options
	eio.opt_copy = k.Bool("copy")
	eio.opt_read = k.Bool("read")
	eio.opt_write = k.Bool("write")
	eio.opt_seq = k.Bool("seq")
	eio.opt_time = k.Bool("time")
	eio.opt_raw = k.Bool("raw")
	eio.opt_pardon = k.Bool("pardon")

	// parse --type
	for _, v := range k.Strings("type") {
		// skip empty types
		if len(v) == 0 {
			continue
		}

		// canonical name?
		typ, err := msg.TypeString(v)
		if err == nil {
			eio.opt_type = append(eio.opt_type, typ)
			continue
		}

		// a plain integer?
		tnum, err2 := strconv.Atoi(v)
		if err2 == nil && tnum >= 0 && tnum <= 0xff {
			eio.opt_type = append(eio.opt_type, msg.Type(tnum))
			continue
		}

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

	// not write-only? read input to bgpipe
	if !eio.opt_write {
		if eio.IsBidir {
			eio.InputL = p.AddInput(msg.DIR_L)
			eio.InputR = p.AddInput(msg.DIR_R)
			if eio.IsLast {
				eio.InputD = eio.InputL
			} else {
				eio.InputD = eio.InputR
			}
		} else if eio.IsLeft {
			eio.InputL = p.AddInput(msg.DIR_L)
			eio.InputR = eio.InputL // redirect R messages to L
			eio.InputD = eio.InputL
		} else {
			eio.InputR = p.AddInput(msg.DIR_R)
			eio.InputL = eio.InputR // redirect L messages to R
			eio.InputD = eio.InputR
		}
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
func (eio *Extio) ReadSingle(buf []byte, cb pipe.CallbackFunc) (read_err error) {
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

	// raw message?
	m := eio.P.GetMsg()
	if eio.opt_raw { // raw message, must be exactly 1
		n, err := m.FromBytes(buf)
		switch {
		case err != nil:
			read_err = err // parse error
		case n != len(buf):
			read_err = ErrLength // dangling bytes after msg?
		default:
			m.CopyData() // can't keep reference to buf
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
			if m.Type == msg.INVALID {
				m.Use(msg.KEEPALIVE) // for convenience
			}
		case buf[0] == '{': // an UPDATE
			m.Use(msg.UPDATE)
			read_err = m.Update.FromJSON(buf)
		default:
			// TODO: add exabgp?
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
	switch m.Dir {
	case msg.DIR_L:
		return eio.InputL.WriteMsg(m)
	case msg.DIR_R:
		return eio.InputR.WriteMsg(m)
	default:
		return eio.InputD.WriteMsg(m)
	}
}

// ReadBuf reads all messages from the process, as bytes in buf, buffering if needed.
// Must not be used concurrently. cb may be nil.
func (eio *Extio) ReadBuf(buf []byte, cb pipe.CallbackFunc) (read_err error) {
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
			read_err = err
		}
	} else {
		// buffer
		if eio.buf.Len() < 1024*1024 {
			eio.buf.Write(buf)
		} else {
			return ErrLength
		}

		// parse all lines in buf so far
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
	var buf [64 * 1024]byte
	for {
		n, err := rd.Read(buf[:])
		if n > 0 {
			read_err = eio.ReadBuf(buf[:n], cb)
		}
		switch {
		case read_err != nil:
			return read_err
		case err == nil:
			continue
		case err == io.EOF:
			return nil
		default:
			return err
		}
	}
}

func (eio *Extio) checkMsg(m *msg.Msg) bool {
	// filter message types?
	if len(eio.opt_type) > 0 && slices.Index(eio.opt_type, m.Type) < 0 {
		return false
	}

	// overwrite message metadata?
	if eio.opt_seq {
		m.Seq = 0
	}
	if eio.opt_time {
		m.Time = time.Now().UTC()
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
		if err == nil {
			_, err = m.WriteTo(bb)
		}
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
