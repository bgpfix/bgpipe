package extio

import (
	"bytes"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/valyala/bytebufferpool"
)

// countWriter counts Write calls, safe for concurrent reads
type countWriter struct {
	sync.Mutex
	buf   bytes.Buffer
	calls int
}

func (w *countWriter) Write(p []byte) (int, error) {
	w.Lock()
	defer w.Unlock()
	w.calls++
	return w.buf.Write(p)
}

func (w *countWriter) get() (string, int) {
	w.Lock()
	defer w.Unlock()
	return w.buf.String(), w.calls
}

// TestWriteStream checks that WriteStream batches queued output, but flushes when idle
func TestWriteStream(t *testing.T) {
	eio := &Extio{
		Output: make(chan *bytebufferpool.ByteBuffer, 100),
		Pool:   new(bytebufferpool.Pool),
	}
	send := func(s string) {
		bb := eio.Pool.Get()
		bb.WriteString(s)
		eio.Output <- bb
	}

	// queue 100 messages before WriteStream starts: should be batched
	var want bytes.Buffer
	for i := range 100 {
		s := fmt.Sprintf("msg %d\n", i)
		want.WriteString(s)
		send(s)
	}

	w := &countWriter{}
	done := make(chan error)
	go func() { done <- eio.WriteStream(w) }()

	// wait until all is written
	deadline := time.Now().Add(time.Second)
	for {
		if got, calls := w.get(); got == want.String() {
			if calls > 10 {
				t.Fatalf("100 queued messages took %d writes, want batching", calls)
			}
			break
		} else if time.Now().After(deadline) {
			t.Fatalf("timeout: got %d bytes, want %d", len(got), want.Len())
		}
		time.Sleep(time.Millisecond)
	}

	// a single message on an idle stream must be flushed right away
	send("last\n")
	want.WriteString("last\n")
	for deadline = time.Now().Add(time.Second); ; time.Sleep(time.Millisecond) {
		if got, _ := w.get(); got == want.String() {
			break
		} else if time.Now().After(deadline) {
			t.Fatal("idle message not flushed")
		}
	}

	close(eio.Output)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
