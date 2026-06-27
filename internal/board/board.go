package board

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

var nonAlphanumericRegex = regexp.MustCompile(`[^a-z0-9-]+`)

func Slugify(s string, maxLen int) string {
	if maxLen <= 0 {
		maxLen = 40
	}

	slug := strings.ToLower(s)

	slug = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return '-'
	}, slug)

	slug = nonAlphanumericRegex.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")

	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}

	if len(slug) > maxLen {
		slug = slug[:maxLen]
		slug = strings.TrimRight(slug, "-")
	}

	return slug
}

type TicketID string

func NewTicketID() TicketID {
	return TicketID(uuid.New().String())
}

type TicketStatus string

const (
	StatusBacklog    TicketStatus = "backlog"
	StatusInProgress TicketStatus = "in_progress"
	StatusDone       TicketStatus = "done"
	StatusArchived   TicketStatus = "archived"
)

type AgentStatus string

const (
	AgentNone      AgentStatus = "none"
	AgentIdle      AgentStatus = "idle"
	AgentWorking   AgentStatus = "working"
	AgentWaiting   AgentStatus = "waiting"
	AgentCompleted AgentStatus = "completed"
	AgentError     AgentStatus = "error"
)

// PiState represents the runtime state of a pi session bound to a ticket.
// This is the canonical state type for awp v2 (single-agent: pi only).
// AgentStatus is kept for backward compat with Phase 0 (legacy multi-agent).
type PiState string

const (
	PiStateNone         PiState = "none"          // ticket has no pi bound
	PiStateStarting     PiState = "starting"      // pi process spawning
	PiStateIdle         PiState = "idle"          // pi waiting for prompt
	PiStateStreaming    PiState = "streaming"     // pi generating tokens
	PiStateThinking     PiState = "thinking"      // pi thinking_level reasoning
	PiStateToolCall     PiState = "tool_call"     // pi executing a tool
	PiStateAwaitingUser PiState = "awaiting_user" // pi asking user (confirm/input)
	PiStateCompacting   PiState = "compacting"    // pi compressing context
	PiStateRetrying     PiState = "retrying"      // pi auto-retrying
	PiStateError        PiState = "error"
	PiStateExited       PiState = "exited"        // pi process exited
)

type Ticket struct {
	ID          TicketID     `json:"id"`
	ProjectID   string       `json:"project_id"`
	Title       string       `json:"title"`
	Description string       `json:"description,omitempty"`
	Status      TicketStatus `json:"status"`

	UseWorktree  bool   `json:"use_worktree"`
	WorktreePath string `json:"worktree_path,omitempty"`
	BranchName   string `json:"branch_name,omitempty"`
	BaseBranch   string `json:"base_branch,omitempty"`

	// AgentStatus / AgentSpawnedAt / AgentSessionID are used for pi
	// (the single supported agent) — despite the "Agent" prefix, they
	// are NOT multi-agent residue. They track:
	//   - AgentStatus: high-level state (idle/working/etc.) for UI badges
	//   - AgentSpawnedAt: when the agent started, for "X minutes ago" display
	//   - AgentSessionID: pi session UUID for resume UX
	//
	// The Pi* fields below are the canonical runtime state; AgentStatus
	// mirrors PiState in the single-agent world. AgentPort was removed
	// in M7 (zero readers; legacy multi-agent TCP port concept).
	AgentStatus    AgentStatus `json:"agent_status"`
	AgentSpawnedAt *time.Time  `json:"agent_spawned_at,omitempty"`
	AgentSessionID string      `json:"agent_session_id,omitempty"`

	// Pi session binding (Phase 1+).
	// These fields are the canonical state for awp v2. Agent* fields above
	// are legacy from a multi-agent design and will be deprecated
	// once the UI fully migrates to Pi* fields.
	PiSessionID   string    `json:"pi_session_id,omitempty"`     // pi UUID v7
	PiSessionPath string    `json:"pi_session_path,omitempty"`  // full .jsonl path
	PiSpawnedAt   time.Time `json:"pi_spawned_at,omitempty"`
	PiState       PiState   `json:"pi_state"`                    // default "none"
	PiActivity    string    `json:"pi_activity,omitempty"`       // "running bash", "reading file.go"
	PiModel       string    `json:"pi_model,omitempty"`          // "anthropic/claude-sonnet-4"
	PiThinking    string    `json:"pi_thinking,omitempty"`       // "off"|"minimal"|...|"xhigh"

	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`

	Labels   []string          `json:"labels,omitempty"`
	Priority int               `json:"priority,omitempty"`
	Meta     map[string]string `json:"meta,omitempty"`

	// Dependencies - tickets that block this one (informational only, no enforcement)
	BlockedBy []TicketID `json:"blocked_by,omitempty"`
}

func NewTicket(title, projectID string) *Ticket {
	now := time.Now()
	return &Ticket{
		ID:          NewTicketID(),
		ProjectID:   projectID,
		Title:       title,
		Status:      StatusBacklog,
		AgentStatus: AgentNone,
		PiState:     PiStateNone,
		UseWorktree: true,
		Priority:    3,
		CreatedAt:   now,
		UpdatedAt:   now,
		Labels:      []string{},
		Meta:        map[string]string{},
	}
}

func (t *Ticket) Touch() {
	t.UpdatedAt = time.Now()
}

func (t *Ticket) SetStatus(status TicketStatus) error {
	if err := t.CanTransitionTo(status); err != nil {
		return err
	}
	now := time.Now()
	t.Status = status
	t.UpdatedAt = now

	switch status {
	case StatusInProgress:
		t.StartedAt = &now
	case StatusDone:
		t.CompletedAt = &now
	}
	return nil
}

// CanTransitionTo enforces the PURE ticket state machine.
//
// This method is intentionally agent-agnostic: it knows only about
// TicketStatus transitions, not about AgentStatus or any runtime
// concerns. The board package shouldn't know about agent semantics.
//
// Rules:
//   - archived is terminal: no transition out (except archived → archived no-op)
//   - All other transitions are allowed (backlog ↔ in_progress ↔ done,
//     any → archived, done → backlog to reopen)
//
// Returns nil for allowed transitions, error otherwise.
//
// Caller responsibility: if a transition would orphan a running agent
// (e.g., in_progress → backlog with AgentStatus == AgentWorking), the
// caller must check that BEFORE invoking this method. The UI layer's
// dropTicket / quickMoveTicket handlers do this check.
func (t *Ticket) CanTransitionTo(target TicketStatus) error {
	// Same-status transition is a no-op; always allowed.
	if t.Status == target {
		return nil
	}

	// Archived is terminal — no transition out (except to itself, handled above).
	if t.Status == StatusArchived {
		return fmt.Errorf("cannot transition from %s to %s (archived is terminal)", t.Status, target)
	}

	// All other transitions are allowed.
	return nil
}

type Column struct {
	ID     string       `json:"id"`
	Name   string       `json:"name"`
	Status TicketStatus `json:"status"`
	Color  string       `json:"color"`
	Limit  int          `json:"limit"`
}

func DefaultColumns() []Column {
	return []Column{
		{ID: "backlog", Name: "Backlog", Status: StatusBacklog, Color: "#89b4fa", Limit: 0},
		{ID: "in-progress", Name: "In Progress", Status: StatusInProgress, Color: "#f9e2af", Limit: 3},
		{ID: "done", Name: "Done", Status: StatusDone, Color: "#a6e3a1", Limit: 0},
	}
}

// ErrTicketNotFound is returned when a ticket lookup fails.
// Use errors.Is(err, board.ErrTicketNotFound) to test for it.
var ErrTicketNotFound = errors.New("ticket not found")
