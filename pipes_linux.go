package pipes

import (
	"io"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

type PipeReader struct {
	fd *os.File
}

func (r *PipeReader) Read(p []byte) (int, error) {
	return r.fd.Read(p)
}

func (r *PipeReader) Close() error {
	return r.fd.Close()
}

func (r *PipeReader) SyscallConn() (syscall.RawConn, error) {
	return r.fd.SyscallConn()
}

type PipeWriter struct {
	fd *os.File
}

func (w *PipeWriter) Write(p []byte) (int, error) {
	return w.fd.Write(p)
}

func (w *PipeWriter) Close() error {
	return w.fd.Close()
}

func (w *PipeWriter) SyscallConn() (syscall.RawConn, error) {
	return w.fd.SyscallConn()
}

func New() (*PipeReader, *PipeWriter, error) {
	var p [2]int
	if err := unix.Pipe2(p[:], unix.O_CLOEXEC|unix.O_NONBLOCK); err != nil {
		return nil, nil, err
	}
	pr := &PipeReader{fd: os.NewFile(uintptr(p[0]), "read")}
	pw := &PipeWriter{fd: os.NewFile(uintptr(p[1]), "write")}
	return pr, pw, nil
}

// Open opens a fifo in read only mode
// It *does not* block when opening.
// This should have smiliar semantics to os.Open, except this is for a fifo.
//
// See OpenFifo more more granular control.
func Open(p string) (*PipeReader, error) {
	pr, _, err := OpenFifo(p, os.O_RDONLY, 0)
	return pr, err
}

// Create opens the fifo with RDWR mode. If the fifo does not exist it will
// create it with 0666 (before umask) permissions.
//
// This should have similar semnatics to os.Create, except for fifos.
func Create(p string) (*PipeReader, *PipeWriter, error) {
	return OpenFifo(p, os.O_RDWR|os.O_CREATE, 0666)
}

// OpenFifo opens a fifo from the provided path.
// The fifo is always opened in non-blocking mode.
//
// If flag includes os.O_CREATE this will create the fifo.
// The mode parameter should be used to set fifo permissions.
//
// Note, according to Linux fifo semantics, this will block if you are trying
// to open as os.O_WRONLY and nothing has the fifo opened in read-mode. To
// ensure a non-blocking experience, use os.O_RDWR.
// You can open once with RDWR, then open again with O_WRONLY to get around
// this semantic.
//
// If no open mode is specified (RDWR, RDONLY, WRONLY), then RDWR is used.
// TODO: Is this the right way to handle the open mode?
func OpenFifo(p string, flag int, mode os.FileMode) (pr *PipeReader, pw *PipeWriter, _ error) {
	if flag&os.O_RDWR == 0 && flag&os.O_RDONLY == 0 && flag&os.O_WRONLY == 0 {
		flag |= os.O_RDWR
	}

	if flag&os.O_CREATE != 0 {
		if _, err := os.Stat(p); err != nil {
			if !os.IsNotExist(err) {
				return nil, nil, err
			}
			if err := unix.Mkfifo(p, uint32(mode.Perm())); err != nil {
				return nil, nil, err
			}
		}
	}

	flag &= ^os.O_CREATE

	if flag&unix.O_NONBLOCK != 0 && flag&os.O_RDWR == 0 {
		// Open first with rdwr so the main open does not block
		flag2 := flag
		flag2 &= ^os.O_RDONLY
		flag2 &= ^os.O_WRONLY
		flag2 &= ^unix.O_NONBLOCK

		fdrdwr, err := os.OpenFile(p, flag2|os.O_RDWR, 0)
		if err != nil {
			return nil, nil, err
		}
		defer fdrdwr.Close()
	}

	f, err := os.OpenFile(p, flag, 0)
	if err != nil {
		return nil, nil, err
	}

	if flag&os.O_RDONLY != 0 || flag&os.O_RDWR != 0 {
		pr = &PipeReader{fd: f}
	}
	if flag&os.O_WRONLY != 0 || flag&os.O_RDWR != 0 {
		pw = &PipeWriter{fd: f}
	}
	return pr, pw, nil
}

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

func splice(rc, wc syscall.RawConn, remain int64) (copied int64, spliceErr error) {
	spliceOpts := unix.SPLICE_F_MOVE | unix.SPLICE_F_NONBLOCK | unix.SPLICE_F_MORE

	noEnd := remain == 0
	if noEnd {
		remain = 1 << 62
	}

	var readErr error
	err := wc.Write(func(wfd uintptr) bool {
		readErr = rc.Read(func(rfd uintptr) bool {
			var n int64
			for remain > 0 {
				n, spliceErr = unix.Splice(int(rfd), nil, int(wfd), nil, int(remain), spliceOpts)
				if n > 0 {
					copied += n
					if !noEnd {
						remain -= n
					}
				}

				if spliceErr != nil {
					if spliceErr == unix.EINTR {
						continue
					}
					return true
				}

				if n == 0 {
					// EOF
					return true
				}
				continue
			}

			return true
		})
		if readErr != nil {
			return true
		}
		if remain == 0 {
			return true
		}
		return spliceErr != unix.EAGAIN
	})

	if err != nil {
		return copied, err
	}

	if readErr != nil {
		return copied, readErr
	}

	return copied, spliceErr
}

func (w *PipeWriter) readFrom(rc syscall.RawConn, remain int64) (bool, int64, error) {
	// TODO: Maybe cache this
	wc, err := w.fd.SyscallConn()
	if err != nil {
		return false, 0, err
	}

	var handled bool
	n, err := splice(rc, wc, remain)
	if n > 0 {
		handled = true
	}
	return handled, n, err
}

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

	n, err := splice(rc, w, 0)
	return n > 0, n, err
}
