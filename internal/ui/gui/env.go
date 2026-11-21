package gui

import (
	"context"
	"io"
	"log/slog"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/draw"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/input"
	"github.com/Or4cu1o/mothdex-cli/internal/ui/gui/text"
	"github.com/Or4cu1o/mothdex-cli/pkg/mothdex"
)

// Service is the part of the app service the GUI uses (docs/ux/gui.md section 7). Tests pass guitest.Fake.
type Service interface {
	DBInfo(ctx context.Context) (app.Result[mothdex.DBInfo], error)
	Search(ctx context.Context, q app.SearchQuery) (app.SearchPage, error)
	Entry(ctx context.Context, ref app.Ref) (app.Result[mothdex.Entry], error)
	Random(ctx context.Context, q app.RandomQuery) (app.Result[[]mothdex.Entry], error)
	Platforms(ctx context.Context, f app.PlatformFilter) (app.Result[[]mothdex.Platform], error)
	Regions(ctx context.Context) (app.Result[[]mothdex.Region], error)
	Links(ctx context.Context, ref app.Ref, sel app.LinkSelector) ([]app.LinkChoice, error)
	Cover(ctx context.Context, ref app.Ref) (io.ReadCloser, string, error)
	Enqueue(ctx context.Context, reqs []app.DownloadRequest) ([]app.JobID, error)
	SaveJobBoxart(ctx context.Context, job app.JobSnapshot, destDir string) (string, error)
	History(ctx context.Context, f app.HistoryFilter) ([]app.HistoryRecord, error)
	ConfigList() ([]app.Setting, error)
	Jobs() []app.JobSnapshot
	Cancel(id app.JobID) error
	Wait(ctx context.Context) (app.BatchResult, error)
	Close() error
	ActiveProfile() app.ProfileInfo
}

// Msg is what Update reacts to. The run loop sends InputMsg, TickMsg, ResizeMsg, ControllerMsg, JobMsg and
// SignalMsg; the results of a Cmd come back as unexported messages.
type Msg = any

// Cmd is work that must not run on the render thread: a service call. The run loop runs it on its own
// goroutine and sends the Msg it returns back to Update.
type Cmd func() Msg

// Messages from the run loop.
type (
	// InputMsg is one action of the input layer.
	InputMsg struct {
		Action input.Action
		// Repeat is true for an action the input layer repeated because the button is held, so a screen can
		// ignore it where one more press would do something else (B at an empty field goes back).
		Repeat bool
	}
	// TextMsg is text typed on a physical keyboard while the model asked for text input.
	TextMsg struct{ Text string }
	// TickMsg says time has passed: timers are checked against the time given to Update.
	TickMsg struct{}
	// ResizeMsg is a new display size in pixels (a window event, an HDMI change).
	ResizeMsg struct{ W, H int }
	// ControllerMsg is the type of the pad now in use, for the hint labels.
	ControllerMsg struct{ Type input.ControllerType }
	// JobMsg is one event of the downloader.
	JobMsg struct{ Job app.JobSnapshot }
	// SignalMsg is SIGTERM or SIGINT: no dialog, cancel, Wait, Close, exit.
	SignalMsg struct{}
	// QuitRequestMsg is a request to close the window (Alt+F4 on a desktop): it asks like Start menu Quit,
	// so downloads are never canceled without the person's word.
	QuitRequestMsg struct{}
	// RedrawMsg says the window must be drawn again with nothing else changed (it was uncovered or restored).
	RedrawMsg struct{}
)

// Options is what the composition root gives the model.
type Options struct {
	Service        Service
	T              *i18n.Translator
	Font           *text.Font
	Clock          Clock
	Log            *slog.Logger
	DataDir        string // paths.data, where tui_state lives
	LogPath        string // the log file, for the internal error text
	Version        string // shown on the Splash
	LocaleFallback bool   // the interface language needs a font this screen mode lacks: English is shown
	Width, Height  int    // the display size at start

	// What the session decided from the configuration: the download.prefer baseline, download.boxart and
	// interface.confirm_downloads.
	Prefer  app.Preference
	Boxart  bool
	Confirm bool

	// Settings known before ConfigList answers (the command line or the environment may set them).
	ConfirmEast bool
	LargeText   bool
	Safe        SafeArea
}

// rect aliases keep the screens short.
type rect = draw.Rect
