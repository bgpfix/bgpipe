package extio

// close_safe closes channel ch if ch != nil.
// It recovers from panic if the channel is already closed.
// It returns ok=true if the channel was closed successfully.
func close_safe[T any](ch chan T) (ok bool) {
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

// send_safe sends value v to channel ch, if ch != nil.
// It recovers from panic if the channel is closed.
// It returns ok=true if the value was sent successfully.
func send_safe[T any](ch chan T, v T) (ok bool) {
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

// clip returns a slice containing at most the first n elements of slice.
func clip[T []E, E any](slice T, n int) T {
	if len(slice) > n {
		return slice[:n]
	} else {
		return slice
	}
}
