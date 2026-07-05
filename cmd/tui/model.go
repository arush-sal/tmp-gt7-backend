package main

import (
	"context"
	"fmt"

	"github.com/arush-sal/garage/backend/rtps/coach"
	"github.com/arush-sal/garage/backend/rtps/tui"
	tea "github.com/charmbracelet/bubbletea"
)

type view int

const (
	viewLive view = iota // default: streaming pit-wall dashboard
	viewList
	viewDetail
)

// model is the bubbletea state: a live pit-wall dashboard (default), a session list, and a
// per-session detail view. The live view is fed by an SSE stream; list/detail are fed by the API.
type model struct {
	client   *tui.Client
	view     view
	sessions []tui.SessionSummary
	cursor   int
	report   coach.Report
	loading  bool
	err      error
	reqID    int // increments per fetch; async results with a stale id are ignored

	snaps  <-chan tui.LiveSnapshot
	latest tui.LiveSnapshot
	width  int
}

// newLiveModel starts in the live dashboard, fed by the snaps channel (nil is allowed for tests).
func newLiveModel(c *tui.Client, snaps <-chan tui.LiveSnapshot) model {
	return model{client: c, view: viewLive, snaps: snaps, reqID: 1, width: 120}
}

// Messages returned by async fetches; id ties a result to the request that started it.
type sessionsMsg struct {
	id       int
	sessions []tui.SessionSummary
	err      error
}
type reportMsg struct {
	id     int
	report coach.Report
	err    error
}

// liveMsg carries one snapshot from the SSE live stream.
type liveMsg struct{ snap tui.LiveSnapshot }

// waitForSnapshot blocks on the next streamed snapshot and delivers it as a liveMsg. Re-armed after
// each liveMsg so the dashboard updates continuously.
func waitForSnapshot(snaps <-chan tui.LiveSnapshot) tea.Cmd {
	if snaps == nil {
		return nil
	}
	return func() tea.Msg {
		s, ok := <-snaps
		if !ok {
			return nil // stream closed (shutdown)
		}
		return liveMsg{snap: s}
	}
}

func fetchSessions(c *tui.Client, id int) tea.Cmd {
	return func() tea.Msg {
		s, err := c.Sessions(context.Background())
		return sessionsMsg{id, s, err}
	}
}

func fetchReport(c *tui.Client, id int, sessionID string) tea.Cmd {
	return func() tea.Msg {
		r, err := c.Coaching(context.Background(), sessionID)
		return reportMsg{id, r, err}
	}
}

func (m model) Init() tea.Cmd { return waitForSnapshot(m.snaps) }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case liveMsg:
		m.latest = msg.snap
		return m, waitForSnapshot(m.snaps) // re-arm
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	case sessionsMsg:
		if msg.id != m.reqID { // superseded by a newer request
			return m, nil
		}
		m.loading = false
		m.sessions, m.err = msg.sessions, msg.err
		if m.cursor >= len(m.sessions) {
			m.cursor = 0
		}
		return m, nil
	case reportMsg:
		if msg.id != m.reqID {
			return m, nil
		}
		m.loading = false
		m.report, m.err = msg.report, msg.err
		if msg.err == nil {
			m.view = viewDetail
		}
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m model) handleKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	}
	if m.view == viewLive {
		switch k.String() {
		case "s":
			m.view = viewList
			m.loading, m.err = true, nil
			m.reqID++
			return m, fetchSessions(m.client, m.reqID)
		}
		return m, nil
	}
	if m.view == viewDetail {
		switch k.String() {
		case "esc", "backspace", "left":
			m.view = viewList
		}
		return m, nil
	}
	// list view
	switch k.String() {
	case "s", "esc":
		m.view = viewLive
		return m, nil
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.sessions)-1 {
			m.cursor++
		}
	case "r":
		m.loading, m.err = true, nil
		m.reqID++
		return m, fetchSessions(m.client, m.reqID)
	case "enter":
		if len(m.sessions) > 0 {
			m.loading, m.err = true, nil
			m.reqID++
			return m, fetchReport(m.client, m.reqID, m.sessions[m.cursor].ID)
		}
	}
	return m, nil
}

func (m model) View() string {
	if m.view == viewLive {
		return tui.RenderLive(m.latest, m.width)
	}
	if m.loading {
		return "loading…  (q to quit)"
	}
	if m.err != nil {
		return fmt.Sprintf("error: %v\n\nr: retry · q: quit", m.err)
	}
	if m.view == viewDetail {
		return tui.RenderReport(m.report)
	}
	return tui.RenderSessions(m.sessions, m.cursor)
}
