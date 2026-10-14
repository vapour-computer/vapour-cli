//go:build !linux

// Package localprofile is only available on Linux (the profiler uses eBPF and /proc).
package localprofile

import (
	"errors"
	"io"
)

// Supported reports whether profiling can run on this OS.
const Supported = false

type Options struct {
	ProfilerBin string
	Out         io.Writer
}

type Session struct{ DashboardURL string }

func (s *Session) Done() <-chan struct{} { return nil }
func (s *Session) Close()                {}

func Start(string, Options) (*Session, error) {
	return nil, errors.New("the profiler is only available on Linux")
}
