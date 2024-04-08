package extio

import (
	"bytes"
	"errors"
	"fmt"
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

	opt_type  []msg.Type // --type
	opt_read  bool       // --read
	opt_write bool       // --write
	opt_copy  bool       // --copy
	opt_seq   bool       // --seq
	opt_time  bool       // --time
	opt_raw   bool       // --raw

	Callback *pipe.Callback // callback for bgpipe output
	InputL   *pipe.Proc     // L input to bgpipe
	InputR   *pipe.Proc     // R input to bgpipe
	InputD   *pipe.Proc     // default input if data doesn't specify the direction

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
	if f.Lookup("seq") == nil {
		f.Bool("raw", false, "speak raw BGP instead of JSON")
		f.StringSlice("type", []string{}, "skip if message is not of specified type(s)")
		f.Bool("read", false, "read-only mode (no output from bgpipe)")
		f.Bool("write", false, "write-only mode (no input to bgpipe)")
		f.Bool("copy", false, "copy messages instead of filtering (mirror)")
		f.Bool("seq", false, "overwrite message sequence number in input")
		f.Bool("time", false, "overwrite message time in input")
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
			eio.InputL = p.AddProc(msg.DIR_L)
			eio.InputR = p.AddProc(msg.DIR_R)
			if eio.IsLast {
				eio.InputD = eio.InputL
			} else {
				eio.InputD = eio.InputR
			}
		} else if eio.IsLeft {
			eio.InputL = p.AddProc(msg.DIR_L)
			eio.InputR = eio.InputL // redirect R messages to L
			eio.InputD = eio.InputL
		} else {
			eio.InputR = p.AddProc(msg.DIR_R)
			eio.InputL = eio.InputR // redirect L messages to R
			eio.InputD = eio.InputR
		}
	}

	// not read-only? write bgpipe output
	if !eio.opt_read {
		eio.Callback = p.OnMsg(eio.WriteOutput, eio.Dir, eio.opt_type...)
	}

	return nil
}

// ReadInput reads data in buf from the process. Can be used concurrently.
// Does not keep a reference to buf (copies buf if needed).
// If cb() is nil, it is called just before bgpipe input write.
// If cb() returns false, the message is dropped.
func (eio *Extio) ReadInput(buf []byte, cb func(m *msg.Msg) bool) error {
	// write-only to process?
	if eio.opt_write {
		return nil
	}

	var (
		p   = eio.P
		m   *msg.Msg
		err error
	)

	// raw message?
	if eio.opt_raw {
		m = p.GetMsg()
		_, err = m.FromBytes(buf)
		if err == nil {
			m.CopyData() // can't keep reference to buf
		}
	} else { // parse text in buf into m
		buf = bytes.TrimSpace(buf)
		switch {
		case len(buf) == 0 || buf[0] == '#': // comment
			return nil
		case buf[0] == '[': // a BGP message
			// TODO: optimize unmarshal (lookup cache of recently marshaled msgs)
			m = p.GetMsg()
			err = m.FromJSON(buf)

			// shortcut
			if m.Type == msg.INVALID {
				m.Use(msg.KEEPALIVE)
			}
		case buf[0] == '{': // an UPDATE
			m = p.GetMsg().Use(msg.UPDATE)
			err = m.Update.FromJSON(buf)
		case bytes.HasPrefix(buf, msg.BgpMarker): // a raw message
			m = p.GetMsg()
			_, err = m.FromBytes(buf)
			if err == nil {
				m.CopyData() // can't reference buf
			}
		default:
			// TODO: add exabgp?
			err = errors.New("unrecognized format")
		}
	}

	// parse error?
	if err != nil {
		if eio.opt_raw {
			eio.Err(err).Hex("input", buf).Msg("input parse error")
		} else {
			eio.Err(err).Bytes("input", buf).Msg("input parse error")
		}
		p.PutMsg(m)
		return nil
	}

	// filter type?
	if len(eio.opt_type) > 0 {
		if slices.Index(eio.opt_type, m.Type) < 0 {
			p.PutMsg(m)
			return nil
		}
	}

	// overwrite message metadata?
	if eio.opt_seq {
		m.Seq = 0
	}
	if eio.opt_time {
		m.Time = time.Now().UTC()
	}

	// callback?
	if cb != nil {
		if !cb(m) {
			p.PutMsg(m)
			return nil
		}
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

// WriteOutput sends BGP message to the process. Can be used concurrently.
func (eio *Extio) WriteOutput(m *msg.Msg) {
	// read-only from process?
	if eio.opt_read {
		return
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
		return
	}

	// try writing, don't panic on channel closed [1]
	if !send_safe(eio.Output, bb) {
		mx.Callback.Drop()
		return
	}
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
