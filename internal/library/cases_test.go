package library

// T-16 catalog case 44: the exact refresh URLs of the catalog, through the validator the
// profile loader and Refresh both use.

import "testing"

func TestCase44RefreshURLPolicyCatalogURLs(t *testing.T) {
	for _, u := range []string{
		"http://localhost/reloadgames", "http://localhost:1234/reloadgames", "http://127.0.0.1.nip.io:1234/reloadgames",
		"http://192.168.1.10:1234/reloadgames", "https://127.0.0.1:1234/reloadgames", "http://127.0.0.1:1234@evil.example/reloadgames",
		"http://evil.example#@127.0.0.1:1234/reloadgames", "http://127.0.0.1:1234/reloadgames/../x",
	} {
		if ValidateRefreshURL(u) == nil {
			t.Errorf("%q must be rejected", u)
		}
	}
	for _, u := range []string{"http://127.0.0.1:1234/reloadgames", "http://[::1]:1234/reloadgames"} {
		if err := ValidateRefreshURL(u); err != nil {
			t.Errorf("%q: %v", u, err)
		}
	}
}
