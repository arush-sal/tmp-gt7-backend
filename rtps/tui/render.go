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
	errorStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1"))
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
		// Show the human-readable car name (resolved by the collector), truncated to keep the row
		// aligned; the numeric car_id remains available on the JSON API for debugging.
		line := fmt.Sprintf("%-14s %-30.30s %-9s laps %d/%d  %s",
			s.ID, s.CarName, s.Status, s.CurrentLap, s.TotalLaps, elapsed)
		if i == selected {
			b.WriteString(selectedStyle.Render("> "+line) + "\n")
		} else {
			b.WriteString("  " + line + "\n")
		}
	}
	b.WriteString("\n" + dimStyle.Render("up/down: select · enter: analyze · r: refresh · q: quit"))
	return b.String()
}

// RenderReport renders a session's coaching report: PB, predicted best, consistency, per-lap and
// per-sector deltas, and recommendations.
func RenderReport(rep coach.Report) string {
	a := rep.Analysis
	var b strings.Builder
	b.WriteString(titleStyle.Render("Session "+a.SessionID) + "\n\n")

	if !a.HasPB {
		b.WriteString(dimStyle.Render("No valid lap — no Personal Best to analyze.") + "\n\n")
	} else {
		b.WriteString(headerStyle.Render("Personal Best") + "  " + pbLabel(a.PBLap, a.PBTime) + "\n")
		if a.PredictedBest > 0 {
			b.WriteString(headerStyle.Render("Predicted best") + fmt.Sprintf("  %s  (%s on the table)\n",
				lap(a.PredictedBest), delta(a.TimeOnTable)))
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

// dash is the placeholder for a value that is not yet resolved (RKB-deferred or enrichment pending).
const dash = "—"

// RenderLive renders the live pit-wall dashboard from one snapshot. width<=0 uses 120. connErr, if
// non-nil, is a live-stream connection failure (dial failure, non-OK HTTP status). In the idle
// branch it replaces the "waiting" line; in the live branch the last-known frame is still rendered
// (staying useful during a reconnect) but the footer is swapped for a stale-data warning so the
// dashboard never looks healthy while the stream is actually down.
func RenderLive(s LiveSnapshot, width int, connErr error) string {
	if width <= 0 {
		width = 120
	}
	if s.Status != "live" || s.Frame == nil {
		idleLine := dimStyle.Render("Waiting for telemetry… (idle — no live session)")
		if connErr != nil {
			idleLine = errorStyle.Render(fmt.Sprintf("Connection problem (retrying): %v", connErr))
		}
		return titleStyle.Render("GARAGE — Live") + "\n\n" +
			idleLine + "\n\n" +
			dimStyle.Render("s: sessions · q: quit")
	}
	f := s.Frame
	header := lipgloss.NewStyle().MaxWidth(width).Render(liveHeader(s))
	left := leftTelemetry(f)
	right := rightColumn(s)
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", right)
	footer := dimStyle.Render("streaming · s: sessions · q: quit")
	if connErr != nil {
		footer = errorStyle.Render(fmt.Sprintf("⚠ stream lost, reconnecting (showing last data): %v", connErr)) +
			dimStyle.Render("  ·  s: sessions · q: quit")
	}
	return header + "\n\n" + body + "\n\n" + footer
}

func liveHeader(s LiveSnapshot) string {
	f := s.Frame
	pos := dash
	if f.TotalPositions > 0 && f.CurrentPosition > 0 {
		pos = fmt.Sprintf("P%d/%d", f.CurrentPosition, f.TotalPositions)
	}
	predicted := dash
	if a := s.Enrichment.Analysis; a != nil && a.PredictedBest > 0 {
		predicted = lap(a.PredictedBest)
	}
	// DELTA: live delta to the session-best reference lap. Negative = ahead of the reference,
	// positive = behind. Nil (no reference lap yet) falls back to the dash placeholder.
	delt := dash
	if d := s.Enrichment.DeltaToBestNs; d != nil {
		delt = delta(time.Duration(*d))
	}
	return headerStyle.Render(fmt.Sprintf(
		"POS %s   LAP %d/%d   TRACK %s   DELTA %s   PREDICTED %s   BEST %s",
		pos, f.CurrentLap, f.TotalLaps, dash, delt, predicted, lap(f.BestLap())))
}

func leftTelemetry(f *LiveFrame) string {
	var b strings.Builder
	b.WriteString(headerStyle.Render("TELEMETRY") + "\n")
	fmt.Fprintf(&b, "SPEED  %6.1f km/h\n", f.CarSpeed)
	sug := ""
	// GT7 sends suggested_gear as a nibble; 15 = "no suggestion" sentinel. Reverse-engineered protocol fact.
	if f.SuggestedGear > 0 && f.SuggestedGear < 15 && f.SuggestedGear != f.CurrentGear {
		sug = dimStyle.Render(fmt.Sprintf(" (shift %d)", f.SuggestedGear))
	}
	fmt.Fprintf(&b, "GEAR   %d%s\n", f.CurrentGear, sug)
	fmt.Fprintf(&b, "RPM    %5.0f\n", f.Rpm)
	b.WriteString("THROT  " + bar(f.Throttle/100, 20) + "\n")
	b.WriteString("BRAKE  " + bar(f.Brake/100, 20) + "\n")
	fmt.Fprintf(&b, "BOOST  %+.2f\n", f.Boost)
	return b.String()
}

func rightColumn(s LiveSnapshot) string {
	var b strings.Builder
	// Lap timing (direct/derived)
	b.WriteString(headerStyle.Render("LAP") + "\n")
	if s.Derived != nil {
		fmt.Fprintf(&b, "  current %s\n", lap(time.Duration(s.Derived.CurrentLapTimeNs)))
	}
	fmt.Fprintf(&b, "  last    %s\n", lap(s.Frame.LastLap()))
	fmt.Fprintf(&b, "  best    %s\n\n", lap(s.Frame.BestLap()))

	// Tyres (direct)
	b.WriteString(headerStyle.Render("TYRE °C") + "\n")
	fmt.Fprintf(&b, "  FL %3.0f  FR %3.0f\n", s.Frame.TyreTempFL, s.Frame.TyreTempFR)
	fmt.Fprintf(&b, "  RL %3.0f  RR %3.0f\n\n", s.Frame.TyreTempRL, s.Frame.TyreTempRR)

	// Fuel (direct/derived)
	b.WriteString(headerStyle.Render("FUEL") + "\n")
	if s.Derived != nil {
		fmt.Fprintf(&b, "  %.1f L (%.0f%%)\n\n", s.Frame.CurrentFuel, s.Derived.FuelPct)
	} else {
		fmt.Fprintf(&b, "  %.1f L (%s)\n\n", s.Frame.CurrentFuel, dash)
	}

	// Analysis (enrichment — placeholder until it resolves)
	b.WriteString(headerStyle.Render("ANALYSIS") + "\n")
	switch a := s.Enrichment.Analysis; {
	case a == nil: // enrichment not resolved yet
		b.WriteString(dimStyle.Render("  "+dash+" no analysis yet (needs a completed lap)") + "\n")
	case a.HasPB:
		fmt.Fprintf(&b, "  PB %s\n", pbLabel(a.PBLap, a.PBTime))
		for _, r := range a.Recommendations {
			b.WriteString(dimStyle.Render("  • "+r) + "\n")
		}
	default: // resolved, but no valid lap yet — surface the analyzer's own recommendation(s)
		if len(a.Recommendations) > 0 {
			for _, r := range a.Recommendations {
				b.WriteString(dimStyle.Render("  • "+r) + "\n")
			}
		} else {
			b.WriteString(dimStyle.Render("  "+dash+" no valid lap yet") + "\n")
		}
	}
	b.WriteString("\n" + headerStyle.Render("COACHING") + "\n")
	b.WriteString(dimStyle.Render("  pending") + "\n")
	return b.String()
}

// bar renders a 0..1 value as a filled bar of the given width.
func bar(v float32, width int) string {
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	n := int(float32(width)*v + 0.5)
	return "[" + strings.Repeat("#", n) + strings.Repeat("·", width-n) + "]"
}

// pbLabel formats a Personal-Best value with its lap number when concretely known ("lap N  M:SS.mmm"),
// and without it when the lap is unknown ("M:SS.mmm"). PBLap is 0 when the GT7-authoritative best time
// didn't match any captured lap — show the real best time, but omit the lap rather than print "lap 0".
func pbLabel(pbLap int32, pbTime time.Duration) string {
	if pbLap > 0 {
		return fmt.Sprintf("lap %d  %s", pbLap, lap(pbTime))
	}
	return lap(pbTime)
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
