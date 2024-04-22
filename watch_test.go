package fdmom

import (
	"os"
	"testing"
	"time"
)

// Leeway for no delay expectations.
const holdupMax = 100 * time.Millisecond

// Pipe contains a test setup. Shutdown is taken care of.
type pipe struct {
	// test subject
	*Watch
	// pipe read and write end
	r, w *os.File
	// file descriptor of read end
	rFD int
}

// An empty watch list may not result in read ready.
func TestWatchNoneTimeout(t *testing.T) {
	p := newPipe(t)

	start := time.Now()
	got, err := p.Watch.AwaitFDWithRead(0)
	if err != ErrTimeout {
		t.Errorf("non-blocking got FD %#x with error %v, want ErrTimeout",
			got, err)
	} else if age := time.Since(start); age > holdupMax {
		t.Errorf("non-blocking took %s, want %s at most",
			age, holdupMax)
	}

	const blockFor = 500 * time.Millisecond
	got, err = p.Watch.AwaitFDWithRead(blockFor)
	if err != ErrTimeout {
		t.Errorf("wait up to 500 ms got FD %#x with error %v, want ErrTimeout",
			got, err)
	} else if age := time.Since(start); age < blockFor || age > blockFor+holdupMax {
		t.Errorf("wait up to 500 ms took %s, want in range [%s, %s]",
			age, blockFor, blockFor+holdupMax)
	}
}

// Timeout first and then find read ready.
func TestWatchTimeoutRecover(t *testing.T) {
	p := newPipe(t)
	err := p.Watch.IncludeFD(p.rFD)
	if err != nil {
		t.Fatal(err)
	}

	got, err := p.Watch.AwaitFDWithRead(10 * time.Millisecond)
	if err != ErrTimeout {
		t.Errorf("initial await got FD %#x with error %v, want ErrTimeout",
			got, err)
	}

	const writeDelay = time.Millisecond
	go func() {
		time.Sleep(writeDelay)
		_, err := p.w.WriteString("Hello")
		if err != nil {
			t.Error("test data lost:", err)
		}
	}()

	got, err = p.Watch.AwaitFDWithRead(writeDelay + holdupMax)
	if err != nil || got != p.rFD {
		t.Errorf("second await got FD %#x with error %v, want FD %#x",
			got, err, p.rFD)
	}
}

func TestWatchPendingWrite(t *testing.T) {
	p := newPipe(t)

	// before inclusion on watch list
	_, err := p.w.WriteString("Hello")
	if err != nil {
		t.Fatal("test data lost:", err)
	}
	err = p.Watch.IncludeFD(p.rFD)
	if err != nil {
		t.Fatal(err)
	}

	// should work on all timeout options, and repeatedly
	got, err := p.Watch.AwaitFDWithRead(0)
	if err != nil || got != p.rFD {
		t.Errorf("non-blocking got FD %#x with error %v, want FD %#x",
			got, err, p.rFD)
	}
	got, err = p.Watch.AwaitFDWithRead(time.Millisecond)
	if err != nil || got != p.rFD {
		t.Errorf("await 1 ms got FD %#x with error %v, want FD %#x",
			got, err, p.rFD)
	}
	got, err = p.Watch.AwaitFDWithRead(-1)
	if err != nil || got != p.rFD {
		t.Errorf("indefinite wait got FD %#x with error %v, want FD %#x",
			got, err, p.rFD)
	}
}

func TestWatchIncludeDupe(t *testing.T) {
	p := newPipe(t)

	fd := p.rFD
	err := p.Watch.IncludeFD(fd)
	if err != nil {
		t.Fatal("error on initial inclusion:", err)
	}
	err = p.Watch.IncludeFD(fd)
	if err != nil {
		t.Fatal("error on redundant inclusion:", err)
	}

	_, err = p.w.WriteString("Hello")
	if err != nil {
		t.Fatal("test data lost:", err)
	}

	got, err := p.Watch.AwaitFDWithRead(-1)
	if err != nil || got != fd {
		t.Errorf("got FD %#x and error %v, want FD %d",
			got, err, fd)
	}
}

func TestWatchExcludeDupe(t *testing.T) {
	p := newPipe(t)

	fd := p.rFD
	err := p.Watch.IncludeFD(fd)
	if err != nil {
		t.Fatal(err)
	}
	err = p.Watch.ExcludeFD(fd)
	if err != nil {
		t.Fatal("error on initial exclusion:", err)
	}
	err = p.Watch.ExcludeFD(fd)
	if err != nil {
		t.Fatal("error on redundant exclusion:", err)
	}

	_, err = p.w.WriteString("Hello")
	if err != nil {
		t.Fatal("test data lost:", err)
	}

	got, err := p.Watch.AwaitFDWithRead(50 * time.Millisecond)
	if err != ErrTimeout {
		t.Errorf("got FD %#x with error %v, want ErrTimeout",
			got, err)
	}
}

func newPipe(t *testing.T) pipe {
	t.Parallel()
	const testTimeout = 2 * time.Second

	w, err := OpenWatch()
	if err != nil {
		t.Fatal(err)
	}
	// close watch on test timeout or test completion
	timeout := time.AfterFunc(testTimeout, func() {
		t.Error("closing watch on test timeout")
		err := w.Close()
		if err != nil {
			t.Error(err)
		}
	})
	t.Cleanup(func() {
		if timeout.Stop() {
			err := w.Close()
			if err != nil {
				t.Error(err)
			}
		}
	})

	p := pipe{Watch: w}
	p.r, p.w, err = os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		p.r.Close()
		p.w.Close()
	})
	// timeout pipe I/O too
	testDeadline := time.Now().Add(testTimeout)
	for _, f := range []*os.File{p.r, p.w} {
		f.SetDeadline(testDeadline)
	}

	p.rFD = int(p.r.Fd())
	return p
}
