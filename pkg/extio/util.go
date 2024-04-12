package extio

func close_safe[T any](ch chan T) (ok bool) {
	if ch != nil {
		defer func() { recover() }()
		close(ch)
		return true
	}
	return
}

func send_safe[T any](ch chan T, v T) (ok bool) {
	if ch != nil {
		defer func() { recover() }()
		ch <- v
		return true
	}
	return
}
