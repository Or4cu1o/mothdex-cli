package update

import (
	"context"
	"crypto/tls"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
)

const origin = "https://github.com/Or4cu1o/mothdex-cli/releases"

var errRedirect = errors.New("untrusted update redirect")

func allowedURL(u *url.URL) bool {
	if u.Scheme != "https" || u.User != nil || u.Fragment != "" {
		return false
	}
	switch u.Host {
	case "github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com":
		return true
	}
	return false
}
func newClient(in *http.Client) *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	c := &http.Client{Transport: tr}
	if in != nil {
		*c = *in
	}
	c.Jar = nil
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 || !allowedURL(req.URL) {
			return errRedirect
		}
		req.Header = make(http.Header)
		req.Header.Set("User-Agent", "mothdex")
		return nil
	}
	return c
}
func (u *Updater) request(ctx context.Context, path string, limit int64, latest bool) ([]byte, string, error) {
	c := *u.o.Client
	if latest {
		c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, origin+path, nil)
	if err != nil {
		return nil, "", failure(app.CodeNetwork, "request", err)
	}
	req.Header.Set("User-Agent", "mothdex")
	resp, err := c.Do(req)
	if err != nil {
		code := app.CodeNetwork
		if errors.Is(err, errRedirect) {
			code = app.CodeUntrustedRedirect
		}
		return nil, "", failure(code, "download", err)
	}
	defer resp.Body.Close()
	if latest {
		if resp.StatusCode != http.StatusFound {
			return nil, "", failure(app.CodeNetwork, "latest_status", nil)
		}
		return nil, resp.Header.Get("Location"), nil
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, "", failure(app.CodeNotFound, "asset_missing", nil)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", failure(app.CodeNetwork, "asset_status", fmt.Errorf("HTTP %d", resp.StatusCode))
	}
	if resp.ContentLength > limit {
		return nil, "", failure("size_mismatch", "body_limit", nil)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if int64(len(b)) > limit || (resp.ContentLength >= 0 && resp.ContentLength != int64(len(b))) {
		return nil, "", failure("size_mismatch", "body_length", err)
	}
	if err != nil {
		return nil, "", failure(app.CodeNetwork, "read_body", err)
	}
	return b, "", nil
}
func releaseURL(link string, stable bool) (Release, error) {
	v, ok := strings.CutPrefix(link, origin+"/tag/v")
	if !ok || !validVersion(v) || (stable && strings.Contains(strings.Split(v, "+")[0], "-")) {
		return Release{}, failure("integrity", "release_tag", nil)
	}
	return Release{Version: v, Tag: "v" + v, PageURL: link, Prerelease: strings.Contains(strings.Split(v, "+")[0], "-")}, nil
}

// Latest discovers stable from the unfollowed Location and beta from the public Atom feed.
func (u *Updater) Latest(ctx context.Context) (Release, error) {
	_, loc, err := u.request(ctx, "/latest", 0, true)
	if err != nil {
		return Release{}, err
	}
	best, err := releaseURL(loc, true)
	if err != nil {
		return Release{}, err
	}
	if u.o.Channel == "stable" {
		return best, nil
	}
	b, _, err := u.request(ctx, ".atom", 1<<20, false)
	if err != nil {
		return Release{}, err
	}
	var feed struct {
		Entries []struct {
			Links []struct {
				Href string `xml:"href,attr"`
			} `xml:"link"`
		} `xml:"entry"`
	}
	if err = xml.Unmarshal(b, &feed); err != nil {
		return Release{}, failure("integrity", "atom", err)
	}
	for _, e := range feed.Entries {
		for _, l := range e.Links {
			r, e := releaseURL(l.Href, false)
			if e == nil && compare(r.Version, best.Version) > 0 {
				best = r
			}
		}
	}
	return best, nil
}
