package pipes

import (
	"bytes"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newPipe(t testing.TB) (*PipeReader, *PipeWriter) {
	t.Helper()

	r, w, err := New()
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { r.Close(); w.Close() })

	return r, w
}

func TestReadFrom(t *testing.T) {
	t.Run("raw", func(t *testing.T) {
		t.Run("regular file", func(t *testing.T) {
			f := createFile(t)
			pr, pw := newPipe(t)

			go drainPipe(pr)

			prepareReadFrom(t, f, 1e6)
			f.Seek(0, io.SeekStart)

			doReadFromTest(t, pw, f, 1e6)
		})

		t.Run("limited reader", func(t *testing.T) {
			f := createFile(t)
			pr, pw := newPipe(t)

			go drainPipe(pr)

			prepareReadFrom(t, f, 2e6)
			f.Seek(0, io.SeekStart)

			// Note the file has 2e6 bytes but we limit to 1e6
			limit := &io.LimitedReader{R: f, N: 1e6}
			doReadFromTest(t, pw, limit, limit.N)
		})
	})

	t.Run("userspace", func(t *testing.T) {
		t.Run("fallback copy", func(t *testing.T) {
			pr, pw := newPipe(t)

			go io.Copy(ioutil.Discard, pr)

			buf := bytes.NewReader(make([]byte, 1e6))
			doReadFromTest(t, pw, buf, 1e6)
		})

		t.Run("limited reader", func(t *testing.T) {
			pr, pw := newPipe(t)
			go io.Copy(ioutil.Discard, pr)

			// Note the buffer is 2e6, and we limit with 1e6.
			buf := &io.LimitedReader{R: bytes.NewReader(make([]byte, 2e6)), N: 1e6}
			doReadFromTest(t, pw, buf, buf.N)
		})
	})
}

func TestOpenFifo(t *testing.T) {
	t.Run("async", func(t *testing.T) {
		dir := t.TempDir()
		fifo := filepath.Join(dir, filepath.Base(t.Name()))

		results, err := AsyncOpenFifo(fifo, os.O_WRONLY|os.O_CREATE, 0600)
		if err != nil {
			t.Fatal(err)
		}

		var haveResults bool
		defer func() {
			if !haveResults {
				select {
				case r := <-results:
					if r.R != nil {
						t.Error("unexpected pipe reader")
						r.R.Close()
					}
					if r.W != nil {
						r.W.Close()
					}
				case <-time.After(5 * time.Second):
					t.Fatal("timeout waiting for async results to return")
				}
			}
		}()

		select {
		case r := <-results:
			t.Error(r.Err)
			if r.R != nil {
				r.R.Close()
			}
			if r.W != nil {
				r.W.Close()
			}
			t.Fatal("should not have gotten result")
		default:
		}

		r, _, err := OpenFifo(fifo, os.O_RDONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()

		select {
		case result := <-results:
			haveResults = true
			if result.Err != nil {
				t.Fatal(err)
			}
			if result.R != nil {
				t.Error("got unexpected pipe reader for write only request")
				result.R.Close()
			}

			if result.W == nil {
				t.Fatal("missing write side")
			}
			defer result.W.Close()

			data := []byte("hello")
			_, err = result.W.Write(data)
			if err != nil {
				t.Fatal(err)
			}

			buf := make([]byte, len(data))
			_, err := r.Read(buf)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(buf, data) {
				t.Fatalf("expected %q, got %q", string(data), string(buf))
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for async open")
		}
	})
}

func TestOpenFifoCloseRDWR(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, filepath.Base(t.Name()))

	r, w, err := OpenFifo(p, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkReadFrom(b *testing.B) {
	benchReadFromFile(b)
}

func benchReadFromFile(b *testing.B) {
	var (
		f *os.File
	)

	defer func() {
		f.Close()
		os.RemoveAll(f.Name())
	}()

	prep := func(b testing.TB, i int, total int64) io.Reader {
		if i == 0 {
			f = createFile(b)
			prepareReadFrom(b, f, total)
		}
		_, err := f.Seek(0, io.SeekStart)
		if err != nil {
			b.Fatal(err)
		}
		return f
	}

	b.Run("regular file", func(b *testing.B) { benchReadFrom(b, prep) })
}

func benchReadFrom(b *testing.B, prep prepFunc) {
	b.Run("16K", func(b *testing.B) { doBenchReadFrom(b, prep, 16*1024) })
	b.Run("32K", func(b *testing.B) { doBenchReadFrom(b, prep, 32*1024) })
	b.Run("64K", func(b *testing.B) { doBenchReadFrom(b, prep, 64*1024) })
	b.Run("128K", func(b *testing.B) { doBenchReadFrom(b, prep, 128*1024) })
	b.Run("256K", func(b *testing.B) { doBenchReadFrom(b, prep, 256*1024) })
	b.Run("512K", func(b *testing.B) { doBenchReadFrom(b, prep, 512*1024) })
	b.Run("1MB", func(b *testing.B) { doBenchReadFrom(b, prep, 1024*1024) })
	b.Run("10MB", func(b *testing.B) { doBenchReadFrom(b, prep, 10*1024*1024) })
	b.Run("100MB", func(b *testing.B) { doBenchReadFrom(b, prep, 100*1024*1024) })
	b.Run("1GB", func(b *testing.B) { doBenchReadFrom(b, prep, 1024*1024*1024) })
}

func drainPipe(pr *PipeReader) {
	buf := make([]byte, 1e6)
	io.CopyBuffer(ioutil.Discard, pr, buf)
}

func doBenchReadFrom(b *testing.B, prep prepFunc, total int64) {
	b.StopTimer()

	b.SetBytes(total)
	b.ResetTimer()
	b.StartTimer()

	pr, pw := newPipe(b)
	defer func() {
		pr.Close()
		pw.Close()
	}()

	go drainPipe(pr)

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		r := prep(b, i, total)
		b.StartTimer()
		doReadFromTest(b, pw, r, total)
	}
}

func doReadFromTest(t testing.TB, p *PipeWriter, f io.Reader, total int64) {
	t.Helper()
	n, err := p.ReadFrom(f)
	if err != nil {
		t.Fatal(err)
	}
	if n != total {
		t.Errorf("Got unexpected number of bytes for ReadFrom, expected %d, got %d", total, n)
	}
}

type prepFunc func(b testing.TB, i int, total int64) io.Reader

func createFile(t testing.TB) *os.File {
	dir := t.TempDir()

	w, err := os.Create(filepath.Join(dir, filepath.Base(t.Name())))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })

	return w
}

func prepareReadFrom(t testing.TB, w io.Writer, total int64) {
	data := make([]byte, 1024*1024)
	var copied int64

	for (total - copied) > 0 {
		remain := total - copied
		if remain > int64(len(data)) {
			remain = int64(len(data))
		}

		n, err := w.Write(data[:remain])
		if err != nil {
			t.Fatal(err)
		}

		copied += int64(n)
	}

	if total != copied {
		t.Fatalf("wrote unexpected amount of data to test file, expected: %d, got: %d", total, copied)
	}
}
