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
	viewList view = iota
	viewDetail
)

// model is the bubbletea state: a session list and a per-session detail view, both fed by the API.
type model struct {
	client   *tui.Client
	view     view
	sessions []tui.SessionSummary
	cursor   int
	report   coach.Report
	loading  bool
	err      error
	reqID    int // increments per fetch; async results with a stale id are ignored
}

func newModel(c *tui.Client) model { return model{client: c, loading: true, reqID: 1} }

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

func (m model) Init() tea.Cmd { return fetchSessions(m.client, m.reqID) }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
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
	if m.view == viewDetail {
		switch k.String() {
		case "esc", "backspace", "left":
			m.view = viewList
		}
		return m, nil
	}
	// list view
	switch k.String() {
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
