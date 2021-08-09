package pipes

import (
	"io"
	"syscall"
)

// ReadFrom implements io.ReaderFrom for the pipe writer. It tries to use
// splice(2) to splice data from the passed in reader to the pipe. If the
// reader does not support splicing then it falls back to normal io.Copy
// semantics.
func (w *PipeWriter) ReadFrom(r io.Reader) (int64, error) {
	var (
		remain int64 = 0
		rr           = r
	)

	if lr, ok := r.(*io.LimitedReader); ok {
		rr = lr.R
		remain = lr.N
		if remain == 0 {
			return 0, nil
		}
	}

	if rc, ok := rr.(syscall.Conn); ok {
		if raw, err := rc.SyscallConn(); err == nil {
			handled, n, err := w.readFrom(raw, remain)
			if handled || err == nil {
				return n, err
			}
		}
	}

	return io.Copy(w.fd, r)
}

func (w *PipeWriter) readFrom(rc syscall.RawConn, remain int64) (bool, int64, error) {
	// TODO: Maybe cache this
	wc, err := w.fd.SyscallConn()
	if err != nil {
		return false, 0, err
	}

	var handled bool
	n, err := copyRaw(rc, wc, remain)
	if n > 0 {
		handled = true
	}
	return handled, n, err
}
