// Copyright 2016 Canonical Ltd.
// Licensed under the LGPLv3, see LICENCE file for details.

package fslock

import (
	"golang.org/x/sys/windows"
	"log"
	"syscall"
	"time"
)

func init() {

	log.SetFlags(log.Lmicroseconds | log.Ldate)
}

// Lock implements cross-process locks using syscalls.
// This implementation is based on LockFileEx syscall.
type Lock struct {
	filename string
	handle   windows.Handle
}

// New returns a new lock around the given file.
func New(filename string) *Lock {
	return &Lock{filename: filename}
}

// TryLock attempts to lock the lock.  This method will return ErrLocked
// immediately if the lock cannot be acquired.
func (l *Lock) TryLock() error {
	err := l.LockWithTimeout(0)
	if err == ErrTimeout {
		// in our case, timing out immediately just means it was already locked.
		return ErrLocked
	}
	return err
}

// Lock locks the lock.  This call will block until the lock is available.
func (l *Lock) Lock() error {
	return l.LockWithTimeout(-1)
}

// Unlock unlocks the lock.
func (l *Lock) Unlock() error {
	return windows.Close(l.handle)
}

// LockWithTimeout tries to lock the lock until the timeout expires.  If the
// timeout expires, this method will return ErrTimeout.
func (l *Lock) LockWithTimeout(timeout time.Duration) (err error) {
	name, err := windows.UTF16PtrFromString(l.filename)
	if err != nil {
		return err
	}

	// Open for asynchronous I/O so that we can timeout waiting for the lock.
	// Also open shared so that other processes can open the file (but will
	// still need to lock it).
	handle, err := windows.CreateFile(
		name,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ,
		nil,
		windows.OPEN_ALWAYS,
		windows.FILE_FLAG_OVERLAPPED|windows.FILE_ATTRIBUTE_NORMAL,
		0)
	if err != nil {
		return err
	}
	l.handle = handle
	defer func() {
		if err != nil {
			windows.Close(handle)
		}
	}()

	millis := uint32(windows.INFINITE)
	if timeout >= 0 {
		millis = uint32(timeout.Nanoseconds() / 1000000)
	}

	ol, err := newOverlapped()
	if err != nil {
		return err
	}
	defer windows.CloseHandle(ol.HEvent)
	err = windows.LockFileEx(handle, windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, ol)
	if err == nil {
		return nil
	}

	// ERROR_IO_PENDING is expected when we're waiting on an asychronous event
	// to occur.
	if err != windows.ERROR_IO_PENDING {
		return err
	}
	s, err := windows.WaitForSingleObject(ol.HEvent, millis)

	switch s {
	case syscall.WAIT_OBJECT_0:
		// success!
		return nil
	case syscall.WAIT_TIMEOUT:
		return ErrTimeout
	default:
		return err
	}
}

// newOverlapped creates a structure used to track asynchronous
// I/O requests that have been issued.
func newOverlapped() (*windows.Overlapped, error) {
	manualReset := uint32(1)
	initialState := uint32(0)
	event, err := windows.CreateEvent(nil, manualReset, initialState, nil)
	if err != nil {
		return nil, err
	}
	return &windows.Overlapped{HEvent: event}, nil
}
