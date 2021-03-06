package pipes

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"runtime"
	"testing"
	"time"
)

func TestCopier(t *testing.T) {
	var c *Copier

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	defer func() {
		if !t.Failed() {
			return
		}

		buf := make([]byte, 1e6)
		runtime.Stack(buf, true)
		fmt.Println(string(buf))
		t.Logf("%+v", *c)
	}()

	r1, w1 := newPipe(t)
	r2, w2 := newPipe(t)
	r3, w3 := newPipe(t)
	r4, w4 := newPipe(t)

	buf1 := bytes.NewBuffer(nil)
	buf2 := bytes.NewBuffer(nil)
	buf3 := bytes.NewBuffer(nil)

	go io.Copy(buf1, r2)
	go io.Copy(buf2, r3)
	go io.Copy(buf3, r4)

	var err error
	c, err = NewCopier(ctx, r1, w2, w3)
	if err != nil {
		t.Fatal(err)
	}

	_, err = w1.Write([]byte("hello"))
	if err != nil {
		t.Fatal(err)
	}

	checkBuffer(t, buf1, "hello")
	checkBuffer(t, buf2, "hello")

	if err := c.Add(w4); err != nil {
		t.Fatal(err)
	}

	_, err = w1.Write([]byte(" world"))
	if err != nil {
		t.Fatal(err)
	}

	checkBuffer(t, buf1, "hello world")
	checkBuffer(t, buf2, "hello world")
	checkBuffer(t, buf3, " world")
}

func checkBuffer(t *testing.T, buf *bytes.Buffer, val string) {
	t.Helper()

	for i := 0; i < 100; i++ {
		if buf.String() == val {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Errorf("expected %q, got %q", val, buf)
}
