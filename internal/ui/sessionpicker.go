// Package ui — sessionpicker.go: ModeSessionPicker.
//
// Modal overlay for picking a pi session to resume. Opened from
// ModeNormal with P. Shows a list of recent pi sessions, supports
// substring filter, and on Enter creates a new ticket for the
// selected session and triggers the spawn flow
// (prepareSpawn → spawnReadyMsg → terminal.Pane.Start).
//
// This replaces the dead PiPane-based resume path. Resume now
// goes through the same spawn flow as new tickets —
// the only difference is the --session flag pointing at the
// saved session ID.
package ui

import (
	"fmt"
	"os"

	"github.com/pi/awp/internal/project"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/pi/awp/internal/board"
	"github.com/pi/awp/internal/observability"
	"github.com/pi/awp/internal/pi"
	)

// openSessionPicker switches to picker mode and starts a background
// scan for sessions. Sessions are scanned once; the model renders
// whatever has been loaded. Filter is applied client-side as the
// user types.
func (m *Model) openSessionPicker() tea.Cmd {
	observability.Debug("Model.openSessionPicker")
	m.mode = ModeSessionPicker
	m.pickerFilter = ""
	m.pickerIndex = 0
	m.pickerLoading = true
	m.pickerErr = nil
	m.pickerSessions = nil

	// Lazy-init the session store. Cached for the picker lifetime.
	return m.scanSessionsCmd()
}

// scanSessionsCmd returns a Cmd that scans the pi session store.
// Reuses pi.NewSessionStore with the user's HOME (default).
func (m *Model) scanSessionsCmd() tea.Cmd {
	home, _ := os.UserHomeDir()
	store := pi.NewSessionStore(home)
	return func() tea.Msg {
		sessions, err := store.List(home)
		return sessionsLoadedMsg{sessions: sessions, err: err}
	}
}

type sessionsLoadedMsg struct {
	sessions []pi.SessionInfo
	err      error
}

// handleSessionPickerMode handles key events while in ModeSessionPicker.
func (m *Model) handleSessionPickerMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.pickerLoading {
		// While loading, only allow Esc/Ctrl+G to dismiss.
		if msg.String() == "esc" || msg.String() == "ctrl+g" {
			m.mode = ModeNormal
		}
		return m, nil
	}

	// Filter is applied client-side via filteredPickerSessions() — the
	// function recomputes the filter from m.pickerSessions on each call.
	// Pickers stay under ~1000 sessions in practice, so O(n) per keystroke
	// is fine. Use Enter to select, Esc to cancel.
	switch msg.String() {
	case "esc", "ctrl+g":
		m.mode = ModeNormal
		m.pickerFilter = ""
		m.pickerIndex = 0
		return m, nil

	case "enter":
		sessions := m.filteredPickerSessions()
		if m.pickerIndex < 0 || m.pickerIndex >= len(sessions) {
			m.notify("No session selected")
			return m, nil
		}
		sel := sessions[m.pickerIndex]
		m.pickerFilter = ""
		m.notify("Resuming session " + truncateID(sel.ID, 12))
		return m, m.resumeSessionCmd(sel)

	case "down":
		max := len(m.filteredPickerSessions()) - 1
		if m.pickerIndex < max {
			m.pickerIndex++
		}
		return m, nil

	case "up":
		if m.pickerIndex > 0 {
			m.pickerIndex--
		}
		return m, nil

	case "pgdown":
		m.pickerIndex += 10
		max := len(m.filteredPickerSessions()) - 1
		if m.pickerIndex > max {
			m.pickerIndex = max
		}
		return m, nil

	case "pgup":
		m.pickerIndex -= 10
		if m.pickerIndex < 0 {
			m.pickerIndex = 0
		}
		return m, nil
	}

	// Add to filter on alphanumeric + safe punctuation only (cap at 64 chars)
	if len(msg.Runes) == 1 && isFilterChar(msg.Runes[0]) && len(m.pickerFilter) < 64 {
		m.pickerFilter += string(msg.Runes)
		m.pickerIndex = 0
		return nil, nil
	}
	// Backspace
	if msg.String() == "backspace" && len(m.pickerFilter) > 0 {
		m.pickerFilter = m.pickerFilter[:len(m.pickerFilter)-1]
		m.pickerIndex = 0
		return m, nil
	}

	return m, nil
}

// resumeSessionCmd creates a ticket for the picked session and
// triggers the spawn flow (m.spawnAgent).
//
// This is the bug fix: previously this called spawnPiWithSession
// which used m.piFactory (always nil in production), so resume
// silently failed with "Pi factory returned nil".
func (m *Model) resumeSessionCmd(info pi.SessionInfo) tea.Cmd {
	observability.Debug("Model.resumeSessionCmd", "session", info.ID)

	// Idempotency: if a ticket already exists for this session,
	// reuse it.
	for _, t := range m.globalStore.All() {
		if t.AgentSessionID == info.ID {
			m.notify("Already resumed: " + truncateID(string(t.ID), 8))
			if t.Status != board.StatusInProgress {
				t.Status = board.StatusInProgress
				if err := m.globalStore.Save(t); err != nil {
					m.notify("Save failed: " + err.Error())
				}
			}
			m.focusedPane = t.ID
			m.refreshColumnTickets()
			return m.spawnAgentCmd(t.ID)
		}
	}

	projects := m.projectRegistry.List()
	if len(projects) == 0 {
		m.notify("No project. Run: awp project new <name>")
		return nil
	}

	// Match session's CWD against registered projects.
	projectID := matchProjectByCWD(projects, info.CWD)
	t := board.NewTicket("Resume "+truncateID(info.ID, 8), info.FirstPrompt)
	t.ProjectID = projectID
	t.AgentSessionID = info.ID
	t.Status = board.StatusInProgress
	t.PiState = board.PiStateIdle
	if err := m.globalStore.Add(t); err != nil {
		m.notify("Add ticket failed: " + err.Error())
		return nil
	}
	if err := m.globalStore.Save(t); err != nil {
		m.notify("Save ticket failed: " + err.Error())
		return nil
	}
	m.refreshColumnTickets()
	m.focusedPane = t.ID
	m.notify("Resumed session " + truncateID(info.ID, 8) + " (press 'a' to attach)")
	return m.spawnAgentCmd(t.ID)
}

// spawnAgentCmd returns a tea.Cmd that delegates to m.spawnAgent
// for the given ticket. Mirrors the spawn flow but
// accepts an explicit ticketID (so we can resume sessions that
// the user just created without requiring them to navigate to it).
//
// We pre-fill m.activeColumn / m.activeTicket so m.spawnAgent's
// m.selectedTicket() returns the right ticket.
func (m *Model) spawnAgentCmd(ticketID board.TicketID) tea.Cmd {
	return func() tea.Msg {
		// Find ticket in column cache and select it
		for colIdx, tickets := range m.columnTickets {
			for rowIdx, t := range tickets {
				if t.ID == ticketID {
					m.activeColumn = colIdx
					m.activeTicket = rowIdx
					_, cmd := m.spawnAgent()
					return cmd
				}
			}
		}
		m.notify("Ticket not found in column cache")
		return nil
	}
}

func (m *Model) filteredPickerSessions() []pi.SessionInfo {
	out := make([]pi.SessionInfo, 0, len(m.pickerSessions))
	for _, s := range m.pickerSessions {
		if m.pickerFilter == "" || strings.Contains(strings.ToLower(s.CWD), strings.ToLower(m.pickerFilter)) {
			out = append(out, s)
		}
	}
	return out
}

// matchProjectByCWD picks the best project for a given working
// directory. Falls back to the first project if no match.
func matchProjectByCWD(projects []*project.Project, cwd string) string {
	absCWD, _ := filepath.Abs(cwd)
	for _, p := range projects {
		if p.RepoPath == absCWD {
			return p.ID
		}
	}
	// Try prefix match (session's CWD might be a subdir)
	for _, p := range projects {
		if strings.HasPrefix(absCWD, p.RepoPath) {
			return p.ID
		}
	}
	if len(projects) > 0 {
		return projects[0].ID
	}
	return ""
}

// isFilterChar returns true if the rune is safe to include in a
// filter string (alphanumeric + a few safe symbols).
func isFilterChar(r rune) bool {
	if r >= 'a' && r <= 'z' {
		return true
	}
	if r >= 'A' && r <= 'Z' {
		return true
	}
	if r >= '0' && r <= '9' {
		return true
	}
	switch r {
	case '.', '/', '-', '_', '@':
		return true
	}
	return false
}

// truncateID returns the first n characters of s, with "…" if
// truncated. Used in the picker UI for compact session display.
func truncateID(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n < 2 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

// renderSessionPickerView renders the session picker overlay.
func (m *Model) renderSessionPickerView() string {
	var b strings.Builder

	titleStyle := lipgloss.NewStyle().
		Foreground(m.colors.primary).
		Bold(true).
		Padding(0, 1)

	filterStyle := lipgloss.NewStyle().
		Foreground(m.colors.info)

	b.WriteString(titleStyle.Render("Resume pi session"))
	b.WriteString("\n\n")

	if m.pickerLoading {
		b.WriteString("Loading sessions…")
		return b.String()
	}
	if m.pickerErr != nil {
		errStyle := lipgloss.NewStyle().Foreground(m.colors.err)
		b.WriteString(errStyle.Render("Error: " + m.pickerErr.Error()))
		return b.String()
	}

	sessions := m.filteredPickerSessions()
	b.WriteString(filterStyle.Render(fmt.Sprintf("Filter: %s", m.pickerFilter)))
	b.WriteString(fmt.Sprintf("  (%d/%d sessions)\n\n", len(sessions), len(m.pickerSessions)))

	if len(sessions) == 0 {
		b.WriteString("No sessions match.\n")
		b.WriteString("Press Esc to cancel.")
		return b.String()
	}

	// Show up to 20 sessions around pickerIndex
	const visible = 20
	start := m.pickerIndex - visible/2
	if start < 0 {
		start = 0
	}
	end := start + visible
	if end > len(sessions) {
		end = len(sessions)
	}
	if end-start < visible && start > 0 {
		start = max(end-visible, 0)
	}

	for i := start; i < end; i++ {
		s := sessions[i]
		cursor := "  "
		style := lipgloss.NewStyle()
		if i == m.pickerIndex {
			cursor = "▶ "
			style = style.Foreground(m.colors.primary).Bold(true)
		}
		line := fmt.Sprintf("%s%s  %s",
			cursor,
			truncateID(s.ID, 12),
			s.CWD,
		)
		b.WriteString(style.Render(line))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	hintStyle := lipgloss.NewStyle().Foreground(m.colors.muted)
	b.WriteString(hintStyle.Render("↑/↓: navigate  Enter: resume  Esc: cancel"))

	return b.String()
}
