package gateway

import (
	"errors"
	"net/http"
	"testing"
)

const cloudflare502 = `<!DOCTYPE html><html><head><title>api.x.ai | 502: Bad gateway</title></head><body><div id="cf-error-details">Cloudflare Ray ID: 8abc</div></body></html>`

func TestCloudflareHTML502Classification(t *testing.T) {
	failure := newHTTPUpstreamFailure(http.StatusBadGateway, []byte(cloudflare502), 1, "a")
	if failure.Code != "upstream_cloudflare_error" || failure.AccountScoped || failure.Fingerprint != "502:cloudflare_html" {
		t.Fatalf("unexpected classification: %+v", failure)
	}
	plain := newHTTPUpstreamFailure(http.StatusBadGateway, []byte(`{"error":"x"}`), 1, "a")
	if plain.Code != "upstream_server_error" {
		t.Fatalf("JSON 502 must stay generic, got %s", plain.Code)
	}
	fingerprints := map[string]int{}
	if shouldStopForNonAccountFingerprint(fingerprints, failure) {
		t.Fatal("first cloudflare hit should still allow one retry")
	}
	if !shouldStopForNonAccountFingerprint(fingerprints, failure) {
		t.Fatal("second cloudflare hit must fail fast")
	}
}

func TestHTTP2ProtocolErrorClassification(t *testing.T) {
	err := errors.New("stream error: stream ID 5; PROTOCOL_ERROR; received from peer")
	failure := newTransportUpstreamFailure(err, 1, "a")
	if failure.Code != "upstream_http2_protocol_error" || failure.HTTPStatus != http.StatusBadGateway || failure.AccountScoped {
		t.Fatalf("unexpected classification: %+v", failure)
	}
	fingerprints := map[string]int{}
	shouldStopForNonAccountFingerprint(fingerprints, failure)
	if !shouldStopForNonAccountFingerprint(fingerprints, failure) {
		t.Fatal("second PROTOCOL_ERROR must fail fast")
	}
}
