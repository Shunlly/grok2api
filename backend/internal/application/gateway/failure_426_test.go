package gateway

import (
	"net/http"
	"strings"
	"testing"
)

func TestUpgradeRequiredIsClearRequestScopedFailure(t *testing.T) {
	body := []byte(`{"error":"Your Grok CLI version (1.0.4) is outdated. Please update to version 1.0.13 or later"}`)
	failure := newHTTPUpstreamFailure(http.StatusUpgradeRequired, body, 1, "a")
	if failure.Code != "upstream_client_outdated" || !failure.RequestScopedForbidden || failure.AccountScoped {
		t.Fatalf("unexpected failure: %#v", failure)
	}
	if !strings.Contains(failure.PublicMessage, "ClientVersion") || !strings.Contains(failure.PublicMessage, "1.0.13") {
		t.Fatalf("message = %q", failure.PublicMessage)
	}
}
