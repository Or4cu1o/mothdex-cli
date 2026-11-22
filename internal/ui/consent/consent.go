// Package consent reads the "Download anyway" state of a job the same way for every UI: a download that
// stopped with confirmation_required is not a failure, it waits for a person to accept the real size
// (downloader.md 3.2 step 6). The consent is the exact size shown, never a blanket one.
package consent

import (
	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/proxy"
)

// Needed reports whether the job stopped because its real size needs a person's OK. A job whose real size is
// not known has nothing to accept, so it is an ordinary failure: the service always writes the size into the
// error, and this keeps a UI from offering a consent it could not state.
func Needed(j app.JobSnapshot) bool {
	return j.State == app.StateFailed && j.Err != nil && j.Err.Code == string(proxy.CodeConfirmationRequired) && Size(j) > 0
}

// Size is the real size the person is asked to accept: the Content-Length the proxy reported, which the service
// puts in the error as "size". It is 0 when the error does not carry it: the catalog's size (Expected) is not
// what the downloader stopped on, so it is never offered in its place.
func Size(j app.JobSnapshot) int64 {
	if j.Err == nil {
		return 0
	}
	if n, ok := j.Err.Args["size"].(int64); ok && n > 0 {
		return n
	}
	return 0
}

// Dir is the folder the file would be saved to, as the service wrote it into the error, "" if unknown.
func Dir(j app.JobSnapshot) string {
	if j.Err == nil {
		return ""
	}
	s, _ := j.Err.Args["path"].(string)
	return s
}

// WithConsent is the original request asking again with the consent for exactly the size shown. A job
// whose size is not known (0) gets no consent, so it would stop again rather than download unasked.
func WithConsent(r app.DownloadRequest, j app.JobSnapshot) app.DownloadRequest {
	r.SizeConsent = Size(j)
	return r
}

// Args are the values the "Download anyway" texts share, and the suffix of their keys: "" when the catalog had a
// size for the file, "_unknown" when it had none. human formats a size in the UI's own way; the caller adds what
// is its own (the name, and the folder cleaned and shortened as its screen needs).
func Args(j app.JobSnapshot, human func(int64) string) (suffix string, args map[string]any) {
	args = map[string]any{"size": human(Size(j))}
	if j.CatalogSize > 0 {
		args["estimate"] = human(j.CatalogSize)
		return "", args
	}
	return "_unknown", args
}
