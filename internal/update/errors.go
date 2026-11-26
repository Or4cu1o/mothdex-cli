package update

import "github.com/Or4cu1o/mothdex-cli/internal/app"

// Update errors retain their stable code/reason while both front ends use these
// message and hint keys instead of catalog-download guidance or internal reasons.
func failure(code, reason string, cause error) *app.Error {
	family, message := errorMessage(code, reason)
	hint := "update.unchanged_hint"
	switch {
	case family == "signature":
		hint = "update.tamper_hint"
	case code == app.CodeUpdateUnavailable:
		hint = "errors.update_unavailable.hint"
	case code == app.CodeUpdateFailed && reason == "gui_missing":
		hint = "errors.update_failed.hint"
	}
	return &app.Error{Code: code, Message: message, Cause: cause, Args: map[string]any{
		"reason": reason, "restored": true, "install_command": InstallCommand(),
		"message_key": "update.errors." + family + ".message", "hint_key": hint,
	}}
}
func unavailable(reason string, cause error) error {
	return failure(app.CodeUpdateUnavailable, reason, cause)
}

func errorMessage(code, reason string) (family, message string) {
	if code == "integrity" && reason == "signature" {
		return "signature", "The release signature could not be verified."
	}
	switch code {
	case "integrity":
		return "integrity", "The update files failed integrity verification."
	case "size_mismatch":
		return "size_mismatch", "The update files have an invalid size."
	case app.CodeNotFound:
		return "not_found", "The requested update files were not found."
	case app.CodeUntrustedRedirect:
		return "untrusted_redirect", "The update was redirected to an untrusted address."
	case app.CodeNetwork:
		return "network", "The update server could not be reached or the transfer was interrupted."
	case app.CodeUpdateUnavailable:
		return "update_unavailable", "This installation cannot be updated automatically."
	case app.CodeUsage:
		return "usage", "The update options are invalid."
	case app.CodeCanceled:
		return "canceled", "The update was canceled."
	case app.CodeConfirmationNeeded:
		return "confirmation_required", "The update needs your confirmation."
	default:
		return "update_failed", "The update could not be completed."
	}
}
