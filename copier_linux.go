package pipes

import (
	"context"
	"io"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

func NewCopier(ctx context.Context, r *PipeReader, writers ...*PipeWriter) (*Copier, error) {
	ls := make([]syscall.RawConn, 0, len(writers))
	for _, w := range writers {
		wrc, err := w.SyscallConn()
		if err != nil {
			return nil, err
		}
		ls = append(ls, wrc)
	}

	rwc, err := r.SyscallConn()
	if err != nil {
		return nil, err
	}

	c := &Copier{
		r:       rwc,
		writers: ls,
	}

	c.cond = sync.NewCond(&c.mu)

	go c.run(ctx)

	return c, nil
}

type Copier struct {
	r       syscall.RawConn
	writers []syscall.RawConn

	mu        sync.Mutex
	cond      *sync.Cond
	pending   []syscall.RawConn
	closedErr error
}

func (c *Copier) run(ctx context.Context) {
	for {
		c.cond.L.Lock()
		for c.closedErr == nil && len(c.writers) == 0 && ctx.Err() == nil && len(c.pending) == 0 {
			c.cond.Wait()
		}

		if c.closedErr != nil || ctx.Err() != nil {
			if c.closedErr == nil {
				c.closedErr = ctx.Err()
			}
			c.cond.L.Unlock()
			return
		}

		c.writers = append(c.writers, c.pending...)
		c.pending = c.pending[:0]

		c.cond.L.Unlock()

		c.doCopy(ctx)
	}
}

func (c *Copier) Add(w *PipeWriter) error {
	c.mu.Lock()
	if err := c.closedErr; err != nil {
		c.mu.Unlock()
		return err
	}
	c.mu.Unlock()

	wrc, err := w.SyscallConn()
	if err != nil {
		return err
	}

	c.mu.Lock()
	c.pending = append(c.pending, wrc)
	c.mu.Unlock()
	c.cond.Signal()

	return nil
}

func (c *Copier) doCopy(ctx context.Context) {
	if ctx.Err() != nil {
		c.mu.Lock()
		c.closedErr = ctx.Err()
		c.mu.Unlock()
		return
	}

	var (
		first = true
		evict []int
	)

	err := c.r.Read(func(rfd uintptr) bool {
		if first {
			first = false
			return false
		}

		var (
			total int64
		)
		for i, wrc := range c.writers {
			if ctx.Err() != nil {
				c.closedErr = ctx.Err()
				return true
			}

			if i == len(c.writers)-1 {
				n, err := c.doSplice(rfd, wrc, total)
				if err != nil || (total > 0 && n < total) {
					evict = append(evict, i)
				}
			} else {
				n, err := c.doTee(rfd, wrc, total)
				if err != nil || (total > 0 && n < total) {
					evict = append(evict, i)
					continue
				}
				if i == 0 {
					total = n
				}
			}
		}

		return true
	})

	for n, i := range evict {
		c.writers = append(c.writers[:i-n], c.writers[i-n+1:]...)
	}

	if err != nil {
		c.mu.Lock()
		if c.closedErr == nil {
			c.closedErr = err
		}
		c.mu.Unlock()
	}
}

// Copier calls doSplice when it is copying to the last (or only) writer.
//
// When `total` is 0, this should be the *only* writer.
// In such a case we only want to splice until EAGAIN (or some fatal error).
//
// When `total` is greater than zero we need to keep trying until either
// we have written `total` bytes OR some fatal error (*not* EGAIN).
func (c *Copier) doSplice(rfd uintptr, wrc syscall.RawConn, total int64) (int64, error) {
	var (
		written   int64
		spliceErr error
	)

	writeErr := wrc.Write(func(wfd uintptr) bool {
		n, err := splice(int(rfd), int(wfd), total-written)
		if n > 0 {
			written += n
		}
		spliceErr = err

		if err == unix.EAGAIN {
			if total > 0 && written < total {
				return false
			}
		}

		if n == 0 && spliceErr == nil {
			spliceErr = io.EOF
		}

		return true
	})
	if writeErr != nil {
		return written, writeErr
	}
	return written, spliceErr
}

func (c *Copier) doTee(rfd uintptr, wrc syscall.RawConn, total int64) (int64, error) {
	var (
		written int64
		teeErr  error
	)

	writeErr := wrc.Write(func(wfd uintptr) bool {
		n, err := tee(int(rfd), int(wfd), total-written)
		if n > 0 {
			written += n
		}
		teeErr = err

		if n == 0 && err == nil {
			teeErr = io.EOF
		}

		return true
	})

	if writeErr != nil {
		return written, writeErr
	}
	return written, teeErr
}
