package ws

import (
	"net/http/httptest"
	"testing"
)

func TestSameOrigin(t *testing.T) {
	request := httptest.NewRequest("GET", "https://cloud.example/api/ws", nil)
	request.Host = "cloud.example"
	request.Header.Set("Origin", "https://cloud.example")
	if !sameOrigin(request) {
		t.Fatal("same origin was rejected")
	}

	request.Header.Set("Origin", "https://evil.example")
	if sameOrigin(request) {
		t.Fatal("cross origin was accepted")
	}
}

func TestWebSocketProtocolToken(t *testing.T) {
	request := httptest.NewRequest("GET", "https://cloud.example/api/ws", nil)
	request.Header.Set("Sec-WebSocket-Protocol", "hopper, header.payload.signature")
	if token := websocketProtocolToken(request); token != "header.payload.signature" {
		t.Fatalf("token = %q", token)
	}

	request.Header.Set("Sec-WebSocket-Protocol", "header.payload.signature")
	if token := websocketProtocolToken(request); token != "" {
		t.Fatalf("accepted malformed protocol list: %q", token)
	}
}
