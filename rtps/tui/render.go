package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/arush-sal/garage/backend/rtps/coach"
	"github.com/charmbracelet/lipgloss"
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Underline(true)
	headerStyle   = lipgloss.NewStyle().Bold(true)
	selectedStyle = lipgloss.NewStyle().Bold(true).Reverse(true)
	dimStyle      = lipgloss.NewStyle().Faint(true)
)

// RenderSessions renders the session list with the selected row highlighted.
func RenderSessions(sessions []SessionSummary, selected int) string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("GARAGE — Sessions") + "\n\n")
	if len(sessions) == 0 {
		b.WriteString(dimStyle.Render("No sessions yet. Stream telemetry into the ingest server.") + "\n")
		return b.String()
	}
	for i, s := range sessions {
		elapsed := "—" // active/incomplete session: EndTime not set yet
		if s.EndTime.After(s.StartTime) {
			elapsed = dur(s.EndTime.Sub(s.StartTime))
		}
		line := fmt.Sprintf("%-14s car %-6d %-9s laps %d/%d  %s",
			s.ID, s.CarID, s.Status, s.CurrentLap, s.TotalLaps, elapsed)
		if i == selected {
			b.WriteString(selectedStyle.Render("> "+line) + "\n")
		} else {
			b.WriteString("  " + line + "\n")
		}
	}
	b.WriteString("\n" + dimStyle.Render("up/down: select · enter: analyze · r: refresh · q: quit"))
	return b.String()
}

// RenderReport renders a session's coaching report: PB, theoretical best, consistency, per-lap and
// per-sector deltas, and recommendations.
func RenderReport(rep coach.Report) string {
	a := rep.Analysis
	var b strings.Builder
	b.WriteString(titleStyle.Render("Session "+a.SessionID) + "\n\n")

	if !a.HasPB {
		b.WriteString(dimStyle.Render("No valid lap — no Personal Best to analyze.") + "\n\n")
	} else {
		b.WriteString(headerStyle.Render("Personal Best") + fmt.Sprintf("  lap %d  %s\n", a.PBLap, lap(a.PBTime)))
		if a.TheoreticalBest > 0 {
			b.WriteString(headerStyle.Render("Theoretical best") + fmt.Sprintf("  %s  (%s on the table)\n",
				lap(a.TheoreticalBest), delta(a.TimeOnTable)))
		}
		c := a.Consistency
		b.WriteString(headerStyle.Render("Consistency") + fmt.Sprintf("  %d laps · best %s · mean %s · spread %s · sd %s\n\n",
			c.Count, lap(c.Best), lap(c.Mean), dur(c.Spread), dur(c.StdDev)))

		b.WriteString(headerStyle.Render("Laps") + "\n")
		for _, l := range a.Laps {
			tag := ""
			if l.LapNumber == a.PBLap {
				tag = dimStyle.Render(" (PB)")
			}
			fmt.Fprintf(&b, "  lap %-2d  %s  %s%s\n", l.LapNumber, lap(l.Duration), delta(l.DeltaToPB), tag)
			for _, sd := range l.SectorDeltas {
				b.WriteString(dimStyle.Render(fmt.Sprintf("        S%d %s\n", sd.Index, delta(sd.DeltaToPB))))
			}
		}
	}

	if len(a.Recommendations) > 0 {
		b.WriteString("\n" + headerStyle.Render("Recommendations") + "\n")
		for _, r := range a.Recommendations {
			b.WriteString("  • " + r + "\n")
		}
	}
	b.WriteString("\n" + dimStyle.Render(fmt.Sprintf("coaching: %s", rep.Coaching.Status)))
	b.WriteString("\n" + dimStyle.Render("esc: back · q: quit"))
	return b.String()
}

// lap formats a lap time as M:SS.mmm.
func lap(d time.Duration) string {
	if d <= 0 {
		return "—"
	}
	return fmt.Sprintf("%d:%06.3f", int(d/time.Minute), (d % time.Minute).Seconds())
}

// delta formats a signed delta in seconds (+X.XXXs).
func delta(d time.Duration) string { return fmt.Sprintf("%+.3fs", d.Seconds()) }

// dur formats a plain duration in seconds.
func dur(d time.Duration) string { return fmt.Sprintf("%.3fs", d.Seconds()) }
