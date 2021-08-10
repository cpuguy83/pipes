package pipes

import (
	"bytes"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestReadFrom(t *testing.T) {
	t.Run("raw", func(t *testing.T) {
		t.Run("regular file", func(t *testing.T) {
			p, f := prepareReadFrom(t, 1e6)
			defer p.Close()
			defer func() {
				f.Close()
				os.RemoveAll(f.Name())
			}()
			doReadFromTest(t, p, f, 1e6)
		})

		t.Run("limited reader", func(t *testing.T) {
			p, f := prepareReadFrom(t, 2e6)
			defer p.Close()
			defer func() {
				f.Close()
				os.RemoveAll(f.Name())
			}()

			// Note the file has 2e6 bytes but we limit to 1e6
			limit := &io.LimitedReader{R: f, N: 1e6}
			doReadFromTest(t, p, limit, limit.N)
		})
	})

	t.Run("userspace", func(t *testing.T) {
		t.Run("fallback copy", func(t *testing.T) {
			pr, pw, err := New()
			if err != nil {
				t.Fatal(err)
			}
			defer pr.Close()
			defer pw.Close()

			go io.Copy(ioutil.Discard, pr)

			buf := bytes.NewReader(make([]byte, 1e6))
			doReadFromTest(t, pw, buf, 1e6)
		})

		t.Run("limited reader", func(t *testing.T) {
			pr, pw, err := New()
			if err != nil {
				t.Fatal(err)
			}
			defer pr.Close()
			defer pw.Close()
			go io.Copy(ioutil.Discard, pr)

			// Note the buffer is 2e6, and we limit with 1e6.
			buf := &io.LimitedReader{R: bytes.NewReader(make([]byte, 2e6)), N: 1e6}
			doReadFromTest(t, pw, buf, buf.N)
		})
	})
}

func TestOpenFifo(t *testing.T) {
	dir := t.TempDir()

	t.Run("async open", func(t *testing.T) {
		_, pw, err := OpenFifo(filepath.Join(dir, filepath.Base(t.Name())), unix.O_NONBLOCK|os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		pw.Close()
	})
}

func BenchmarkReadFrom(b *testing.B) {
	b.Run("16K", func(b *testing.B) { doBenchReadFrom(b, 16*1024) })
	b.Run("32K", func(b *testing.B) { doBenchReadFrom(b, 32*1024) })
	b.Run("64K", func(b *testing.B) { doBenchReadFrom(b, 64*1024) })
	b.Run("128K", func(b *testing.B) { doBenchReadFrom(b, 128*1024) })
	b.Run("256K", func(b *testing.B) { doBenchReadFrom(b, 256*1024) })
	b.Run("512K", func(b *testing.B) { doBenchReadFrom(b, 512*1024) })
	b.Run("1MB", func(b *testing.B) { doBenchReadFrom(b, 1024*1024) })
	b.Run("10MB", func(b *testing.B) { doBenchReadFrom(b, 10*1024*1024) })
	b.Run("100MB", func(b *testing.B) { doBenchReadFrom(b, 100*1024*1024) })
	b.Run("1GB", func(b *testing.B) { doBenchReadFrom(b, 1024*1024*1024) })
}

func doBenchReadFrom(b *testing.B, total int64) {
	b.StopTimer()

	p, f := prepareReadFrom(b, total)
	defer p.Close()
	defer func() {
		f.Close()
		os.RemoveAll(f.Name())
	}()

	b.SetBytes(total)
	b.ResetTimer()
	b.StartTimer()

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()

		doReadFromTest(b, p, f, total)
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

func prepareReadFrom(t testing.TB, total int64) (*PipeWriter, *os.File) {
	dir := t.TempDir()

	f, err := os.Create(filepath.Join(dir, filepath.Base(t.Name())))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })

	pr, pw, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pr.Close(); pw.Close() })

	// Make sure we dump all the data out of p so it doesn't fill up.
	// This goroutine will exit once `p` is closed.
	buf := make([]byte, 1e6)
	go io.CopyBuffer(ioutil.Discard, pr, buf)

	data := make([]byte, 1024*1024)
	var copied int64

	for (total - copied) > 0 {
		remain := total - copied
		if remain > int64(len(data)) {
			remain = int64(len(data))
		}

		n, err := f.Write(data[:remain])
		if err != nil {
			t.Fatal(err)
		}

		copied += int64(n)
	}

	if total != copied {
		t.Fatalf("wrote unexpected amount of data to test file, expected: %d, got: %d", total, copied)
	}

	_, err = f.Seek(0, io.SeekStart)
	if err != nil {
		t.Fatal(err)
	}

	return pw, f
}
