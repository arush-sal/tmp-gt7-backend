package main

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/arush-sal/garage/backend/rtps/tui"
	tea "github.com/charmbracelet/bubbletea"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

func keyMsg(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestModel_LiveMsgUpdatesAndStaysLive(t *testing.T) {
	m := newLiveModel(nil, nil, nil)
	if m.view != viewLive {
		t.Fatalf("default view = %d, want viewLive", m.view)
	}
	nm, _ := m.Update(liveMsg{snap: tui.LiveSnapshot{Status: "live", Frame: &tui.LiveFrame{CarSpeed: 120}}})
	got := nm.(model)
	if got.latest.Frame == nil || got.latest.Frame.CarSpeed != 120 {
		t.Errorf("live snapshot not stored: %+v", got.latest)
	}
	if got.view != viewLive {
		t.Errorf("view changed off live on a liveMsg")
	}
}

func TestModel_ToggleToSessions(t *testing.T) {
	m := newLiveModel(nil, nil, nil)
	nm, _ := m.handleKey(keyMsg("s"))
	if nm.(model).view != viewList {
		t.Errorf("'s' should switch to viewList")
	}
}

// TestModel_LiveErrMsgSetsLastErr guards against a permanent SSE stream failure (backend down,
// 404) being silently discarded — it must be tracked on the model so the view can surface it.
func TestModel_LiveErrMsgSetsLastErr(t *testing.T) {
	m := newLiveModel(nil, nil, nil)
	connErr := errors.New("GET http://localhost:8080/live: 404 Not Found")
	nm, _ := m.Update(liveErrMsg{err: connErr})
	got := nm.(model)
	if got.lastErr == nil || got.lastErr.Error() != connErr.Error() {
		t.Errorf("lastErr = %v, want %v", got.lastErr, connErr)
	}
}

// TestModel_LiveMsgClearsLastErr guards against a stale connection-error banner surviving after
// the stream has recovered — a fresh snapshot means the connection is healthy again.
func TestModel_LiveMsgClearsLastErr(t *testing.T) {
	m := newLiveModel(nil, nil, nil)
	nm, _ := m.Update(liveErrMsg{err: errors.New("boom")})
	m = nm.(model)
	if m.lastErr == nil {
		t.Fatalf("expected lastErr to be set before liveMsg")
	}
	nm, _ = m.Update(liveMsg{snap: tui.LiveSnapshot{Status: "live", Frame: &tui.LiveFrame{CarSpeed: 50}}})
	got := nm.(model)
	if got.lastErr != nil {
		t.Errorf("lastErr = %v, want nil after a fresh snapshot", got.lastErr)
	}
}

// TestModel_View_LiveThenErrShowsReconnecting is the regression test for the "permanent failure
// looks healthy" bug relocated to the live path: once a live frame arrives, m.latest.Status stays
// "live" forever (a later stream error only sets m.lastErr, it does not revert the snapshot). A
// naive View() would keep rendering the plain "streaming" footer even though the connection is
// dead. After a liveMsg followed by a liveErrMsg, View() must surface the connection-error /
// reconnecting text instead of a clean healthy footer.
func TestModel_View_LiveThenErrShowsReconnecting(t *testing.T) {
	m := newLiveModel(nil, nil, nil)
	nm, _ := m.Update(liveMsg{snap: tui.LiveSnapshot{Status: "live", Frame: &tui.LiveFrame{CarSpeed: 120}}})
	m = nm.(model)

	connErr := errors.New("GET http://localhost:8080/live: 404 Not Found")
	nm, _ = m.Update(liveErrMsg{err: connErr})
	m = nm.(model)

	if m.view != viewLive {
		t.Fatalf("view = %d, want viewLive", m.view)
	}
	if m.latest.Status != "live" {
		t.Fatalf("expected stale snapshot to remain Status=live, got %q", m.latest.Status)
	}

	out := stripANSI(m.View())
	if !strings.Contains(out, "reconnecting") {
		t.Errorf("View() after live+err should surface a reconnecting banner, got:\n%s", out)
	}
	if strings.Contains(out, "streaming · s: sessions · q: quit") {
		t.Errorf("View() after live+err should not show the plain healthy footer, got:\n%s", out)
	}
}
