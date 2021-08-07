package pipes

import (
	"io"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

var _ io.ReadWriteCloser = &Pipe{}

type Pipe struct {
	rfd *os.File
	wfd *os.File
}

func New() (*Pipe, error) {
	var p [2]int
	if err := unix.Pipe2(p[:], unix.O_CLOEXEC|unix.O_NONBLOCK); err != nil {
		return nil, err
	}

	return &Pipe{
		rfd: os.NewFile(uintptr(p[0]), "read"),
		wfd: os.NewFile(uintptr(p[1]), "write"),
	}, nil
}

func (p *Pipe) Write(data []byte) (n int, err error) {
	return p.wfd.Write(data)
}

func (p *Pipe) Read(data []byte) (int, error) {
	return p.rfd.Read(data)
}

func (p *Pipe) Close() error {
	p.rfd.Close()
	p.wfd.Close()
	return nil
}

func (p *Pipe) ReadFrom(r io.Reader) (int64, error) {
	var (
		remain int64 = 0
		rr           = r
	)
	if lr, ok := r.(*io.LimitedReader); ok {
		rr = lr.R
		remain = lr.N
	}
	if rc, ok := rr.(syscall.Conn); ok {
		if raw, err := rc.SyscallConn(); err == nil {
			return p.readFrom(raw, remain)
		}
	}
	return io.Copy(p.wfd, r)
}

func (p *Pipe) readFrom(rc syscall.RawConn, remain int64) (int64, error) {
	noEnd := remain == 0
	if noEnd {
		remain = 1 << 62
	}

	if remain == 0 {
		return 0, nil
	}

	var (
		copied    int64
		spliceErr error
	)

	spliceOpts := unix.SPLICE_F_MOVE | unix.SPLICE_F_NONBLOCK | unix.SPLICE_F_MORE

	wc, err := p.wfd.SyscallConn()
	if err != nil {
		return 0, err
	}

	err = wc.Write(func(wfd uintptr) bool {
		readErr := rc.Read(func(rfd uintptr) bool {
			var n int64
			for remain > 0 {
				n, spliceErr = unix.Splice(int(rfd), nil, int(wfd), nil, int(remain), spliceOpts)
				if n > 0 {
					copied += n
					if !noEnd {
						remain -= n
					}
				}
				if err != nil {
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

	return copied, spliceErr
}
