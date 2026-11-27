package update

import (
	"errors"
	"strings"
	"testing"

	"github.com/Or4cu1o/mothdex-cli/internal/app"
	"github.com/Or4cu1o/mothdex-cli/internal/i18n"
	"github.com/Or4cu1o/mothdex-cli/locales"
)

func TestUpdaterErrorsUseLocalizedUpdateMessagesAndHints(t *testing.T) {
	bundle, err := i18n.Load(locales.FS)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ code, reason, family, hint string }{
		{"integrity", "checksum_mismatch", "integrity", "update.unchanged_hint"},
		{"size_mismatch", "expanded_limit", "size_mismatch", "update.unchanged_hint"},
		{app.CodeNotFound, "asset_missing", "not_found", "update.unchanged_hint"},
		{app.CodeUntrustedRedirect, "redirect", "untrusted_redirect", "update.unchanged_hint"},
		{app.CodeNetwork, "timeout", "network", "update.network_hint"},
		{"integrity", "signature", "signature", "update.tamper_hint"},
		{app.CodeUpdateFailed, "check_failed", "update_failed", "update.unchanged_hint"},
		{app.CodeUpdateUnavailable, "no_state", "update_unavailable", "errors.update_unavailable.hint"},
		{app.CodeUsage, "channel", "usage", "errors.usage.hint"},
		{app.CodeCanceled, "canceled", "canceled", "update.unchanged_hint"},
		{app.CodeConfirmationNeeded, "confirmation", "confirmation_required", "errors.confirmation_required.hint"},
	} {
		t.Run(tc.family, func(t *testing.T) {
			cause := errors.New("private technical cause")
			e := failure(tc.code, tc.reason, cause)
			key := "update.errors." + tc.family + ".message"
			if e.Code != tc.code || e.Args["reason"] != tc.reason || !errors.Is(e, cause) || e.Args["restored"] != true {
				t.Fatalf("error contract changed: %+v", e)
			}
			if e.Message == tc.reason || e.Message == "" || e.Args["message_key"] != key || e.Args["hint_key"] != tc.hint {
				t.Fatalf("raw or generic updater error: %+v", e)
			}
			for _, locale := range []string{"en_us", "pt_br", "es_es", "ru_ru"} {
				tr := bundle.New(locale)
				if !tr.Has(key) || !tr.Has(tc.hint) {
					t.Fatalf("%s missing updater message/hint", locale)
				}
				message := tr.T(key, i18n.Args(e.Args))
				hint := tr.T(tc.hint, i18n.Args(e.Args))
				if strings.Contains(message, "{") || strings.Contains(hint, "{") || message == tc.reason || strings.Contains(hint, "--link") || strings.Contains(message, cause.Error()) || strings.Contains(hint, cause.Error()) {
					t.Fatalf("%s raw or download fallback: %s / %s", locale, message, hint)
				}
				if locale != "en_us" && message == e.Message {
					t.Fatalf("%s update message fell back to English", locale)
				}
				if locale == "en_us" && message != e.Message {
					t.Fatalf("JSON fallback differs from English message: %q != %q", e.Message, message)
				}
			}
		})
	}
}
