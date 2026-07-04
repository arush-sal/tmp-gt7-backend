package tui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClient_Sessions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/sessions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"id":"s1","car_id":1384,"status":"finished","current_lap":3,"total_laps":2}]`))
	}))
	defer srv.Close()

	got, err := NewClient(srv.URL).Sessions(context.Background())
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(got) != 1 || got[0].ID != "s1" || got[0].CarID != 1384 {
		t.Errorf("sessions = %+v", got)
	}
}

func TestClient_Coaching(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"analysis":{"session_id":"s1","has_pb":true,"pb_lap":2},"coaching":{"status":"pending","note":"x"}}`))
	}))
	defer srv.Close()

	rep, err := NewClient(srv.URL).Coaching(context.Background(), "s1")
	if err != nil {
		t.Fatalf("Coaching: %v", err)
	}
	if rep.Analysis.SessionID != "s1" || rep.Analysis.PBLap != 2 || rep.Coaching.Status != "pending" {
		t.Errorf("report = %+v", rep)
	}
}

func TestClient_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"session not found"}`))
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL).Coaching(context.Background(), "nope")
	if err == nil {
		t.Fatal("want error for 404, got nil")
	}
	if !strings.Contains(err.Error(), "session not found") {
		t.Errorf("error = %q, want it to surface the API error body", err)
	}
}
