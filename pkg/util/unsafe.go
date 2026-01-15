package util

// Close closes channel ch if ch != nil.
// It recovers from panic if the channel is already closed.
// It returns ok=true if the channel was closed successfully.
func Close[T any](ch chan T) (ok bool) {
	if ch == nil {
		return
	}
	defer func() {
		if !ok {
			recover()
		}
	}()
	close(ch)
	return true
}

// Send sends value v to channel ch, if ch != nil.
// It recovers from panic if the channel is closed.
// It returns ok=true if the value was sent successfully.
func Send[T any](ch chan T, v T) (ok bool) {
	if ch == nil {
		return
	}
	defer func() {
		if !ok {
			recover()
		}
	}()
	ch <- v
	return true
}
