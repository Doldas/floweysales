package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTrackerOperationsCapability(t *testing.T) {
	var started bool
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+strings.Repeat("a", 32) {
			t.Error("missing capability token")
		}
		if r.Method == "POST" {
			started = true
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["symbol"] != "NVDA" || body["provider"] != "YAHOO" {
				t.Errorf("unexpected payload %v", body)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"trackers":[]}`))
	}))
	defer backend.Close()
	tools := &HTTPTrackerOperations{BaseURL: backend.URL, Token: strings.Repeat("a", 32), HTTP: backend.Client()}
	if _, err := tools.ExecuteTracker(context.Background(), "tracker_list", "{}"); err != nil {
		t.Fatal(err)
	}
	if _, err := tools.ExecuteTracker(context.Background(), "tracker_start", `{"symbol":"NVDA","provider":"YAHOO"}`); err != nil {
		t.Fatal(err)
	}
	if !started {
		t.Fatal("tracker start not sent")
	}
	// Which providers are installed is the backend's call; malformed IDs never leave the client.
	started = false
	if _, err := tools.ExecuteTracker(context.Background(), "tracker_start", `{"symbol":"NVDA","provider":"../yahoo"}`); err == nil || started {
		t.Fatal("malformed provider accepted")
	}
}

func TestTrackerToolsDisabledByDefault(t *testing.T) {
	server := New(nil)
	for _, tool := range server.listTools() {
		if strings.HasPrefix(tool.Name, "tracker_") {
			t.Fatalf("unexpected privileged tool %q", tool.Name)
		}
	}
}
