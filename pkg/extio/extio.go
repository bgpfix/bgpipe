package extio

import (
	"bytes"
	"errors"
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

	opt_copy  bool // copy
	opt_seq   bool // no-seq
	opt_time  bool // no-time
	opt_read  bool // no-read
	opt_write bool // no-write

	callback *pipe.Callback // our Write callback
	inputL   *pipe.Proc     // L input for data coming from the process
	inputR   *pipe.Proc     // R input for data coming from the process
	inputD   *pipe.Proc     // input if data doesn't specify the direction

	Output chan *bytebufferpool.ByteBuffer // output ready to be sent to the process
	Pool   *bytebufferpool.Pool            // pool of byte buffers
}

// NewExtio creates a new object for given stage. If out is not defined, creates a new chan(100).
func NewExtio(parent *core.StageBase, out ...chan *bytebufferpool.ByteBuffer) *Extio {
	var output chan *bytebufferpool.ByteBuffer
	if len(out) > 0 && out[0] != nil {
		output = out[0]
	} else {
		output = make(chan *bytebufferpool.ByteBuffer, 100)
	}

	eio := &Extio{
		StageBase: parent,
		Output:    output,
		Pool:      &bbpool,
	}

	// add CLI options iff needed
	f := eio.Options.Flags
	if f.Lookup("copy") == nil {
		f.Bool("copy", false, "copy (mirror) instead of filtering")
		f.Bool("no-write", false, "do not write messages at all")
		f.Bool("no-read", false, "do not read messages at all")
		f.Bool("no-seq", false, "ignore input sequence number")
		f.Bool("no-time", false, "ignore input message time")
	}

	return eio
}

// Attach must be called from the parent stage attach
func (eio *Extio) Attach() error {
	p := eio.P
	k := eio.K

	// options
	eio.opt_copy = k.Bool("copy")
	eio.opt_seq = k.Bool("no-seq")
	eio.opt_time = k.Bool("no-time")
	eio.opt_read = k.Bool("no-read")
	eio.opt_write = k.Bool("no-write")

	// unmarshal to pipe inputs
	if !eio.opt_read {
		// NB: at least one of IsLeft / IsRight is true
		if eio.IsLeft && eio.IsRight {
			eio.inputL = p.AddProc(msg.DIR_L)
			eio.inputR = p.AddProc(msg.DIR_R)

			if eio.B.StageCount() == 1 {
				eio.inputD = eio.inputL
			} else {
				eio.inputD = eio.inputR
			}
		} else if eio.IsLeft {
			eio.inputL = p.AddProc(msg.DIR_L)
			eio.inputR = eio.inputL // redirect R messages to L
			eio.inputD = eio.inputL
		} else {
			eio.inputR = p.AddProc(msg.DIR_R)
			eio.inputL = eio.inputR // redirect L messages to R
			eio.inputD = eio.inputR
		}
	}

	// marshal all messages to eio.Output
	if !eio.opt_write {
		eio.callback = p.OnMsg(eio.Write, eio.Dir)
	}

	return nil
}

// Read reads data in buf from the process. Can be used concurrently.
// If cb() is nil, it is called just before bgpipe input write.
// If cb() returns false, the message is dropped.
func (eio *Extio) Read(buf []byte, cb func(m *msg.Msg) bool) error {
	// done?
	if eio.opt_read {
		return nil
	}

	var (
		p   = eio.P
		m   *msg.Msg
		err error
	)

	// parse into m
	buf = bytes.TrimSpace(buf)

	// TODO: add exabgp input?
	switch {
	case len(buf) == 0 || buf[0] == '#': // comment
		return nil

	case buf[0] == '[': // a full BGP message
		// TODO: optimize JSON unmarshal (lookup cache)
		m = p.GetMsg()
		err = m.FromJSON(buf)

	case buf[0] == '{': // an UPDATE message
		m = p.GetMsg().Use(msg.UPDATE)
		err = m.Update.FromJSON(buf)

	default:
		err = errors.New("invalid input")
	}

	if err != nil {
		eio.Err(err).Bytes("buf", buf).Msg("input parse error")
		p.PutMsg(m)
		return nil
	}

	// overwrite metadata?
	if eio.opt_seq {
		m.Seq = 0
	}
	if eio.opt_time {
		m.Time = time.Time{}
	}

	// fix type?
	// TODO: filter type from process?
	if m.Type == msg.INVALID {
		m.Use(msg.KEEPALIVE)
	}

	// callback?
	if cb != nil {
		if !cb(m) {
			p.PutMsg(m)
			return nil
		}
	}

	// sail
	switch m.Dir {
	case msg.DIR_L:
		return eio.inputL.WriteMsg(m)
	case msg.DIR_R:
		return eio.inputR.WriteMsg(m)
	default:
		return eio.inputD.WriteMsg(m)
	}
}

// Write sends BGP message to the process. Can be used concurrently.
func (eio *Extio) Write(m *msg.Msg) {
	mx := pipe.MsgContext(m)

	// filter the message?
	if !eio.opt_copy {
		// TODO: if borrow not set already, add the flag and keep m for later re-use (if enabled)
		//       NB: in such a case, we won't be able to re-use m ever
		mx.Action.Drop()
	}

	// marshal to a bytes buffer
	bb := eio.Pool.Get()
	bb.Write(m.GetJSON())

	// try writing, don't panic on channel closed [1]
	if !send_safe(eio.Output, bb) {
		mx.Callback.Drop()
		return
	}

	// TODO: warn if output full?
}

// Put puts a byte buffer back to pool
func (eio *Extio) Put(bb *bytebufferpool.ByteBuffer) {
	if bb != nil {
		eio.Pool.Put(bb)
	}
}

// OutputClose closes eio.Output, stopping the flow from bgpipe to the process
func (eio *Extio) OutputClose() error {
	close_safe(eio.Output)
	eio.callback.Drop()
	return nil
}

// InputClose closes all stage inputs, stopping the flow from the process to bgpipe
func (eio *Extio) InputClose() error {
	eio.opt_read = true
	eio.inputL.Close()
	eio.inputR.Close()
	return nil
}
