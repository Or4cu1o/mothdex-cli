//go:build gui && crashtest

// A build for testers only (docs/contracts/cli/logs.md section 8): the render thread panics on its second
// frame, so the crash file, the log line and the crash screen can be checked on a real machine. No release is
// built with this tag.

package main

import (
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/loop"
)

// crashValue carries a made-up secret, to see that no file holds it.
const crashValue = "crashtest: forced panic at https://example.test/get?token=CRASHTESTSECRET"

type crashingBackend struct {
	loop.Backend
	frames int
}

func (b *crashingBackend) Present(layout draw.Layout, l *draw.List) error {
	if b.frames++; b.frames == 2 {
		panic(crashValue)
	}
	return b.Backend.Present(layout, l)
}

func init() {
	wrapBackend = func(b loop.Backend) loop.Backend { return &crashingBackend{Backend: b} }
}
