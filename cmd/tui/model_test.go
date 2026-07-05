package main

import (
	"testing"

	"github.com/arush-sal/garage/backend/rtps/tui"
	tea "github.com/charmbracelet/bubbletea"
)

func keyMsg(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestModel_LiveMsgUpdatesAndStaysLive(t *testing.T) {
	m := newLiveModel(nil, nil)
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
	m := newLiveModel(nil, nil)
	nm, _ := m.handleKey(keyMsg("s"))
	if nm.(model).view != viewList {
		t.Errorf("'s' should switch to viewList")
	}
}
