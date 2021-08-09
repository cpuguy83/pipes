package pipes

import (
	"io"
	"syscall"
)

func (r *PipeReader) WriteTo(w io.Writer) (int64, error) {
	if wc, ok := w.(syscall.Conn); ok {
		if raw, err := wc.SyscallConn(); err == nil {
			handled, n, err := r.writeTo(raw)
			if handled || err == nil {
				return n, err
			}
		}
	}

	return io.Copy(w, r.fd)
}

func (r *PipeReader) writeTo(w syscall.RawConn) (bool, int64, error) {
	rc, err := r.SyscallConn()
	if err != nil {
		return false, 0, err
	}

	n, err := copyRaw(rc, w, 0)
	return n > 0, n, err
}
