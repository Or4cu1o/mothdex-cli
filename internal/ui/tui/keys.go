package tui

import "strings"

// screenID names a screen; the key map and the help depend on it.
type screenID int

const (
	scSearch screenID = iota
	scResults
	scEntry
	scQueue
	scHistory
	scSettings
)

// focusKind says where typing goes: it decides whether a letter is a command or text.
type focusKind int

const (
	focusNone   focusKind = iota // lists and screens: letters are commands
	focusText                    // a text field has focus: letters are text
	focusPrompt                  // a [y/N] question is open
)

// action is what a key means. The reducer is a pure function so the whole map is tested as a table.
type action int

const (
	actNone action = iota
	actText        // the key belongs to the focused text field
	actQuit
	actRedraw
	actUp
	actDown
	actLeft
	actRight
	actPageUp
	actPageDown
	actFirst
	actLast
	actEnter
	actBack
	actBlur // Esc inside a text field
	actNextField
	actPrevField
	actToggle
	actSearchScreen
	actDownload
	actDownloadBest
	actQueue
	actHistory
	actSettings
	actRandom
	actRetry
	actSort
	actNextPage
	actPrevPage
	actPrevEntry
	actNextEntry
	actCancel
	actCancelAll
	actClearFinished
	actFailedOnly
	actExport
	actCover
	actPrefer
	actCopy
	actHelp
	actYes
	actNo
)

// normalize turns Bubble Tea's key string into the form the table uses: shift+<letter> is the capital.
func normalize(k string) string {
	if rest, ok := strings.CutPrefix(k, "shift+"); ok && len([]rune(rest)) == 1 {
		return strings.ToUpper(rest)
	}
	return k
}

// reduce maps a key to an action. root is true on the first screen of the stack, where q leaves.
func reduce(sc screenID, focus focusKind, key string, root bool) action {
	key = normalize(key)
	switch key {
	case "ctrl+c":
		return actQuit
	case "ctrl+l":
		return actRedraw
	}
	switch focus {
	case focusPrompt:
		return promptAction(key)
	case focusText:
		return textAction(key)
	}
	if a := screenKey(sc, key); a != actNone { // a screen may take a global key for itself
		return a
	}
	if a, ok := commonKeys[key]; ok {
		return a
	}
	if key == "q" && root {
		return actQuit
	}
	return actNone
}

// promptAction: y confirms; n, Enter and Esc decline (the default is No).
func promptAction(key string) action {
	switch key {
	case "y", "Y":
		return actYes
	case "n", "N", "enter", "esc":
		return actNo
	}
	return actNone
}

// textAction: in a text field only these keys are commands; everything else is typed.
func textAction(key string) action {
	switch key {
	case "esc":
		return actBlur
	case "enter":
		return actEnter
	case "tab":
		return actNextField
	case "shift+tab":
		return actPrevField
	case "up":
		return actUp
	case "down":
		return actDown
	}
	return actText
}

// commonKeys are the global keys of tui.md section 8, valid when no text field has focus.
var commonKeys = map[string]action{
	"up": actUp, "k": actUp, "down": actDown, "j": actDown,
	"left": actLeft, "h": actLeft, "right": actRight, "l": actRight,
	"pgup": actPageUp, "ctrl+b": actPageUp, "pgdown": actPageDown, "ctrl+f": actPageDown,
	"home": actFirst, "g": actFirst, "end": actLast, "G": actLast,
	"enter": actEnter, "esc": actBack, "space": actToggle,
	"/": actSearchScreen, "d": actDownload,
	"Q": actQueue, "H": actHistory, "S": actSettings, "?": actHelp,
}

// screenKey holds the letters that mean different things on different screens.
func screenKey(sc screenID, key string) action {
	switch sc {
	case scSearch:
		switch key {
		case "r":
			return actRandom
		case "tab":
			return actNextField
		case "shift+tab":
			return actPrevField
		}
	case scResults:
		switch key {
		case "s":
			return actSort
		case "n", "pgdown":
			return actNextPage
		case "p", "pgup":
			return actPrevPage
		case "r":
			return actRetry
		}
	case scEntry:
		switch key {
		case "r":
			return actRetry
		case "D":
			return actDownloadBest
		case "c":
			return actCover
		case "P":
			return actPrefer
		case "left", "h", "[":
			return actPrevEntry
		case "right", "l", "]":
			return actNextEntry
		}
	case scQueue:
		switch key {
		case "x":
			return actCancel
		case "X":
			return actCancelAll
		case "r":
			return actRetry
		case "c":
			return actClearFinished
		case "y":
			return actCopy
		}
	case scHistory:
		switch key {
		case "f":
			return actFailedOnly
		case "e":
			return actExport
		case "r":
			return actRetry
		}
	case scSettings:
	}
	return actNone
}

// helpKey is one entry of the status bar and of the `?` overlay: the key as shown and the i18n key
// of its label (ui.tui.key.<label>).
type helpKey struct{ keys, label string }

// helpFor lists the keys of a screen, most useful first: the status bar shows the first few.
func helpFor(sc screenID) []helpKey {
	global := []helpKey{{"Q", "queue"}, {"H", "history"}, {"S", "settings"}, {"?", "help"}}
	var own []helpKey
	switch sc {
	case scSearch:
		own = []helpKey{{"enter", "run"}, {"tab", "next_field"}, {"r", "random"}}
	case scResults:
		own = []helpKey{{"↑↓", "move"}, {"space", "mark"}, {"enter", "open"}, {"d", "download_marked"}, {"n/p", "page"}, {"s", "sort"}, {"/", "edit_search"}}
	case scEntry:
		own = []helpKey{{"↑↓", "link"}, {"enter/d", "download_link"}, {"D", "download_best"}, {"c", "cover"}, {"P", "prefer"}, {"←→", "entry"}, {"esc", "back"}}
	case scQueue:
		own = []helpKey{{"↑↓", "move"}, {"x", "cancel"}, {"X", "cancel_all"}, {"r", "retry"}, {"c", "clear_finished"}, {"esc", "back"}}
	case scHistory:
		own = []helpKey{{"↑↓", "move"}, {"f", "failed_only"}, {"/", "filter"}, {"e", "export"}, {"enter", "details"}, {"esc", "back"}}
	case scSettings:
		own = []helpKey{{"↑↓", "move"}, {"enter", "edit"}, {"space", "toggle"}, {"backspace", "reset"}, {"esc", "back"}}
	}
	return append(own, global...)
}
