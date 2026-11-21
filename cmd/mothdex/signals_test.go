package main

import (
	"os"
	"syscall"
	"testing"
	"time"
)

// The signal rules of ui.md section 1: the first signal cancels, a second within two seconds exits
// with 130, and the Shell can take SIGINT over while SIGTERM keeps its default.
func TestSignalRouter(t *testing.T) {
	type event struct {
		sig     os.Signal
		after   time.Duration // time since the previous event
		handoff string        // "take" or "release" before the signal, "" for none
	}
	sec := time.Second
	tests := []struct {
		name       string
		events     []event
		wantCancel int
		wantExit   []int
		wantSunk   int // SIGINTs delivered to the Shell
	}{
		{"one interrupt cancels", []event{{os.Interrupt, 0, ""}}, 1, nil, 0},
		{"a second one within the window exits", []event{{os.Interrupt, 0, ""}, {os.Interrupt, sec, ""}}, 1, []int{130}, 0},
		{"a second one after the window only cancels again", []event{{os.Interrupt, 0, ""}, {os.Interrupt, 3 * sec, ""}}, 2, nil, 0},
		{"terminate then interrupt within the window exits", []event{{syscall.SIGTERM, 0, ""}, {os.Interrupt, sec, ""}}, 1, []int{130}, 0},
		{"the Shell takes SIGINT", []event{{os.Interrupt, 0, "take"}}, 0, nil, 1},
		{"two handed-over interrupts never exit", []event{{os.Interrupt, 0, "take"}, {os.Interrupt, 0, ""}}, 0, nil, 2},
		{"SIGTERM still cancels while the Shell holds SIGINT", []event{{syscall.SIGTERM, 0, "take"}}, 1, nil, 0},
		{"after release SIGINT cancels again", []event{{os.Interrupt, 0, "take"}, {os.Interrupt, 0, "release"}}, 1, nil, 1},
		{"a handed-over interrupt does not open the exit window", []event{{os.Interrupt, 0, "take"}, {os.Interrupt, sec, "release"}}, 1, nil, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
			var cancels int
			var exits []int
			r := newSignalRouter(func() { cancels++ }, func(c int) { exits = append(exits, c) }, func() time.Time { return now })
			var sink <-chan os.Signal
			var release func()
			sunk := 0
			for _, ev := range tc.events {
				now = now.Add(ev.after)
				switch ev.handoff {
				case "take":
					sink, release = r.interrupts()
				case "release":
					release()
				}
				r.handle(ev.sig)
				for sink != nil {
					select {
					case <-sink:
						sunk++
						continue
					default:
					}
					break
				}
			}
			if cancels != tc.wantCancel || len(exits) != len(tc.wantExit) || sunk != tc.wantSunk {
				t.Fatalf("cancels %d exits %v sunk %d; want %d %v %d", cancels, exits, sunk, tc.wantCancel, tc.wantExit, tc.wantSunk)
			}
			for i, c := range tc.wantExit {
				if exits[i] != c {
					t.Fatalf("exit codes %v, want %v", exits, tc.wantExit)
				}
			}
		})
	}
}
