package wrapper

import (
	"encoding/json"
	"html"
	"net/http"
	"net/url"
	"strings"
)

// OGame's support SSO leaves the game server intentionally. Keep this exception
// separate from session redirects and never follow it with the game's client.
func isSupportRedirect(method string, vals url.Values, response requestResponse) bool {
	if method != http.MethodGet || vals.Get("page") != "ingame" || vals.Get("component") != "support" ||
		(response.statusCode != http.StatusFound && response.statusCode != http.StatusSeeOther) {
		return false
	}
	target, err := url.Parse(response.location)
	return err == nil && strings.EqualFold(target.Scheme, "https") && target.User == nil &&
		strings.EqualFold(target.Hostname(), "ogame.support.gameforge.com") &&
		(target.Port() == "" || target.Port() == "443") && target.Path == "/index.php"
}

// Match the legacy support endpoint's browser-side navigation contract. JSON
// encoding escapes script delimiters as well as quotes in the SSO URL; do not
// log this URL because its query can contain a one-time authentication key.
func supportRedirectPage(location string) []byte {
	encoded, _ := json.Marshal(location)
	return []byte("<script>document.location.href=" + string(encoded) + ";</script>" +
		"<noscript><a href=\"" + html.EscapeString(location) + "\">Open OGame support</a></noscript>")
}
