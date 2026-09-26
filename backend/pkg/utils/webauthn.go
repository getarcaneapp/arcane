package utils

import (
	"slices"

	"github.com/go-webauthn/webauthn/protocol"
)

// NormalizePasskeyAssertionExtensions drops an unrequested appid=false output, which Safari
// reports even though it means the legacy AppID was not used.
func NormalizePasskeyAssertionExtensions(session protocol.SessionExtensions, assertion *protocol.ParsedCredentialAssertionData) {
	if assertion == nil {
		return
	}
	appID := assertion.ClientExtensionResults.AppID
	if appID == nil || *appID {
		return
	}
	if session.AppID != "" || slices.Contains(session.Requested, protocol.ExtensionAppID) {
		return
	}
	assertion.ClientExtensionResults.AppID = nil
}
