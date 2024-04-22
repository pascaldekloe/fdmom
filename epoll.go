//go:build linux

package fdmom

import (
	"errors"
	"fmt"
	"syscall"
	"time"
)

// ErrWatchable is only available on Linux. The event notification facility with
// epoll(7) can not operate on regular files or directories.
var ErrWatchable = errors.New("file type not suitable for Watch with epoll(7)")

// Watch monitors a list of files for read availability.
type Watch struct {
	epollFD int // epoll(7)
}

// OpenWatch starts with an empty file list.
func OpenWatch() (*Watch, error) {
	const noFlags = 0
	epollFD, err := syscall.EpollCreate1(noFlags)
	if err != nil {
		return nil, fmt.Errorf("no Watch due epoll_create1(2) error %w", err)
	}
	return &Watch{epollFD: epollFD}, nil
}

// Close implements the io.Closer interface.
func (w *Watch) Close() error {
	err := syscall.Close(w.epollFD)
	if err != nil {
		return fmt.Errorf("Watch stuck on close(2) of epoll(7) error %w", err)
	}
	return nil
}

// AwaitFDWithRead blocks until it finds a file descriptor with read available
// per direct. Positive timeout values, including zero for non-blocking, cause
// an ErrTimeout on expiry. Negative timeouts block indefinitely.
func (w *Watch) AwaitFDWithRead(timeout time.Duration) (fd int, err error) {
	// timeout rounds up as they are a minimum guarantee
	msec := int((timeout + time.Millisecond - 1) / time.Millisecond)
	if timeout < 0 {
		msec = -1 // indefinite
	}

	// epoll_wait(2) goes round robin on multiple matches
	var buf [1]syscall.EpollEvent
	for {
		n, err := syscall.EpollWait(w.epollFD, buf[:], msec)
		if err == nil {
			if n == 0 {
				return 0, ErrTimeout
			}
			break
		}
		if err == syscall.EINTR {
			continue
		}
		return 0, fmt.Errorf("Watch unavailable due epoll_wait(2) error %w", err)
	}
	return int(buf[0].Fd), nil
}

// IncludeFD adds the file descriptor to the watch list. Duplicates are ignored
// silently.
func (w *Watch) IncludeFD(fd int) error {
	event := syscall.EpollEvent{
		Fd:     int32(fd),
		Events: syscall.EPOLLIN,
	}
	err := syscall.EpollCtl(w.epollFD, syscall.EPOLL_CTL_ADD, fd, &event)
	switch err {
	case nil, syscall.EEXIST:
		return nil
	case syscall.EPERM:
		return ErrWatchable
	}
	return fmt.Errorf("Watch include of file lost on epoll_ctl(2) error %w", err)
}

// ExcludeFD removes the file descriptor from the watch list. Absence is ignored
// silently.
func (w *Watch) ExcludeFD(fd int) error {
	event := syscall.EpollEvent{
		Fd:     int32(fd),
		Events: syscall.EPOLLIN,
	}
	err := syscall.EpollCtl(w.epollFD, syscall.EPOLL_CTL_DEL, fd, &event)
	switch err {
	case nil, syscall.ENOENT:
		return nil
	case syscall.EPERM:
		// not documented whether this can happen
		return ErrWatchable
	}
	return fmt.Errorf("Watch exclude of file lost on epoll_ctl(2) error %w", err)
}
