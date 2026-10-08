package gonf

// LiveDone returns a channel that is closed when watching has stopped.
func LiveDone(l *Live) <-chan struct{} { return l.done }
