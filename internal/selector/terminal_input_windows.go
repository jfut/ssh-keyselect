//go:build windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package selector

import (
	"errors"
	"os"
	"runtime"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

var cancelSynchronousIO = windows.NewLazySystemDLL("kernel32.dll").NewProc("CancelSynchronousIo")

type windowsTerminalRead struct {
	data   []byte
	result chan windowsTerminalReadResult
}

type windowsTerminalReadResult struct {
	n   int
	err error
}

// windowsTerminalInput confines reads to one OS thread so cancellation can stop
// synchronous console/pipe reads without closing the inherited stdin handle.
type windowsTerminalInput struct {
	input    *os.File
	requests chan windowsTerminalRead
	stopped  chan struct{}
	done     chan struct{}
	mu       sync.Mutex
	thread   windows.Handle
	finished bool
	once     sync.Once
	closeErr error
}

func newWindowsTerminalInput(input *os.File) (*windowsTerminalInput, error) {
	r := &windowsTerminalInput{
		input: input, requests: make(chan windowsTerminalRead),
		stopped: make(chan struct{}), done: make(chan struct{}),
	}
	ready := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		thread, err := windows.OpenThread(windows.THREAD_TERMINATE, false, windows.GetCurrentThreadId())
		if err != nil {
			ready <- err
			return
		}
		r.thread = thread
		defer func() {
			// Prevent cancellation from targeting this thread after it returns to Go's thread pool.
			r.mu.Lock()
			r.finished = true
			_ = windows.CloseHandle(thread)
			close(r.done)
			r.mu.Unlock()
		}()
		ready <- nil
		for {
			select {
			case <-r.stopped:
				return
			case request := <-r.requests:
				select {
				case <-r.stopped:
					request.result <- windowsTerminalReadResult{err: os.ErrClosed}
				default:
					n, err := input.Read(request.data)
					request.result <- windowsTerminalReadResult{n: n, err: err}
				}
			}
		}
	}()
	if err := <-ready; err != nil {
		return nil, err
	}
	return r, nil
}

func (r *windowsTerminalInput) Read(data []byte) (int, error) {
	result := make(chan windowsTerminalReadResult, 1)
	select {
	case <-r.stopped:
		return 0, os.ErrClosed
	case r.requests <- windowsTerminalRead{data: data, result: result}:
	}
	read := <-result
	return read.n, read.err
}

func (r *windowsTerminalInput) Close() error {
	r.once.Do(func() {
		close(r.stopped)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			r.mu.Lock()
			if !r.finished {
				// Both blocking inherited handles and overlapped Go pipes are supported.
				if ok, _, err := cancelSynchronousIO.Call(uintptr(r.thread)); ok == 0 && !errors.Is(err, windows.ERROR_NOT_FOUND) && r.closeErr == nil {
					r.closeErr = err
				}
				if err := windows.CancelIoEx(windows.Handle(r.input.Fd()), nil); err != nil && !errors.Is(err, windows.ERROR_NOT_FOUND) && r.closeErr == nil {
					r.closeErr = err
				}
			}
			r.mu.Unlock()
			select {
			case <-r.done:
				return
			case <-ticker.C:
				// Retry if cancellation raced with the worker entering its read syscall.
			}
		}
	})
	return r.closeErr
}
