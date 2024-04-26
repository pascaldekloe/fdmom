//go:build darwin || netbsd || freebsd || openbsd || dragonfly

package fdmom

import (
	"fmt"
	"syscall"
	"time"
)

// Watch monitors a list of files for read availability.
type Watch struct {
	queueFD    int
	roundRobin int
}

// OpenWatch starts with an empty file list.
func OpenWatch() (*Watch, error) {
	fd, err := syscall.Kqueue()
	if err != nil {
		return nil, fmt.Errorf("no watch due kqueue(2) error %w", err)
	}

	return &Watch{queueFD: fd}, nil
}

// Close implements the io.Closer interface.
func (w *Watch) Close() error {
	err := syscall.Close(w.queueFD)
	if err != nil && err != syscall.EBADF {
		return fmt.Errorf("Watch stuck on close(2) of kqueue(2) error %w", err)
	}
	return nil
}

// AwaitFDWithRead blocks until it finds a file descriptor with read available
// per direct. Positive timeout values, including zero for non-blocking, cause
// an ErrTimeout on expiry. Negative timeouts block indefinitely.
func (w *Watch) AwaitFDWithRead(timeout time.Duration) (fd int, err error) {
	ts := syscall.NsecToTimespec(int64(timeout))
	var tsp *syscall.Timespec
	if timeout >= 0 {
		tsp = &ts
	}

	// When two events are read, then pick one in round-robin to
	// prevent a single descriptor from consuming all attention.
	var buf [2]syscall.Kevent_t
	var bufN int
ReadEvents:
	for {
		bufN, err = syscall.Kevent(w.queueFD, nil, buf[:], tsp)
		switch err {
		case nil:
			break ReadEvents

		case syscall.EBADF:
			return 0, ErrClosed

		case syscall.EINTR:
			continue
		}

		return 0, fmt.Errorf("Watch unavailable due kevent(2) error %w", err)
	}

	switch bufN {
	case 0:
		return 0, ErrTimeout
	case 1:
		return int(buf[0].Ident), nil
	default: // 2
		w.roundRobin++
		return int(buf[w.roundRobin&1].Ident), nil
	}
}

// IncludeFD adds the file descriptor to the watch list. Duplicates are ignored
// silently.
func (w *Watch) IncludeFD(fd int) error {
	fd64 := uint64(uint(fd))
	events := [2]syscall.Kevent_t{{
		Ident:  fd64,
		Flags:  syscall.EV_ADD,
		Filter: syscall.EVFILT_READ,
	}, {}}

	// zero value indicates an immediate timeout
	var noBlock syscall.Timespec

	n, err := syscall.Kevent(w.queueFD, events[:1], events[1:], &noBlock)
	// “When kevent() call fails with EINTR error, all changes in the
	// changelist have been applied.”
	// ―the System Calls Manual from FreeBSD
	if err != nil && err != syscall.EINTR {
		if err == syscall.EBADF {
			return ErrClosed
		}
		return fmt.Errorf("Watch IncludeFD lost on kevent(2) error %w", err)
	}

	if n != 0 && events[1].Ident == fd64 && events[1].Flags&syscall.EV_ERROR != 0 {
		return fmt.Errorf("Watch IncludeFD denied by kevent(2) with error %w",
			syscall.Errno(events[1].Data))
	}
	return nil
}

// ExcludeFD removes the file descriptor from the watch list. Absence is ignored
// silently.
func (w *Watch) ExcludeFD(fd int) error {
	fd64 := uint64(uint(fd))
	events := [2]syscall.Kevent_t{{
		Ident:  fd64,
		Flags:  syscall.EV_DELETE,
		Filter: syscall.EVFILT_READ,
	}, {}}

	// zero value indicates an immediate timeout
	var noBlock syscall.Timespec

	n, err := syscall.Kevent(w.queueFD, events[:1], events[1:], &noBlock)
	// “When kevent() call fails with EINTR error, all changes in the
	// changelist have been applied.”
	// ―the System Calls Manual from FreeBSD
	if err != nil && err != syscall.EINTR {
		if err == syscall.EBADF {
			return ErrClosed
		}
		return fmt.Errorf("Watch ExcludeFD lost on kevent(2) error %w", err)
	}

	if n != 0 && events[1].Ident == fd64 && events[1].Flags&syscall.EV_ERROR != 0 {
		err := syscall.Errno(events[1].Data)
		if err != syscall.ENOENT {
			return fmt.Errorf("Watch ExcludeFD denied by kevent(2) with error %w", err)
		}
	}
	return nil
}
