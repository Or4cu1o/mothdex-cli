// Package consent reads the "Download anyway" state of a job the same way for every UI: a download that
// stopped with confirmation_required is not a failure, it waits for a person to accept the real size
// (downloader.md 3.2 step 6). The consent is the exact size shown, never a blanket one.
package consent

import (
	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/proxy"
)

// Needed reports whether the job stopped because its real size needs a person's OK.
func Needed(j app.JobSnapshot) bool {
	return j.State == app.StateFailed && j.Err != nil && j.Err.Code == string(proxy.CodeConfirmationRequired)
}

// Size is the real size the person is asked to accept: the Content-Length the proxy reported, which the
// service puts in the error; the snapshot's Expected is the fallback.
func Size(j app.JobSnapshot) int64 {
	if j.Err != nil {
		if n, ok := j.Err.Args["size"].(int64); ok && n > 0 {
			return n
		}
	}
	return j.Expected
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
