package pipes

import (
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func TestReadFrom(t *testing.T) {
	p, f := prepareReadFrom(t, 1e6)
	defer p.Close()
	defer func() {
		f.Close()
		os.RemoveAll(f.Name())
	}()
	doReadFromTest(t, p, f, 1e6)
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

func doReadFromTest(t testing.TB, p *Pipe, f io.Reader, total int64) {
	n, err := p.ReadFrom(f)
	if err != nil {
		t.Fatal(err)
	}
	if n != total {
		t.Errorf("Got unexpected number of bytes for ReadFrom, expected %d, got %d", total, n)
	}
}

func prepareReadFrom(t testing.TB, total int64) (*Pipe, *os.File) {
	dir := t.TempDir()

	f, err := os.Create(filepath.Join(dir, filepath.Base(t.Name())))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })

	p, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })

	// Make sure we dump all the data out of p so it doesn't fill up.
	// This goroutine will exit once `p` is closed.
	go func() {
		buf := make([]byte, 1e6)
		io.CopyBuffer(ioutil.Discard, p, buf)
	}()

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

	return p, f
}
