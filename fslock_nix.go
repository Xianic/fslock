// Copyright 2016 Canonical Ltd.
// Licensed under the LGPLv3, see LICENCE file for details.

//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd
// +build darwin dragonfly freebsd linux netbsd openbsd

package fslock

import (
	"context"
	"syscall"
	"time"
)

// Lock implements cross-process locks using syscalls.
// This implementation is based on flock syscall.
type Lock struct {
	filename string
	fd       int
}

// New returns a new lock around the given file.
func New(filename string) *Lock {
	return &Lock{filename: filename, fd: -1}
}

// Lock locks the lock.  This call will block until the lock is available.
func (l *Lock) Lock() error {
	if err := l.open(); err != nil {
		return err
	}
	return syscall.Flock(l.fd, syscall.LOCK_EX)
}

// TryLock attempts to lock the lock.  This method will return ErrLocked
// immediately if the lock cannot be acquired.
func (l *Lock) TryLock() error {
	if err := l.open(); err != nil {
		return err
	}
	err := syscall.Flock(l.fd, syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		syscall.Close(l.fd)
	} else {
		syscall.CloseOnExec(l.fd)
	}
	if err == syscall.EWOULDBLOCK {
		return ErrLocked
	}
	return err
}

func (l *Lock) open() error {
	fd, err := syscall.Open(l.filename, syscall.O_CREAT|syscall.O_RDWR, 0600)
	if err != nil {
		return err
	}
	l.fd = fd
	return nil
}

// Unlock unlocks the lock.
func (l *Lock) Unlock() error {
	// -1 represents that failed to open the file
	if l.fd == -1 {
		return nil
	}
	return syscall.Close(l.fd)
}

// LockWithTimeout tries to lock the lock until the timeout expires.  If the
// timeout expires, this method will return ErrTimeout.
func (l *Lock) LockWithTimeout(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	err := l.LockWithContext(ctx)
	if err != nil && ctx.Err() == err {
		// To maintain backwards compatibility, this function must return
		// ErrTimeout when the context expires, not the error produced by the context
		return ErrTimeout
	}

	return err
}

// LockWithContext will wait for the lock until the context is canceled.
func (l *Lock) LockWithContext(ctx context.Context) error {
	if err := l.open(); err != nil {
		return err
	}
	result := make(chan error, 1)
	go func() {
		err := syscall.Flock(l.fd, syscall.LOCK_EX)
		select {
		case <-ctx.Done():
			// Timed out, cleanup if necessary.
			syscall.Flock(l.fd, syscall.LOCK_UN)
			syscall.Close(l.fd)
		case result <- err:
		}
	}()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
