// Command tui is a terminal dashboard for the GARAGE backend: it reads the HTTP API and shows
// sessions and their analysis/coaching. Interactive by default (bubbletea); --once renders a snapshot
// to stdout and exits (scriptable, no TTY needed).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/arush-sal/garage/backend/rtps/tui"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	api := flag.String("api", "http://localhost:8080", "backend HTTP API base URL")
	once := flag.Bool("once", false, "render a snapshot to stdout and exit (no interactive UI)")
	sessionID := flag.String("session", "", "with --once, also render this session's report")
	flag.Parse()

	client := tui.NewClient(*api)

	if *once {
		if err := runOnce(client, *sessionID); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}
	if _, err := tea.NewProgram(newModel(client), tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "tui:", err)
		os.Exit(1)
	}
}

// runOnce prints the session list (and optionally one session's report) once.
func runOnce(client *tui.Client, sessionID string) error {
	ctx := context.Background()
	sessions, err := client.Sessions(ctx)
	if err != nil {
		return err
	}
	fmt.Println(tui.RenderSessions(sessions, -1))
	if sessionID != "" {
		rep, err := client.Coaching(ctx, sessionID)
		if err != nil {
			return err
		}
		fmt.Println()
		fmt.Println(tui.RenderReport(rep))
	}
	return nil
}
