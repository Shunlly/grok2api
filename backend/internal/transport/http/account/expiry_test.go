package account

import (
	"testing"
	"time"
)

func TestCredentialExpiry(t *testing.T) {
	at := time.Now()
	if src, auto := credentialExpiry(accountResponse{AuthType: "sso"}); src != "" || auto {
		t.Fatalf("sso unknown = %q %v", src, auto)
	}
	if src, auto := credentialExpiry(accountResponse{AuthType: "oauth", ExpiresAt: &at, Refreshable: true}); src != "oauthAccessToken" || !auto {
		t.Fatalf("oauth refreshable = %q %v", src, auto)
	}
	if _, auto := credentialExpiry(accountResponse{AuthType: "oauth", ExpiresAt: &at, Refreshable: true, RefreshFailures: 2}); auto {
		t.Fatal("refresh failing must not be autoRenew")
	}
	if src, auto := credentialExpiry(accountResponse{AuthType: "oauth", ExpiresAt: &at}); src != "oauthAccessToken" || auto {
		t.Fatalf("oauth no refresh = %q %v", src, auto)
	}
}
