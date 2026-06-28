package project

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/pi/awp/internal/board"
	"github.com/pi/awp/internal/config"
)

// ticketsDir returns the directory for ticket storage.
// Falls back to current working directory on ConfigDir error.
func ticketsDir() string {
	dir, err := config.ConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "tickets")
}
const ticketsFile = "tickets.json"

type TicketStore struct {
	ProjectID string                           `json:"project_id"`
	Tickets   map[board.TicketID]*board.Ticket `json:"tickets"`
	UpdatedAt time.Time                        `json:"updated_at"`

	repoPath string
}

// NewTicketStore creates an in-memory ticket store for the given
// project. The store is not loaded from disk; use LoadTicketStore
// to load existing tickets.
func NewTicketStore(projectID, repoPath string) *TicketStore {
	return &TicketStore{
		ProjectID: projectID,
		Tickets:   make(map[board.TicketID]*board.Ticket),
		UpdatedAt: time.Now(),
		repoPath:  repoPath,
	}
}

// LoadTicketStore reads the ticket JSON file for the given project
// from disk. Returns an empty store if the file doesn't exist.
func LoadTicketStore(project *Project) (*TicketStore, error) {
	store := NewTicketStore(project.ID, project.RepoPath)

	// One-time migration: tickets written by the pre-rename upstream
	// (before this project was awp) used a `.openkanban` directory. Read
	// from that path if present, then write to the awp-native location.
	// After the first run, this migration is a no-op.
	oldPath := filepath.Join(project.RepoPath, ".openkanban", "tickets.json")
	newPath := store.filePath()

	// Ensure tickets directory exists
	if err := os.MkdirAll(ticketsDir(), 0755); err != nil {
		return nil, err
	}

	// Migration: if old exists and new doesn't, migrate
	if _, err := os.Stat(oldPath); err == nil {
		if _, err := os.Stat(newPath); os.IsNotExist(err) {
			// Old exists, new doesn't - migrate
			data, readErr := os.ReadFile(oldPath)
			if readErr == nil {
				if writeErr := os.WriteFile(newPath, data, 0644); writeErr == nil {
					log.Printf("Migrated tickets from %s to %s. You can safely delete the old .openkanban directory after the first run.", oldPath, newPath)
				}
			}
		}
	}

	// Load from new location
	data, err := os.ReadFile(newPath)
	if err != nil {
		if os.IsNotExist(err) {
			return store, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(data, store); err != nil {
		return nil, err
	}

	if store.Tickets == nil {
		store.Tickets = make(map[board.TicketID]*board.Ticket)
	}
	store.repoPath = project.RepoPath

	return store, nil
}

func (s *TicketStore) filePath() string {
	return filepath.Join(ticketsDir(), s.ProjectID+".json")
}

func (s *TicketStore) Save() error {
	dir := ticketsDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	s.UpdatedAt = time.Now()

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	path := s.filePath()
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func (s *TicketStore) Add(ticket *board.Ticket) {
	ticket.ProjectID = s.ProjectID
	s.Tickets[ticket.ID] = ticket
}

func (s *TicketStore) Get(id board.TicketID) (*board.Ticket, error) {
	t, ok := s.Tickets[id]
	if !ok {
		return nil, board.ErrTicketNotFound
	}
	return t, nil
}

func (s *TicketStore) Delete(id board.TicketID) error {
	if _, ok := s.Tickets[id]; !ok {
		return board.ErrTicketNotFound
	}
	delete(s.Tickets, id)
	return nil
}

func (s *TicketStore) Move(id board.TicketID, newStatus board.TicketStatus) error {
	t, ok := s.Tickets[id]
	if !ok {
		return board.ErrTicketNotFound
	}
	// SetStatus enforces the state machine (Cluster D.3); propagate its error.
	if err := t.SetStatus(newStatus); err != nil {
		return err
	}
	return nil
}

func (s *TicketStore) GetByStatus(status board.TicketStatus) []*board.Ticket {
	var result []*board.Ticket
	for _, t := range s.Tickets {
		if t.Status == status {
			result = append(result, t)
		}
	}
	return result
}

func (s *TicketStore) All() []*board.Ticket {
	result := make([]*board.Ticket, 0, len(s.Tickets))
	for _, t := range s.Tickets {
		result = append(result, t)
	}
	return result
}

func (s *TicketStore) Count() int {
	return len(s.Tickets)
}

func (s *TicketStore) CountByStatus(status board.TicketStatus) int {
	count := 0
	for _, t := range s.Tickets {
		if t.Status == status {
			count++
		}
	}
	return count
}

// GlobalTicketStore aggregates tickets from all projects
type GlobalTicketStore struct {
	registry     *ProjectRegistry
	projects     map[string]*Project
	ticketStores map[string]*TicketStore
	allTickets   map[board.TicketID]*board.Ticket
}

// NewGlobalTicketStore creates an empty cross-project ticket view
// backed by the given project registry.
func NewGlobalTicketStore(registry *ProjectRegistry) *GlobalTicketStore {
	return &GlobalTicketStore{
		registry:     registry,
		projects:     make(map[string]*Project),
		ticketStores: make(map[string]*TicketStore),
		allTickets:   make(map[board.TicketID]*board.Ticket),
	}
}

// LoadGlobalTicketStore loads ticket stores for every project in
// the registry. Projects with missing ticket files are loaded as
// empty stores.
//
// Post-2026-06-28 simplification: tickets with the removed status
// values "done" or "archived" (which may exist in pre-simplification
// JSONL files) are silently dropped from the loaded store. The user
// accepted this as a breaking change ("不兼容，历史数据移除"); no
// migration is performed. See internal/project/load_filter_test.go
// for the contract test.
func LoadGlobalTicketStore(registry *ProjectRegistry) (*GlobalTicketStore, error) {
	g := NewGlobalTicketStore(registry)

	for _, p := range registry.Projects {
		store, err := LoadTicketStore(p)
		if err != nil {
			continue
		}

		g.projects[p.ID] = p
		g.ticketStores[p.ID] = store

		for id, ticket := range store.Tickets {
			// Filter out tickets with removed status values. We compare
			// against raw strings ("done", "archived") rather than
			// board.StatusDone/StatusArchived because those constants
			// no longer exist after the state-machine simplification.
			// This is the breaking-change data-compat boundary.
			if s := string(ticket.Status); s == "done" || s == "archived" {
				continue
			}
			g.allTickets[id] = ticket
		}
	}

	return g, nil
}

func (g *GlobalTicketStore) GetProject(id string) *Project {
	return g.projects[id]
}

func (g *GlobalTicketStore) GetProjectForTicket(ticket *board.Ticket) *Project {
	return g.projects[ticket.ProjectID]
}

func (g *GlobalTicketStore) GetStoreForTicket(ticket *board.Ticket) *TicketStore {
	return g.ticketStores[ticket.ProjectID]
}

func (g *GlobalTicketStore) Get(id board.TicketID) (*board.Ticket, error) {
	t, ok := g.allTickets[id]
	if !ok {
		return nil, board.ErrTicketNotFound
	}
	return t, nil
}

func (g *GlobalTicketStore) Add(ticket *board.Ticket) error {
	store := g.ticketStores[ticket.ProjectID]
	if store == nil {
		return board.ErrTicketNotFound
	}
	store.Add(ticket)
	g.allTickets[ticket.ID] = ticket
	return nil
}

func (g *GlobalTicketStore) Delete(id board.TicketID) error {
	ticket, ok := g.allTickets[id]
	if !ok {
		return board.ErrTicketNotFound
	}

	store := g.ticketStores[ticket.ProjectID]
	if store != nil {
		store.Delete(id)
	}
	delete(g.allTickets, id)
	return nil
}

func (g *GlobalTicketStore) Move(id board.TicketID, newStatus board.TicketStatus) error {
	ticket, ok := g.allTickets[id]
	if !ok {
		return board.ErrTicketNotFound
	}

	store := g.ticketStores[ticket.ProjectID]
	if store != nil {
		store.Move(id, newStatus)
	}
	return nil
}

func (g *GlobalTicketStore) Save(ticket *board.Ticket) error {
	store := g.ticketStores[ticket.ProjectID]
	if store == nil {
		return board.ErrTicketNotFound
	}
	return store.Save()
}

func (g *GlobalTicketStore) SaveAll() error {
	for _, store := range g.ticketStores {
		if err := store.Save(); err != nil {
			return err
		}
	}
	return nil
}

func (g *GlobalTicketStore) GetByStatus(status board.TicketStatus) []*board.Ticket {
	var result []*board.Ticket
	for _, t := range g.allTickets {
		if t.Status == status {
			result = append(result, t)
		}
	}
	return result
}

func (g *GlobalTicketStore) All() []*board.Ticket {
	result := make([]*board.Ticket, 0, len(g.allTickets))
	for _, t := range g.allTickets {
		result = append(result, t)
	}
	return result
}

func (g *GlobalTicketStore) Count() int {
	return len(g.allTickets)
}

func (g *GlobalTicketStore) Projects() []*Project {
	result := make([]*Project, 0, len(g.projects))
	for _, p := range g.projects {
		result = append(result, p)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

func (g *GlobalTicketStore) HasProjects() bool {
	return len(g.projects) > 0
}

func (g *GlobalTicketStore) AddProject(p *Project) {
	g.projects[p.ID] = p
	g.ticketStores[p.ID] = NewTicketStore(p.ID, p.RepoPath)
}

func (g *GlobalTicketStore) RemoveProject(id string) error {
	if _, ok := g.projects[id]; !ok {
		return ErrProjectNotFound
	}

	// Post-2026-06-28: project deletion is final. The JSONL ticket
	// file is removed (not archived to tickets/archived/). The
	// previous archive-on-delete behavior was a relic of the 4-state
	// model; with the 2-state model and the 'delete = done' UX, the
	// user has already declared what they wanted to keep.
	srcPath := filepath.Join(ticketsDir(), id+".json")
	if err := os.Remove(srcPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove ticket file %s: %w", srcPath, err)
	}

	delete(g.projects, id)
	delete(g.ticketStores, id)

	return g.registry.Delete(id)
}

func (g *GlobalTicketStore) GetBlockedBy(ticketID board.TicketID) []*board.Ticket {
	ticket, ok := g.allTickets[ticketID]
	if !ok || len(ticket.BlockedBy) == 0 {
		return nil
	}

	var blockers []*board.Ticket
	for _, blockerID := range ticket.BlockedBy {
		if blocker, exists := g.allTickets[blockerID]; exists {
			blockers = append(blockers, blocker)
		}
	}
	return blockers
}

func (g *GlobalTicketStore) GetBlocks(ticketID board.TicketID) []*board.Ticket {
	var blocks []*board.Ticket
	for _, ticket := range g.allTickets {
		for _, blockerID := range ticket.BlockedBy {
			if blockerID == ticketID {
				blocks = append(blocks, ticket)
				break
			}
		}
	}
	return blocks
}

func (g *GlobalTicketStore) RemoveBlockerReferences(ticketID board.TicketID) {
	for _, ticket := range g.allTickets {
		if len(ticket.BlockedBy) == 0 {
			continue
		}
		var filtered []board.TicketID
		for _, blockerID := range ticket.BlockedBy {
			if blockerID != ticketID {
				filtered = append(filtered, blockerID)
			}
		}
		if len(filtered) != len(ticket.BlockedBy) {
			ticket.BlockedBy = filtered
		}
	}
}
