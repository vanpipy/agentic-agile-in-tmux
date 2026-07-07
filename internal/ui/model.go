package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"text/template"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/pi/awp/internal/board"
	"github.com/pi/awp/internal/config"
	"github.com/pi/awp/internal/git"
	"github.com/pi/awp/internal/observability"
	"github.com/pi/awp/internal/pi"
	"github.com/pi/awp/internal/project"
	"github.com/pi/awp/internal/terminal"
	"github.com/pi/awp/internal/update"

)

type Mode string

const (
	ModeNormal        Mode = "NORMAL"
	ModeInsert        Mode = "INSERT"
	ModeCommand       Mode = "COMMAND"
	ModeHelp          Mode = "HELP"
	ModeConfirm       Mode = "CONFIRM"
	ModeCreateTicket  Mode = "CREATE"
	ModeEditTicket    Mode = "EDIT"
	ModeAgentView     Mode = "AGENT"
	ModeSettings      Mode = "SETTINGS"
	ModeShuttingDown  Mode = "SHUTTING_DOWN"
	ModeSpawning      Mode = "SPAWNING"
	ModeFilter        Mode = "FILTER"
	ModeCreateProject Mode = "NEW_PROJECT"
	// Phase 3 + 4 (awp-specific)
	ModeEventView     Mode = "EVENTS"
	ModeSessionPicker Mode = "PICKER"
	ModeInterception  Mode = "INTERCEPT"
)

const (
	minColumnWidth = 20
	columnOverhead = 5

	ticketHeight       = 6
	columnHeaderHeight = 3

	formFieldTitle       = 0
	formFieldDescription = 1
	formFieldBranch      = 2
	formFieldLabels      = 3
	formFieldPriority    = 4
	formFieldWorktree    = 5
	formFieldBlockedBy   = 6
	formFieldProject     = 7
)

type Model struct {
	config *config.Config
	theme  config.Theme
	colors uiColors

	globalStore      *project.GlobalTicketStore
	projectRegistry  *project.ProjectRegistry
	columns          []board.Column
	filterProjectIDs map[string]bool

	worktreeMgrs   map[string]*git.WorktreeManager

	mode          Mode
	activeColumn  int
	activeTicket  int
	width         int
	height        int
	spinner       spinner.Model
	scrollOffset  int
	columnOffsets []int

	dragging         bool
	dragSourceColumn int
	dragSourceTicket int
	dragTargetColumn int

	hoverColumn int
	hoverTicket int

	lastClickTime   time.Time
	lastClickColumn int
	lastClickTicket int

	columnTickets [][]*board.Ticket

	showHelp    bool
	showConfirm bool
	confirmMsg  string
	confirmFn   func() tea.Cmd

	titleInput         textinput.Model
	descInput          textarea.Model
	branchInput        textinput.Model
	labelsInput        textinput.Model
	ticketPriority     int
	ticketUseWorktree  bool
	projectInput       textinput.Model
	ticketFormField    int
	editingTicketID    board.TicketID
	branchLocked       bool
	selectedProject    *project.Project
	projectListIndex   int
	addProjectPath     textinput.Model

	blockerCandidates  []*board.Ticket
	selectedBlockers   map[board.TicketID]bool
	blockerListIndex   int
	blockerFilterInput textinput.Model

	formScrollOffset int // user's scroll position in the ticket form (mutated by Update() handlers; read by View() but never mutated by View).
	formFieldLines   map[int]int // start line of each form field; populated by renderTicketForm; consumed by clampScrollOffset.

	notification string
	notifyTime   time.Time

	panes          map[board.TicketID]*terminal.Pane
	focusedPane    board.TicketID

	// turnDoneCaches tracks the per-pane JSONL-tail state for the
	// per-turn notification path (PR 2). Keyed by ticketID.
	// sync.Map because the poll goroutine reads and (rarely) writes
	// while the Update goroutine may also access on cleanup; we don't
	// want a separate mutex here. Cache itself has its own mutex
	// (TurnDoneCache.mu) so concurrent field reads are safe.
	turnDoneCaches sync.Map

	spawningTicketID board.TicketID

	settingsIndex   int
	settingsEditing bool
	settingsInput   textinput.Model
	themeListIndex  int

	filterInput textinput.Model
	filterQuery string

	sidebarVisible bool
	sidebarFocused bool
	sidebarIndex   int
	sidebarWidth   int

	updateChecker *update.Checker

	// Phase 3 (awp-specific): session picker state.
	pickerSessions []pi.SessionInfo
	pickerLoading  bool
	pickerErr      error
	pickerFilter   string
	pickerIndex    int
	pickerOffset   int
}

// NewModel constructs the kanban model. awp only supports pi,
// so we no longer accept agentMgr/opencodeServer parameters.
// The pi subprocess is started by prepareSpawn (spawn flow)
// when the user presses 's' on a ticket.
func NewModel(cfg *config.Config, globalStore *project.GlobalTicketStore, projectRegistry *project.ProjectRegistry, filterProjectID string, updateChecker *update.Checker) *Model {
	ti := textinput.New()
	ti.Placeholder = "Enter ticket title..."
	ti.CharLimit = 100
	ti.Width = 40

	di := textarea.New()
	di.Placeholder = "Optional description..."
	di.CharLimit = 0
	di.SetWidth(40)
	di.SetHeight(4)
	di.ShowLineNumbers = false

	bi := textinput.New()
	bi.Placeholder = "Auto-generated from title..."
	bi.CharLimit = 100
	bi.Width = 40

	li := textinput.New()
	li.Placeholder = "bug, urgent, frontend (comma-separated)"
	li.CharLimit = 200
	li.Width = 40

	pi := textinput.New()
	pi.Placeholder = "Select project..."
	pi.CharLimit = 100
	pi.Width = 40

	si := textinput.New()
	si.CharLimit = 200
	si.Width = 40

	fi := textinput.New()
	fi.Placeholder = "Search tickets..."
	fi.CharLimit = 100
	fi.Width = 30

	ap := textinput.New()
	ap.Placeholder = "/path/to/repository"
	ap.CharLimit = 256
	ap.Width = 40
	ap.ShowSuggestions = true       // enable TAB completion in Add Project form
	ap.SetSuggestions(nil)            // initial: no suggestions until user types

	bf := textinput.New()
	bf.Placeholder = "Filter tickets..."
	bf.CharLimit = 100
	bf.Width = 30

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	worktreeMgrs := make(map[string]*git.WorktreeManager)
	for _, p := range globalStore.Projects() {
		worktreeMgrs[p.ID] = git.NewWorktreeManager(p)
	}

	var selectedProject *project.Project
	projects := globalStore.Projects()
	if len(projects) > 0 {
		if filterProjectID != "" {
			selectedProject = globalStore.GetProject(filterProjectID)
		}
		if selectedProject == nil {
			selectedProject = projects[0]
		}
	}

	theme := cfg.GetTheme()
	m := &Model{
		config:             cfg,
		theme:              theme,
		colors:             newUIColors(theme),
		globalStore:        globalStore,
		projectRegistry:    projectRegistry,
		columns:            board.DefaultColumns(),
		filterProjectIDs:   make(map[string]bool),
		worktreeMgrs:       worktreeMgrs,
		mode:               ModeNormal,
		titleInput:         ti,
		descInput:          di,
		branchInput:        bi,
		labelsInput:        li,
		ticketPriority:     3,
		projectInput:       pi,
		settingsInput:      si,
		filterInput:        fi,
		addProjectPath:     ap,
		blockerFilterInput: bf,
		selectedBlockers:   make(map[board.TicketID]bool),
		formFieldLines:     make(map[int]int),
		spinner:            sp,
		panes: make(map[board.TicketID]*terminal.Pane),
		selectedProject:    selectedProject,
		sidebarVisible:     cfg.UI.SidebarVisible,
		sidebarWidth:       24,
		hoverColumn:        -1,
		hoverTicket:        -1,
		updateChecker:      updateChecker,
	}
	if filterProjectID != "" {
		m.filterProjectIDs[filterProjectID] = true
	}

	// Reset all agent statuses on startup so the UI doesn't show stale
	// "working" badges after app restart. In-memory only — we deliberately
	// do NOT Save() to disk because:
	//
	//   (a) If pi was actually still running (orphan-PTY scenario from a
	//       crashed awp), downgrading the on-disk state would lose that
	//       signal. A future orphan-detection pass (D.1 Phase 2) needs the
	//       disk state to identify orphans via PID lookup.
	//
	//   (b) The disk file is unchanged between restarts unless a real event
	//       happens (spawn, stop, status change). Writing on every launch
	//       was unnecessary churn.
	//
	// See Cluster D.1 of the 2026-06-27 audit for context.
	for _, ticket := range globalStore.All() {
		ticket.AgentStatus = board.AgentNone
	}

	m.refreshColumnTickets()
	return m
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		tickAgentStatus(5 * time.Second),
		tickNotification(notificationTickInterval),
		m.spinner.Tick,
		m.checkForUpdates(),
	)
}

func (m *Model) checkForUpdates() tea.Cmd {
	if m.updateChecker == nil {
		return nil
	}
	return func() tea.Msg {
		defer observability.RecoverPanic("checkForUpdates")
		return updateCheckMsg(m.updateChecker.Check())
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.mode == ModeShuttingDown {
		switch msg := msg.(type) {
		case shutdownCompleteMsg:
			return m, tea.Quit
		case spinner.TickMsg:
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	if m.mode == ModeSpawning {
		switch msg := msg.(type) {
		case agentStatusMsg:
			return m, tea.Batch(
				m.pollAgentStatusesAsync(),
				m.pollTurnDonesAsync(),
				tickAgentStatus(5 * time.Second),
			)
		case spawnReadyMsg:
			if msg.ticketID != m.spawningTicketID {
				return m, nil
			}

			ticket, _ := m.globalStore.Get(msg.ticketID)
			if ticket != nil {
				ticket.AgentStatus = board.AgentNone
				if ticket.AgentSpawnedAt == nil {
					now := time.Now()
					ticket.AgentSpawnedAt = &now
				}
				if msg.worktreePath != "" && ticket.WorktreePath == "" {
					ticket.WorktreePath = msg.worktreePath
					ticket.BranchName = msg.branchName
					ticket.BaseBranch = msg.baseBranch
				}
				m.saveTicket(ticket)
			}

			m.panes[msg.ticketID] = msg.pane
			m.focusedPane = msg.ticketID
			m.mode = ModeAgentView
			m.spawningTicketID = ""
			return m, msg.pane.StartCmd(msg.command, msg.args...)

		case spawnErrorMsg:
			if msg.ticketID == m.spawningTicketID {
				m.mode = ModeNormal
				m.spawningTicketID = ""
				
				m.notify(msg.err)
			}
			return m, nil

		case terminal.OutputMsg:
			if board.TicketID(msg.PaneID) == m.spawningTicketID {
				m.mode = ModeAgentView
				m.spawningTicketID = ""
				
			}
			return m.handleTerminalMsg(msg)

		case terminal.ExitMsg:
			if board.TicketID(msg.PaneID) == m.spawningTicketID {
				m.resetSpawnState(board.TicketID(msg.PaneID))
				if msg.Err != nil {
					m.notify("Agent failed: " + msg.Err.Error())
				} else {
					m.notify("Agent exited unexpectedly")
				}
			}
			return m, nil

		case spinner.TickMsg:
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd

		case tea.KeyMsg:
			if msg.String() == "esc" {
				if pane, ok := m.panes[m.spawningTicketID]; ok {
					pane.Stop()
					delete(m.panes, m.spawningTicketID)
					m.turnDoneCaches.Delete(m.spawningTicketID)
				}
				m.mode = ModeNormal
				m.spawningTicketID = ""
				
				m.notify("Spawn cancelled")
				return m, nil
			}
		}
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.focusedPane != "" {
			if pane, ok := m.panes[m.focusedPane]; ok {
				pane.SetSize(m.width, m.height-2)
			}
		}
		return m, nil

	case tea.MouseMsg:
		if m.mode == ModeNormal {
			return m.handleMouse(msg)
		}
		if m.mode == ModeAgentView {
			return m.handleAgentViewMouse(msg)
		}
		if m.mode == ModeCreateTicket || m.mode == ModeEditTicket {
			return m.handleTicketFormMouse(msg)
		}
		if m.mode == ModeFilter {
			return m.handleFilterMouse(msg)
		}
		if m.mode == ModeSettings {
			return m.handleSettingsMouse(msg)
		}
		if m.showHelp {
			if msg.Action == tea.MouseActionPress {
				m.showHelp = false
			}
			return m, nil
		}
		if m.showConfirm {
			return m.handleConfirmMouse(msg)
		}
		return m, nil

	case terminal.OutputMsg, terminal.RenderTickMsg:
		return m.handleTerminalMsg(msg)

	case terminal.ExitMsg:
		ticketID := board.TicketID(msg.PaneID)
		wasFocused := m.focusedPane == ticketID
		delete(m.panes, ticketID)
		m.turnDoneCaches.Delete(ticketID)
		if ticket, _ := m.globalStore.Get(ticketID); ticket != nil {
			ticket.AgentStatus = board.AgentNone
			m.saveTicket(ticket)
		}
		if wasFocused {
			m.mode = ModeNormal
			m.focusedPane = ""
		}
		// PR1 (task/awp): per-task stop notification. Always emit a
		// toast on exit, even for non-focused panes — that's the whole
		// point. Wording differs by focus state and crash vs clean
		// exit (see notifyExit for the matrix).
		m.notifyExit(ticketID, msg.Err, wasFocused)
		return m, nil

	case terminal.ExitFocusMsg:
		m.mode = ModeNormal
		m.focusedPane = ""
		return m, nil

	case agentStatusMsg:
		return m, tea.Batch(
			m.pollAgentStatusesAsync(),
			m.pollTurnDonesAsync(),
			tickAgentStatus(5 * time.Second),
		)

	case agentStatusResultMsg:
		for ticketID, status := range msg {
			if ticket, _ := m.globalStore.Get(ticketID); ticket != nil {
				ticket.AgentStatus = status
			}
		}

	case pollTurnDonesMsg:
		for _, fire := range msg.fires {
			m.handlePaneTurnDone(fire)
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	// notificationMsg fires from tickNotification (every notificationTickInterval).
	// Self-sustaining: clear the toast if it's been on screen longer than
	// notificationDuration; always re-arm the tick on return. Conditional
	// re-arm ("only tick when a toast is visible") would let the tick die
	// during the first 500ms after Init() if no m.notify() ran, recreating
	// the "feature does nothing" regression. See NOTIFY_DIAGNOSIS.md §6.
	case notificationMsg:
		if m.notification != "" && time.Since(m.notifyTime) > notificationDuration {
			m.notification = ""
		}
		return m, tickNotification(notificationTickInterval)

	case updateCheckMsg:
		if msg.UpdateAvailable {
			result := update.CheckResult(msg)
			m.notify(fmt.Sprintf("Update %s available: %s", msg.LatestVersion, result.UpdateHint()))
		}
		return m, nil
	}

	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		if m.mode == ModeNormal {
			return m.handleQuit()
		}
	case "esc":
		if m.mode == ModeAgentView {
			break
		}
		if m.mode == ModeNormal && (m.filterQuery != "" || len(m.filterProjectIDs) > 0) {
			m.clearFilter()
			m.notify("Filter cleared")
			return m, nil
		}
		m.mode = ModeNormal
		m.showHelp = false
		m.showConfirm = false
		m.titleInput.Blur()
		return m, nil
	case "?":
		if m.mode == ModeNormal || m.mode == ModeHelp {
			m.showHelp = !m.showHelp
			return m, nil
		}
	}

	if m.showHelp {
		m.showHelp = false
		return m, nil
	}

	if m.showConfirm {
		return m.handleConfirm(msg)
	}

	switch m.mode {
	case ModeNormal:
		return m.handleNormalMode(msg)
	case ModeCommand:
		return m.handleCommandMode(msg)
	case ModeCreateTicket:
		return m.handleCreateTicketMode(msg)
	case ModeEditTicket:
		return m.handleEditTicketMode(msg)
	case ModeAgentView:
		return m.handleAgentViewMode(msg)
	case ModeSettings:
		return m.handleSettingsMode(msg)
	case ModeFilter:
		return m.handleFilterMode(msg)
	case ModeCreateProject:
		return m.handleCreateProjectMode(msg)
	}

	return m, nil
}

func (m *Model) handleNormalMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "tab":
		if m.sidebarVisible {
			m.sidebarFocused = !m.sidebarFocused
			return m, nil
		}
	case "[":
		m.sidebarVisible = !m.sidebarVisible
		if !m.sidebarVisible {
			m.sidebarFocused = false
		}
		return m, nil
	}

	if m.sidebarFocused {
		return m.handleSidebarNav(msg)
	}

	switch msg.String() {
	case "h", "left":
		if m.activeColumn == 0 && m.sidebarVisible {
			m.sidebarFocused = true
			return m, nil
		}
		m.moveColumn(-1)
	case "l", "right":
		m.moveColumn(1)
	case "j", "down":
		m.moveTicket(1)
	case "k", "up":
		m.moveTicket(-1)
	case "g":
		m.activeTicket = 0
		m.ensureTicketVisible()
	case "G":
		if len(m.columnTickets) > m.activeColumn {
			m.activeTicket = max(len(m.columnTickets[m.activeColumn])-1, 0)
		}
		m.ensureTicketVisible()

	case "n":
		return m.createNewTicket()
	case "e":
		return m.editTicket()
	case "enter":
		return m.attachToAgent()
	case "d":
		return m.confirmDeleteTicket()
	case " ":
		return m.toggleSelectedTicket()
	case "s":
		return m.spawnAgent()
	case "S":
		return m.stopAgent()

	case ":":
		m.mode = ModeCommand

	case "/":
		m.filterInput.SetValue(m.filterQuery)
		m.filterInput.Focus()
		m.mode = ModeFilter

	case "O":
		m.mode = ModeSettings
		m.settingsIndex = 0
		m.settingsEditing = false
	}

	return m, nil
}

func (m *Model) openAddProjectForm() (tea.Model, tea.Cmd) {
	m.addProjectPath.SetValue("")
	m.addProjectPath.SetSuggestions(nil) // start empty; refresh as user types
	m.addProjectPath.ShowSuggestions = true
	m.addProjectPath.Focus()
	m.mode = ModeCreateProject
	m.notification = ""
	return m, textinput.Blink
}

func (m *Model) sidebarAllY() int          { return 2 }
func (m *Model) sidebarProjectStartY() int { return 4 }
func (m *Model) sidebarAddProjectY(projectCount int) int {
	return m.sidebarProjectStartY() + projectCount + 1
}

func (m *Model) handleSidebarMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	y := msg.Y - m.headerHeight()

	if y < 0 {
		return m, nil
	}

	projects := m.globalStore.Projects()

	if y == m.sidebarAllY() {
		m.sidebarIndex = 0
		m.toggleAllProjects()
		return m, nil
	}

	for i := range projects {
		if y == m.sidebarProjectStartY()+i {
			m.sidebarIndex = i + 1
			m.toggleProjectFilter(projects[i].ID)
			return m, nil
		}
	}

	if y == m.sidebarAddProjectY(len(projects)) {
		return m.openAddProjectForm()
	}

	m.sidebarFocused = true
	return m, nil
}

func (m *Model) handleSidebarNav(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	projects := m.globalStore.Projects()
	addIndex := len(projects) + 1

	switch msg.String() {
	case "j", "down":
		if m.sidebarIndex < addIndex {
			m.sidebarIndex++
		}
	case "k", "up":
		if m.sidebarIndex > 0 {
			m.sidebarIndex--
		}
	case "enter", " ":
		if m.sidebarIndex == 0 {
			m.toggleAllProjects()
		} else if m.sidebarIndex == addIndex {
			return m.openAddProjectForm()
		} else {
			idx := m.sidebarIndex - 1
			if idx < len(projects) {
				m.toggleProjectFilter(projects[idx].ID)
			}
		}
	case "l", "right":
		m.sidebarFocused = false
		return m, nil
	case "a":
		return m.openAddProjectForm()
	case "d":
		if m.sidebarIndex > 0 && m.sidebarIndex <= len(projects) {
			m.confirmDeleteProject(projects[m.sidebarIndex-1])
		}
		return m, nil
	case "esc":
		m.sidebarFocused = false
	}

	return m, nil
}

func (m *Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Action {
	case tea.MouseActionPress:
		if msg.Button == tea.MouseButtonLeft {
			if m.hitTestHeader(msg.X, msg.Y) {
				return m, nil
			}
			if m.sidebarVisible && msg.X < m.sidebarWidth {
				return m.handleSidebarMouse(msg)
			}
			col, ticket := m.hitTest(msg.X, msg.Y)
			if col >= 0 {
				m.sidebarFocused = false
				m.activeColumn = col
				if ticket >= 0 {
					now := time.Now()
					isDoubleClick := ticket == m.lastClickTicket &&
						col == m.lastClickColumn &&
						now.Sub(m.lastClickTime) < 400*time.Millisecond

					if isDoubleClick {
						m.lastClickTime = time.Time{}
						m.lastClickColumn = -1
						m.lastClickTicket = -1
						return m.handleDoubleClick()
					}

					m.lastClickTime = now
					m.lastClickColumn = col
					m.lastClickTicket = ticket

					m.activeTicket = ticket
					m.dragging = true
					m.dragSourceColumn = col
					m.dragSourceTicket = ticket
					m.dragTargetColumn = col
				}
				m.ensureColumnVisible()
			}
		}

	case tea.MouseActionMotion:
		if m.dragging && msg.Button == tea.MouseButtonLeft {
			col, _ := m.hitTest(msg.X, msg.Y)
			if col >= 0 {
				m.dragTargetColumn = col
			}
		} else {
			if m.sidebarVisible && msg.X < m.sidebarWidth {
				m.hoverColumn = -1
				m.hoverTicket = -1
			} else {
				col, ticket := m.hitTest(msg.X, msg.Y)
				m.hoverColumn = col
				m.hoverTicket = ticket
			}
		}

	case tea.MouseActionRelease:
		if m.dragging {
			if m.dragTargetColumn != m.dragSourceColumn && m.dragTargetColumn >= 0 {
				return m.dropTicket()
			}
			m.dragging = false
			m.dragTargetColumn = 0
		}
		col, ticket := m.hitTest(msg.X, msg.Y)
		m.hoverColumn = col
		m.hoverTicket = ticket

	default:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.moveTicket(-1)
		case tea.MouseButtonWheelDown:
			m.moveTicket(1)
		}
	}

	return m, nil
}

func (m *Model) hitTestHeader(x, y int) bool {
	if y > 2 {
		return false
	}

	if m.filterQuery != "" || len(m.filterProjectIDs) > 0 {
		clearStart := 20 + len(m.filterQuery) + 15
		if x >= clearStart && x <= clearStart+10 {
			m.clearFilter()
			return true
		}
	}

	if x >= 15 && x <= 30 {
		m.filterInput.SetValue(m.filterQuery)
		m.filterInput.Focus()
		m.mode = ModeFilter
		return true
	}

	return false
}

func (m *Model) hitTest(x, y int) (column, ticket int) {
	if m.width == 0 || len(m.columns) == 0 {
		return -1, -1
	}

	if m.sidebarVisible {
		x = x - m.sidebarWidth - 1
	}

	headerHeight := 2
	if y < headerHeight {
		return -1, -1
	}

	columnWidth := m.calcColumnWidth()
	visibleCols := m.visibleColumnCount(columnWidth)
	numVisible := visibleCols
	if m.scrollOffset+visibleCols > len(m.columns) {
		numVisible = len(m.columns) - m.scrollOffset
	}

	baseWidth, remainder := m.distributeWidth(numVisible)

	hasLeftIndicator := m.scrollOffset > 0
	startX := 0
	if hasLeftIndicator {
		startX = 2
	}

	for i := 0; i < numVisible; i++ {
		colWidth := baseWidth + 3
		if i < remainder {
			colWidth++
		}

		if x >= startX && x < startX+colWidth {
			actualCol := m.scrollOffset + i
			ticketIdx := m.hitTestTicket(y-headerHeight, actualCol)
			return actualCol, ticketIdx
		}
		startX += colWidth
	}

	return -1, -1
}

func (m *Model) hitTestTicket(relativeY, column int) int {
	if column < 0 || column >= len(m.columnTickets) {
		return -1
	}

	tickets := m.columnTickets[column]
	if len(tickets) == 0 {
		return -1
	}

	ticketY := relativeY - columnHeaderHeight
	if ticketY < 0 {
		return -1
	}

	offset := 0
	if column < len(m.columnOffsets) {
		offset = m.columnOffsets[column]
	}

	ticketIdx := offset + (ticketY / ticketHeight)
	if ticketIdx >= len(tickets) {
		return -1
	}

	return ticketIdx
}

func (m *Model) dropTicket() (tea.Model, tea.Cmd) {
	if len(m.columnTickets) <= m.dragSourceColumn {
		m.dragging = false
		return m, nil
	}

	tickets := m.columnTickets[m.dragSourceColumn]
	if len(tickets) <= m.dragSourceTicket {
		m.dragging = false
		return m, nil
	}

	ticket := tickets[m.dragSourceTicket]
	targetStatus := m.columns[m.dragTargetColumn].Status

	// Caller-side orphan-agent guard (FOOT-3): CanTransitionTo is pure
	// (no agent semantics). The UI must check AgentStatus BEFORE
	// invoking Move() to prevent orphaning a running pi subprocess.
	if err := m.checkOrphanAgentBeforeMove(ticket, targetStatus); err != nil {
		m.notify("Move rejected: " + err.Error())
		m.dragging = false
		m.dragTargetColumn = 0
		return m, nil
	}

	if targetStatus == board.StatusInProgress && ticket.WorktreePath == "" {
		if ticket.UseWorktree {
			if err := m.setupWorktree(ticket); err != nil {
				m.notify("Worktree failed: " + err.Error())
				m.dragging = false
				return m, nil
			}
		} else {
			if err := m.setupMainRepoBranch(ticket); err != nil {
				m.notify("Branch setup failed: " + err.Error())
				m.dragging = false
				return m, nil
			}
		}
	}

	if err := m.globalStore.Move(ticket.ID, targetStatus); err != nil {
		m.notify("Move rejected: " + err.Error())
		m.dragging = false
		m.dragTargetColumn = 0
		return m, nil
	}
	m.refreshColumnTickets()
	m.saveTicket(ticket)

	m.activeColumn = m.dragTargetColumn
	m.activeTicket = 0
	m.ensureColumnVisible()
	m.ensureTicketVisible()

	m.notify("Moved to " + string(targetStatus))
	m.dragging = false
	m.dragTargetColumn = 0

	return m, nil
}

func (m *Model) handleCommandMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.mode = ModeNormal
	case "esc":
		m.mode = ModeNormal
	}
	return m, nil
}

func (m *Model) handleConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		m.showConfirm = false
		if m.confirmFn != nil {
			return m, m.confirmFn()
		}
	case "n", "N", "esc":
		m.showConfirm = false
	}
	return m, nil
}

func (m *Model) handleQuit() (tea.Model, tea.Cmd) {
	runningCount := m.RunningAgentCount()
	if runningCount == 0 {
		return m, tea.Quit
	}

	if !m.config.Behavior.ConfirmQuitWithAgents {
		m.mode = ModeShuttingDown
		return m, tea.Batch(m.spinner.Tick, m.cleanupAsync())
	}

	m.showConfirm = true
	m.confirmMsg = fmt.Sprintf("%d agent(s) running. Quit anyway? [y/N]", runningCount)
	m.confirmFn = func() tea.Cmd {
		m.mode = ModeShuttingDown
		m.showConfirm = false
		return tea.Batch(m.spinner.Tick, m.cleanupAsync())
	}
	return m, nil
}

func (m *Model) cleanupAsync() tea.Cmd {
	return func() tea.Msg {
		defer observability.RecoverPanic("cleanupAsync")
		m.Cleanup()
		return shutdownCompleteMsg{}
	}
}

func (m *Model) handleAgentViewMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	pane, ok := m.panes[m.focusedPane]
	if !ok {
		m.mode = ModeNormal
		m.focusedPane = ""
		return m, nil
	}

	if result := pane.HandleKey(msg); result != nil {
		if _, isExit := result.(terminal.ExitFocusMsg); isExit {
			m.mode = ModeNormal
			m.focusedPane = ""
		}
	}

	return m, nil
}

func (m *Model) handleAgentViewMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	pane, ok := m.panes[m.focusedPane]
	if !ok {
		return m, nil
	}

	if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
		if msg.Y == 0 && msg.X >= m.width-25 {
			m.mode = ModeNormal
			return m, nil
		}
	}

	pane.HandleMouse(msg)
	return m, nil
}

func (m *Model) handleTicketFormMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.formScrollOffset -= 3
		if m.formScrollOffset < 0 {
			m.formScrollOffset = 0
		}
		return m, nil
	case tea.MouseButtonWheelDown:
		m.formScrollOffset += 3
		return m, nil
	}

	formWidth := 50
	formLeft := (m.width - formWidth) / 2
	formRight := formLeft + formWidth

	if msg.X < formLeft || msg.X > formRight {
		return m, nil
	}

	formTop := (m.height - 28) / 2
	relY := msg.Y - formTop

	var clickedField int = -1
	switch {
	case relY >= 3 && relY <= 4:
		clickedField = formFieldTitle
	case relY >= 6 && relY <= 9:
		clickedField = formFieldDescription
	case relY >= 11 && relY <= 13:
		clickedField = formFieldBranch
	case relY >= 15 && relY <= 17:
		clickedField = formFieldLabels
	case relY >= 19 && relY <= 21:
		clickedField = formFieldPriority
	case relY >= 23:
		clickedField = formFieldProject
	}

	if clickedField >= 0 && msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
		m.blurAllFormFields()
		m.ticketFormField = clickedField
		m.focusCurrentField()

		if clickedField == formFieldProject {
			projects := m.globalStore.Projects()
			projectRelY := relY - 24
			if projectRelY >= 0 && projectRelY < len(projects) {
				m.projectListIndex = projectRelY
				m.selectedProject = projects[projectRelY]
			}
			// Note: there is intentionally no "+ Add project" row to
			// click on inside the ticket form — single entry point for
			// adding projects is the sidebar (openAddProjectForm).
		}
	}

	var cmd tea.Cmd
	switch m.ticketFormField {
	case formFieldTitle:
		m.titleInput, cmd = m.titleInput.Update(msg)
	case formFieldDescription:
		m.descInput, cmd = m.descInput.Update(msg)
	case formFieldBranch:
		if !m.branchLocked {
			m.branchInput, cmd = m.branchInput.Update(msg)
		}
	case formFieldLabels:
		m.labelsInput, cmd = m.labelsInput.Update(msg)
	}

	return m, cmd
}

func (m *Model) handleCreateTicketMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	return m.handleTicketForm(msg, false)
}

func (m *Model) handleEditTicketMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	return m.handleTicketForm(msg, true)
}

func (m *Model) handleTicketForm(msg tea.KeyMsg, isEdit bool) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.mode = ModeNormal
		m.blurAllFormFields()
		m.editingTicketID = ""
		m.branchLocked = false
		return m, nil

	case "tab":
		return m.nextFormField(isEdit), nil
	case "shift+tab":
		return m.prevFormField(isEdit), nil

	case "ctrl+s":
		return m.saveTicketForm(isEdit)

	case "enter":
		if m.ticketFormField == formFieldTitle {
			return m.saveTicketForm(isEdit)
		}
		if m.ticketFormField == formFieldProject && !isEdit {
			return m.handleProjectSelection()
		}

	case "esc":
		m.mode = ModeNormal
		m.blurAllFormFields()
		m.editingTicketID = ""
		m.branchLocked = false
		return m, nil
	}

	var cmd tea.Cmd
	switch m.ticketFormField {
	case formFieldTitle:
		m.titleInput, cmd = m.titleInput.Update(msg)
	case formFieldDescription:
		m.descInput, cmd = m.descInput.Update(msg)
	case formFieldBranch:
		if !m.branchLocked {
			m.branchInput, cmd = m.branchInput.Update(msg)
		}
	case formFieldLabels:
		m.labelsInput, cmd = m.labelsInput.Update(msg)
	case formFieldPriority:
		cmd = m.handlePriorityNav(msg)
	case formFieldWorktree:
		cmd = m.handleWorktreeToggle(msg)
	case formFieldBlockedBy:
		cmd = m.handleBlockerNav(msg)
	case formFieldProject:
		cmd = m.handleProjectListNav(msg)
	}
	return m, cmd
}

func (m *Model) handlePriorityNav(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "j", "down", "l", "right":
		m.ticketPriority++
		if m.ticketPriority > 5 {
			m.ticketPriority = 1
		}
	case "k", "up", "h", "left":
		m.ticketPriority--
		if m.ticketPriority < 1 {
			m.ticketPriority = 5
		}
	case "1", "2", "3", "4", "5":
		m.ticketPriority = int(msg.String()[0] - '0')
	}
	return nil
}

func (m *Model) handleWorktreeToggle(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case " ", "enter", "h", "l", "left", "right":
		m.ticketUseWorktree = !m.ticketUseWorktree
	case "y", "Y":
		m.ticketUseWorktree = true
	case "n", "N":
		m.ticketUseWorktree = false
	}
	return nil
}


func (m *Model) handleProjectListNav(msg tea.KeyMsg) tea.Cmd {
	projects := m.globalStore.Projects()
	maxIndex := len(projects) - 1
	// No projects at all: nothing to navigate. The user must press
	// Esc and add one from the sidebar (single entry point).
	if maxIndex < 0 {
		return nil
	}

	switch msg.String() {
	case "j", "down":
		m.projectListIndex++
		if m.projectListIndex > maxIndex {
			m.projectListIndex = 0
		}
	case "k", "up":
		m.projectListIndex--
		if m.projectListIndex < 0 {
			m.projectListIndex = maxIndex
		}
	case "d":
		if m.projectListIndex < len(projects) {
			m.confirmDeleteProject(projects[m.projectListIndex])
		}
	}

	// Auto-select the highlighted project.
	if m.projectListIndex < len(projects) {
		m.selectedProject = projects[m.projectListIndex]
	}

	return nil
}

func (m *Model) handleBlockerNav(msg tea.KeyMsg) tea.Cmd {
	visibleCandidates := m.getFilteredBlockerCandidates()

	switch msg.Type {
	case tea.KeyDown, tea.KeyCtrlN:
		if len(visibleCandidates) > 0 {
			m.blockerListIndex++
			if m.blockerListIndex >= len(visibleCandidates) {
				m.blockerListIndex = 0
			}
		}
		return nil
	case tea.KeyUp, tea.KeyCtrlP:
		if len(visibleCandidates) > 0 {
			m.blockerListIndex--
			if m.blockerListIndex < 0 {
				m.blockerListIndex = len(visibleCandidates) - 1
			}
		}
		return nil
	case tea.KeySpace, tea.KeyEnter:
		if m.blockerListIndex < len(visibleCandidates) {
			ticket := visibleCandidates[m.blockerListIndex]
			if m.selectedBlockers[ticket.ID] {
				delete(m.selectedBlockers, ticket.ID)
			} else {
				m.selectedBlockers[ticket.ID] = true
			}
		}
		return nil
	}

	var cmd tea.Cmd
	m.blockerFilterInput, cmd = m.blockerFilterInput.Update(msg)

	newVisible := m.getFilteredBlockerCandidates()
	if m.blockerListIndex >= len(newVisible) && len(newVisible) > 0 {
		m.blockerListIndex = len(newVisible) - 1
	} else if len(newVisible) == 0 {
		m.blockerListIndex = 0
	}

	return cmd
}

func (m *Model) getFilteredBlockerCandidates() []*board.Ticket {
	filterVal := m.blockerFilterInput.Value()
	if filterVal == "" {
		return m.blockerCandidates
	}

	var visible []*board.Ticket
	for _, t := range m.blockerCandidates {
		if strings.Contains(strings.ToLower(t.Title), strings.ToLower(filterVal)) {
			visible = append(visible, t)
		}
	}
	return visible
}

func (m *Model) initBlockerCandidates(excludeTicketID board.TicketID) {
	m.blockerCandidates = nil
	for _, ticket := range m.globalStore.All() {
		if ticket.ID == excludeTicketID {
			continue
		}
		m.blockerCandidates = append(m.blockerCandidates, ticket)
	}
	sort.Slice(m.blockerCandidates, func(i, j int) bool {
		return m.blockerCandidates[i].Title < m.blockerCandidates[j].Title
	})
}

func (m *Model) collectSelectedBlockers() []board.TicketID {
	var blockers []board.TicketID
	for id := range m.selectedBlockers {
		blockers = append(blockers, id)
	}
	sort.Slice(blockers, func(i, j int) bool {
		return string(blockers[i]) < string(blockers[j])
	})
	return blockers
}

func (m *Model) confirmDeleteProject(p *project.Project) {
	ticketCount := 0
	for _, t := range m.globalStore.All() {
		if t.ProjectID == p.ID {
			ticketCount++
		}
	}

	if ticketCount > 0 {
		m.confirmMsg = fmt.Sprintf("Delete '%s' and its %d ticket(s)?", p.Name, ticketCount)
	} else {
		m.confirmMsg = fmt.Sprintf("Delete project '%s'?", p.Name)
	}

	m.showConfirm = true
	m.confirmFn = func() tea.Cmd {
		if err := m.projectRegistry.Delete(p.ID); err != nil {
			m.notify("Failed to delete: " + err.Error())
			return nil
		}

		m.globalStore.RemoveProject(p.ID)
		delete(m.worktreeMgrs, p.ID)

		projects := m.globalStore.Projects()
		if len(projects) > 0 {
			if m.projectListIndex >= len(projects) {
				m.projectListIndex = len(projects) - 1
			}
			m.selectedProject = projects[m.projectListIndex]
		} else {
			m.selectedProject = nil
		}

		delete(m.filterProjectIDs, p.ID)

		m.notify("Deleted: " + p.Name)
		return nil
	}
}

func (m *Model) handleProjectSelection() (tea.Model, tea.Cmd) {
	projects := m.globalStore.Projects()

	if m.projectListIndex >= 0 && m.projectListIndex < len(projects) {
		m.selectedProject = projects[m.projectListIndex]
	}
	// Out-of-bounds index (no projects or stale state): no-op.
	// The ticket form has no inner add-project entry; users add
	// projects from the sidebar only (single entry point).
	return m, nil
}

func (m *Model) createProjectFromPath() (tea.Model, tea.Cmd) {
	path := strings.TrimSpace(m.addProjectPath.Value())
	if path == "" {
		m.notify("Path cannot be empty")
		return m, nil
	}

	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			path = filepath.Join(home, path[2:])
		}
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		m.notify("Invalid path: " + err.Error())
		return m, nil
	}

	// Cluster E.1 (2026-06-27 audit): resolve symlinks BEFORE the .git check.
	// os.Stat follows symlinks, which means a symlink to /etc/passwd/.git
	// (or any user-readable path) could trick the validation. EvalSymlinks
	// returns the canonical target; if the symlink is broken, it returns an
	// error which we surface clearly to the user.
	resolvedPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		m.notify("Path cannot be resolved (broken symlink?): " + err.Error())
		return m, nil
	}

	gitDir := filepath.Join(resolvedPath, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		m.notify("Not a git repository")
		return m, nil
	}

	name := filepath.Base(resolvedPath)
	absPath = resolvedPath // store the canonical path

	newProject := project.NewProject(name, absPath)
	// Project settings only store explicit user overrides. Empty values
// cascade to global config via the cascading getters on project.Project
// (e.g., GetBranchPrefix(), GetBranchTemplate(), GetSlugMaxLength()).
// There is no agent selection — awp is pi-only per AGENTS.md §3 Rule 1.

	if err := m.projectRegistry.Add(newProject); err != nil {
		m.notify("Failed to save: " + err.Error())
		return m, nil
	}

	m.globalStore.AddProject(newProject)
	m.worktreeMgrs[newProject.ID] = git.NewWorktreeManager(newProject)
	m.selectedProject = newProject
	m.addProjectPath.Blur()
	m.projectListIndex = len(m.globalStore.Projects()) - 1

	if m.mode == ModeCreateProject {
		m.mode = ModeNormal
	}

	m.notify("Added project: " + name)
	return m, nil
}

func (m *Model) nextFormField(isEdit bool) *Model {
	m.blurAllFormFields()
	m.ticketFormField++

	maxField := formFieldBlockedBy
	if !isEdit {
		maxField = formFieldProject
	}

	for {
		if m.ticketFormField > maxField {
			m.ticketFormField = formFieldTitle
		}
		if m.ticketFormField == formFieldBranch && m.branchLocked {
			m.ticketFormField++
			continue
		}
		break
	}
	m.focusCurrentField()
	return m
}

func (m *Model) prevFormField(isEdit bool) *Model {
	m.blurAllFormFields()
	m.ticketFormField--

	maxField := formFieldBlockedBy
	if !isEdit {
		maxField = formFieldProject
	}

	for {
		if m.ticketFormField < formFieldTitle {
			m.ticketFormField = maxField
		}
		if m.ticketFormField == formFieldBranch && m.branchLocked {
			m.ticketFormField--
			continue
		}
		break
	}
	m.focusCurrentField()
	return m
}

func (m *Model) blurAllFormFields() {
	m.titleInput.Blur()
	m.descInput.Blur()
	m.branchInput.Blur()
	m.labelsInput.Blur()
	m.blockerFilterInput.Blur()
	m.projectInput.Blur()
}

func (m *Model) focusCurrentField() {
	switch m.ticketFormField {
	case formFieldTitle:
		m.titleInput.Focus()
	case formFieldDescription:
		m.descInput.Focus()
	case formFieldBranch:
		m.branchInput.Focus()
	case formFieldLabels:
		m.labelsInput.Focus()
	case formFieldPriority:
		break
	case formFieldWorktree:
		break
	case formFieldBlockedBy:
		m.blockerFilterInput.Focus()
	case formFieldProject:
		m.projectInput.Focus()
	}
}

func (m *Model) saveTicketForm(isEdit bool) (tea.Model, tea.Cmd) {
	title := strings.TrimSpace(m.titleInput.Value())
	if title == "" {
		m.notify("Title cannot be empty")
		return m, nil
	}

	if m.selectedProject == nil {
		m.notify("No project selected")
		return m, nil
	}

	desc := strings.TrimSpace(m.descInput.Value())
	branchName := strings.TrimSpace(m.branchInput.Value())
	if branchName == "" {
		branchName = m.generateBranchNameFromTitle(title, m.selectedProject)
	}

	labels := m.parseLabels(m.labelsInput.Value())

	blockedBy := m.collectSelectedBlockers()

	if isEdit && m.editingTicketID != "" {
		ticket, _ := m.globalStore.Get(m.editingTicketID)
		if ticket != nil {
			ticket.Title = title
			ticket.Description = desc
			if !m.branchLocked {
				ticket.BranchName = branchName
			}
			ticket.Labels = labels
			ticket.Priority = m.ticketPriority
			ticket.UseWorktree = m.ticketUseWorktree
			ticket.BlockedBy = blockedBy
			ticket.Touch()
			m.saveTicket(ticket)
			m.refreshColumnTickets()
			m.notify("Updated: " + title)
		}
	} else {
		ticket := board.NewTicket(title, m.selectedProject.ID)
		ticket.Description = desc
		ticket.BranchName = branchName
		ticket.Labels = labels
		ticket.Priority = m.ticketPriority
		ticket.UseWorktree = m.ticketUseWorktree
		ticket.BlockedBy = blockedBy
		ticket.Status = m.columns[m.activeColumn].Status
		m.globalStore.Add(ticket)
		m.refreshColumnTickets()
		m.selectTicketByID(ticket.ID)
		m.saveTicket(ticket)
		m.notify("Created: " + title)
	}

	m.mode = ModeNormal
	m.blurAllFormFields()
	m.editingTicketID = ""
	m.branchLocked = false
	return m, nil
}

func (m *Model) parseLabels(input string) []string {
	if strings.TrimSpace(input) == "" {
		return []string{}
	}
	parts := strings.Split(input, ",")
	var labels []string
	for _, p := range parts {
		label := strings.TrimSpace(p)
		if label != "" {
			labels = append(labels, label)
		}
	}
	return labels
}

type settingsField struct {
	key         string
	label       string
	kind        string
	description string
}

// settingsFields lists every toggleable setting shown in the Settings overlay
// (ModeSettings). Order matters: it's the rendering order in renderSettingsView.
//
// Invariants pinned by internal/ui/settings_test.go:
//   - No "default_agent" row (Pi-only rule per AGENTS.md §3 Rule 1).
//   - Exactly 8 rows (changes here must update TestSettings_FieldCount).
//   - All keys are unique (pinned by TestSettings_KeysAreUnique).
var settingsFields = []settingsField{
	{"theme", "Theme", "theme", "Color theme for the UI"},
	{"confirm_quit", "Confirm Quit", "toggle", "Prompt before quitting with running agents"},
	{"branch_prefix", "Branch Prefix", "text", "Prefix for auto-generated branch names (e.g. task/, feature/)"},
	{"delete_worktree", "Delete Worktree", "toggle", "Remove git worktree when deleting tickets"},
	{"delete_branch", "Delete Branch", "toggle", "Delete git branch when deleting tickets"},
	{"force_cleanup", "Force Cleanup", "toggle", "Force worktree removal even with uncommitted changes"},
	{"sidebar_visible", "Show Sidebar", "toggle", "Toggle the project sidebar visibility"},
	{"filter_project", "Filter Project", "project", "Show only tickets from a specific project"},
}

func (m *Model) handleSettingsMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.settingsEditing {
		return m.handleSettingsEdit(msg)
	}

	switch msg.String() {
	case "j", "down":
		m.settingsIndex++
		if m.settingsIndex >= len(settingsFields) {
			m.settingsIndex = len(settingsFields) - 1
		}
	case "k", "up":
		m.settingsIndex--
		m.settingsIndex = max(m.settingsIndex, 0)
	case "enter", " ":
		return m.enterSettingsEdit()
	case "esc", "q":
		m.mode = ModeNormal
	}
	return m, nil
}

func (m *Model) handleSettingsEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	field := settingsFields[m.settingsIndex]

	if field.kind == "project" {
		m.filterInput.SetValue(m.filterQuery)
		m.filterInput.Focus()
		m.mode = ModeFilter
		return m, textinput.Blink
	}

	if field.kind == "theme" {
		return m.handleThemeNav(msg)
	}

	switch msg.String() {
	case "enter":
		m.applySettingsValue(field.key, m.settingsInput.Value())
		m.settingsEditing = false
		m.settingsInput.Blur()
		m.notify("Settings saved")
		return m, nil
	case "esc":
		m.settingsEditing = false
		m.settingsInput.Blur()
		return m, nil
	}

	var cmd tea.Cmd
	m.settingsInput, cmd = m.settingsInput.Update(msg)
	return m, cmd
}

func (m *Model) handleThemeNav(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	themes := config.ThemeNames()
	if len(themes) == 0 {
		return m, nil
	}

	switch msg.String() {
	case "j", "down":
		m.themeListIndex++
		if m.themeListIndex >= len(themes) {
			m.themeListIndex = 0
		}
		m.applySettingsValue("theme", themes[m.themeListIndex])
	case "k", "up":
		m.themeListIndex--
		if m.themeListIndex < 0 {
			m.themeListIndex = len(themes) - 1
		}
		m.applySettingsValue("theme", themes[m.themeListIndex])
	case "enter":
		m.settingsEditing = false
		m.notify("Theme: " + themes[m.themeListIndex])
	case "esc":
		m.settingsEditing = false
	}

	return m, nil
}

func (m *Model) handleSettingsMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return m, nil
	}

	formTop := (m.height - 10) / 2
	relY := msg.Y - formTop - 3

	if relY >= 0 && relY < len(settingsFields) {
		m.settingsIndex = relY
		return m.enterSettingsEdit()
	}

	return m, nil
}

func (m *Model) handleConfirmMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return m, nil
	}

	formCenterY := m.height / 2
	formCenterX := m.width / 2

	yesX := formCenterX - 10
	noX := formCenterX + 5

	if msg.Y == formCenterY+2 {
		if msg.X >= yesX && msg.X <= yesX+5 {
			m.showConfirm = false
			if m.confirmFn != nil {
				return m, m.confirmFn()
			}
		}
		if msg.X >= noX && msg.X <= noX+4 {
			m.showConfirm = false
		}
	}

	return m, nil
}

func (m *Model) enterSettingsEdit() (tea.Model, tea.Cmd) {
	field := settingsFields[m.settingsIndex]

	switch field.kind {
	case "project":
		m.filterInput.SetValue(m.filterQuery)
		m.filterInput.Focus()
		m.mode = ModeFilter
		return m, textinput.Blink

	case "toggle":
		m.applySettingsValue(field.key, "")
		status := m.getSettingsValue(field.key)
		m.notify(field.label + ": " + status)
		return m, nil

	case "theme":
		themes := config.ThemeNames()
		current := m.config.UI.Theme
		m.themeListIndex = 0
		for i, t := range themes {
			if t == current {
				m.themeListIndex = i
				break
			}
		}
		m.settingsEditing = true
		return m, nil


	case "text":
		m.settingsEditing = true
		m.settingsInput.SetValue(m.getSettingsValue(field.key))
		m.settingsInput.Focus()
		return m, textinput.Blink

	default:
		m.settingsEditing = true
		m.settingsInput.SetValue(m.getSettingsValue(field.key))
		m.settingsInput.Focus()
		return m, textinput.Blink
	}
}
func (m *Model) getSettingsValue(key string) string {
	switch key {
	case "theme":
		return m.config.UI.Theme
	case "confirm_quit":
		if m.config.Behavior.ConfirmQuitWithAgents {
			return "On"
		}
		return "Off"
	case "branch_prefix":
		return m.config.Defaults.BranchPrefix
	case "delete_worktree":
		if m.config.Cleanup.DeleteWorktree {
			return "On"
		}
		return "Off"
	case "delete_branch":
		if m.config.Cleanup.DeleteBranch {
			return "On"
		}
		return "Off"
	case "force_cleanup":
		if m.config.Cleanup.ForceWorktreeRemoval {
			return "On"
		}
		return "Off"
	case "filter_project":
		count := len(m.filterProjectIDs)
		if count == 0 {
			return "All Projects"
		}
		return fmt.Sprintf("%d selected", count)
	case "sidebar_visible":
		if m.sidebarVisible {
			return "On"
		}
		return "Off"
	}
	return ""
}

func (m *Model) applySettingsValue(key, value string) {
	switch key {
	case "theme":
		m.config.UI.Theme = value
		m.theme = m.config.GetTheme()
		m.colors = newUIColors(m.theme)
		m.config.Save("")
	case "confirm_quit":
		m.config.Behavior.ConfirmQuitWithAgents = !m.config.Behavior.ConfirmQuitWithAgents
		m.config.Save("")
	case "branch_prefix":
		m.config.Defaults.BranchPrefix = value
		m.config.Save("")
	case "delete_worktree":
		m.config.Cleanup.DeleteWorktree = !m.config.Cleanup.DeleteWorktree
		m.config.Save("")
	case "delete_branch":
		m.config.Cleanup.DeleteBranch = !m.config.Cleanup.DeleteBranch
		m.config.Save("")
	case "force_cleanup":
		m.config.Cleanup.ForceWorktreeRemoval = !m.config.Cleanup.ForceWorktreeRemoval
		m.config.Save("")
	case "sidebar_visible":
		m.sidebarVisible = !m.sidebarVisible
		m.config.UI.SidebarVisible = m.sidebarVisible
		if !m.sidebarVisible {
			m.sidebarFocused = false
		}
		m.config.Save("")
	}
}

func (m *Model) handleFilterMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.filterInput.Blur()
		m.mode = ModeNormal
		return m, nil
	case "esc":
		m.filterQuery = ""
		m.filterInput.SetValue("")
		m.filterInput.Blur()
		m.mode = ModeNormal
		m.refreshColumnTickets()
		return m, nil
	}

	var cmd tea.Cmd
	m.filterInput, cmd = m.filterInput.Update(msg)
	m.filterQuery = m.filterInput.Value()
	m.refreshColumnTickets()
	return m, cmd
}

func (m *Model) handleFilterMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.filterInput, cmd = m.filterInput.Update(msg)
	return m, cmd
}

func (m *Model) handleCreateProjectMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		return m.createProjectFromPath()
	case "esc":
		m.mode = ModeNormal
		m.addProjectPath.Blur()
		return m, nil
	case "ctrl+c":
		m.mode = ModeNormal
		m.addProjectPath.Blur()
		return m, nil
	}

	prev := m.addProjectPath.Value()
	var cmd tea.Cmd
	m.addProjectPath, cmd = m.addProjectPath.Update(msg)

	// Refresh path-completion suggestions on every value change.
	// (Bubbles' HasPrefix filter is applied against the suggestions
	// list, so we just need to keep the list reflecting what's
	// reachable from the current path.) Refresh on every keystroke
	// is cheap (one os.ReadDir on the resolved parent).
	if m.addProjectPath.Value() != prev {
		m.addProjectPath.SetSuggestions(completePath(m.addProjectPath.Value()))
	}

	return m, cmd
}

func (m *Model) clearFilter() {
	m.filterQuery = ""
	m.filterProjectIDs = make(map[string]bool)
	m.refreshColumnTickets()
}

func (m *Model) toggleProjectFilter(projectID string) {
	if m.filterProjectIDs[projectID] {
		delete(m.filterProjectIDs, projectID)
	} else {
		m.filterProjectIDs[projectID] = true
	}
	m.filterQuery = ""
	m.refreshColumnTickets()
}

func (m *Model) toggleAllProjects() {
	projects := m.globalStore.Projects()
	allSelected := len(m.filterProjectIDs) == len(projects) && len(projects) > 0
	for _, p := range projects {
		if !m.filterProjectIDs[p.ID] {
			allSelected = false
			break
		}
	}

	if allSelected || len(m.filterProjectIDs) == 0 {
		m.filterProjectIDs = make(map[string]bool)
		for _, p := range projects {
			m.filterProjectIDs[p.ID] = true
		}
		m.notify("All projects selected")
	} else {
		m.filterProjectIDs = make(map[string]bool)
		m.notify("All projects deselected")
	}
	m.filterQuery = ""
	m.refreshColumnTickets()
}

func (m *Model) moveColumn(delta int) {
	m.activeColumn += delta
	m.activeColumn = max(m.activeColumn, 0)
	if m.activeColumn >= len(m.columns) {
		m.activeColumn = len(m.columns) - 1
	}
	m.activeTicket = 0
	m.ensureColumnVisible()
	m.ensureTicketVisible()
}

func (m *Model) ensureColumnVisible() {
	colWidth := m.calcColumnWidth()
	visibleCols := m.visibleColumnCount(colWidth)

	if m.activeColumn < m.scrollOffset {
		m.scrollOffset = m.activeColumn
	} else if m.activeColumn >= m.scrollOffset+visibleCols {
		m.scrollOffset = m.activeColumn - visibleCols + 1
	}

	maxOffset := max(len(m.columns)-visibleCols, 0)
	if m.scrollOffset > maxOffset {
		m.scrollOffset = maxOffset
	}
}

func (m *Model) headerHeight() int {
	const (
		content      = 1
		borderBottom = 1
		spacing      = 2
	)
	return content + borderBottom + spacing
}

func (m *Model) calcColumnWidth() int {
	boardW := m.boardWidth()
	if boardW == 0 || len(m.columns) == 0 {
		return minColumnWidth
	}

	numCols := len(m.columns)
	totalOverhead := numCols * columnOverhead
	colWidth := (boardW - totalOverhead) / numCols

	return max(colWidth, minColumnWidth)
}

func (m *Model) visibleColumnCount(colWidth int) int {
	boardW := m.boardWidth()
	if boardW == 0 {
		return len(m.columns)
	}
	visible := boardW / (colWidth + columnOverhead)
	visible = max(visible, 1)
	if visible > len(m.columns) {
		visible = len(m.columns)
	}
	return visible
}

func (m *Model) distributeWidth(numCols int) (baseWidth, remainder int) {
	boardW := m.boardWidth()
	if numCols == 0 || boardW == 0 {
		return minColumnWidth, 0
	}
	borders := numCols * 2
	margins := numCols - 1
	available := boardW - borders - margins
	baseWidth = available / numCols
	remainder = available % numCols
	if baseWidth < minColumnWidth {
		baseWidth = minColumnWidth
		remainder = 0
	}
	return baseWidth, remainder
}

func (m *Model) moveTicket(delta int) {
	if len(m.columnTickets) <= m.activeColumn {
		return
	}
	tickets := m.columnTickets[m.activeColumn]
	m.activeTicket += delta
	m.activeTicket = max(m.activeTicket, 0)
	if m.activeTicket >= len(tickets) {
		m.activeTicket = max(len(tickets)-1, 0)
	}
	m.ensureTicketVisible()
}

func (m *Model) visibleTicketCount() int {
	availableHeight := m.columnContentHeight()
	if availableHeight <= 0 {
		return 1
	}
	count := availableHeight / ticketHeight
	return max(count, 1)
}

func (m *Model) columnContentHeight() int {
	boardHeight := m.height - 4
	contentHeight := boardHeight - columnHeaderHeight - 4
	return contentHeight
}

func (m *Model) ensureTicketVisible() {
	if m.activeColumn < 0 || m.activeColumn >= len(m.columnOffsets) {
		return
	}

	offset := m.columnOffsets[m.activeColumn]
	visible := m.visibleTicketCount()

	if m.activeTicket < offset {
		m.columnOffsets[m.activeColumn] = m.activeTicket
	} else if m.activeTicket >= offset+visible {
		m.columnOffsets[m.activeColumn] = m.activeTicket - visible + 1
	}

	m.columnOffsets[m.activeColumn] = max(m.columnOffsets[m.activeColumn], 0)
}

func (m *Model) createNewTicket() (tea.Model, tea.Cmd) {
	m.mode = ModeCreateTicket
	m.ticketFormField = formFieldTitle
	m.editingTicketID = ""
	m.branchLocked = false

	if len(m.filterProjectIDs) == 1 {
		for id := range m.filterProjectIDs {
			m.selectedProject = m.globalStore.GetProject(id)
			break
		}
	} else if m.selectedProject == nil {
		projects := m.globalStore.Projects()
		if len(projects) > 0 {
			m.selectedProject = projects[0]
		}
	}

	m.projectListIndex = 0
	if m.selectedProject != nil {
		for i, p := range m.globalStore.Projects() {
			if p.ID == m.selectedProject.ID {
				m.projectListIndex = i
				break
			}
		}
	}


	m.titleInput.Reset()
	m.descInput.Reset()
	m.branchInput.Reset()
	m.labelsInput.Reset()
	m.ticketPriority = 3
	m.ticketUseWorktree = true

	m.initBlockerCandidates("")
	m.selectedBlockers = make(map[board.TicketID]bool)
	m.blockerListIndex = 0
	m.blockerFilterInput.Reset()
	m.formScrollOffset = 0

	m.blurAllFormFields()
	m.titleInput.Focus()
	return m, m.titleInput.Cursor.BlinkCmd()
}

func (m *Model) editTicket() (tea.Model, tea.Cmd) {
	ticket := m.selectedTicket()
	if ticket == nil {
		m.notify("No ticket selected")
		return m, nil
	}

	m.mode = ModeEditTicket
	m.ticketFormField = formFieldTitle
	m.editingTicketID = ticket.ID
	m.branchLocked = ticket.WorktreePath != ""
	m.selectedProject = m.globalStore.GetProjectForTicket(ticket)
	m.titleInput.SetValue(ticket.Title)
	m.descInput.SetValue(ticket.Description)
	if ticket.BranchName != "" {
		m.branchInput.SetValue(ticket.BranchName)
	} else if m.selectedProject != nil {
		m.branchInput.SetValue(m.generateBranchNameFromTitle(ticket.Title, m.selectedProject))
	}
	m.labelsInput.SetValue(strings.Join(ticket.Labels, ", "))
	m.ticketPriority = ticket.Priority
	if m.ticketPriority < 1 || m.ticketPriority > 5 {
		m.ticketPriority = 3
	}
	m.ticketUseWorktree = ticket.UseWorktree

	m.initBlockerCandidates(ticket.ID)
	m.selectedBlockers = make(map[board.TicketID]bool)
	for _, blockerID := range ticket.BlockedBy {
		m.selectedBlockers[blockerID] = true
	}
	m.blockerListIndex = 0
	m.blockerFilterInput.Reset()
	m.formScrollOffset = 0

	m.blurAllFormFields()
	m.titleInput.Focus()
	return m, m.titleInput.Cursor.BlinkCmd()
}

func (m *Model) attachToAgent() (tea.Model, tea.Cmd) {
	ticket := m.selectedTicket()
	if ticket == nil {
		m.notify("No ticket selected")
		return m, nil
	}

	pane, ok := m.panes[ticket.ID]
	if !ok || !pane.Running() {
		m.notify("No agent running — press 's' to spawn")
		return m, nil
	}

	m.mode = ModeAgentView
	m.focusedPane = ticket.ID
	paneHeight := m.height - 2
	pane.SetSize(m.width, paneHeight)
	return m, nil
}

func (m *Model) handleDoubleClick() (tea.Model, tea.Cmd) {
	ticket := m.selectedTicket()
	if ticket == nil {
		return m, nil
	}

	pane, ok := m.panes[ticket.ID]
	if ok && pane.Running() {
		return m.attachToAgent()
	}

	return m.spawnAgent()
}

func (m *Model) confirmDeleteTicket() (tea.Model, tea.Cmd) {
	ticket := m.selectedTicket()
	if ticket == nil {
		return m, nil
	}

	proj := m.globalStore.GetProjectForTicket(ticket)
	hasUncommitted := false
	if ticket.WorktreePath != "" && m.config.Cleanup.DeleteWorktree && proj != nil {
		if mgr := m.worktreeMgrs[proj.ID]; mgr != nil {
			var err error
			hasUncommitted, err = mgr.HasUncommittedChanges(ticket.WorktreePath)
			if err != nil {
				hasUncommitted = false
			}
		}
	}

	if hasUncommitted && !m.config.Cleanup.ForceWorktreeRemoval {
		m.showConfirm = true
		m.confirmMsg = "Worktree has uncommitted changes. Force delete?"
		m.confirmFn = func() tea.Cmd {
			m.performTicketCleanup(ticket)
			return nil
		}
	} else {
		m.showConfirm = true
		m.confirmMsg = "Delete ticket: " + ticket.Title + "?"
		m.confirmFn = func() tea.Cmd {
			m.performTicketCleanup(ticket)
			return nil
		}
	}
	return m, nil
}

func (m *Model) performTicketCleanup(ticket *board.Ticket) {
	ticketTitle := ticket.Title // Capture before deletion

	if pane, ok := m.panes[ticket.ID]; ok {
		pane.Stop()
		delete(m.panes, ticket.ID)
		m.turnDoneCaches.Delete(ticket.ID)
	}

	proj := m.globalStore.GetProjectForTicket(ticket)
	if proj != nil {
		mgr := m.worktreeMgrs[proj.ID]
		if mgr != nil {
			if ticket.WorktreePath != "" && m.config.Cleanup.DeleteWorktree {
				err := mgr.RemoveWorktree(ticket.WorktreePath)
				if err != nil {
					m.notify("Failed to remove worktree: " + err.Error())
				}
			}

			if ticket.BranchName != "" && m.config.Cleanup.DeleteBranch {
				err := mgr.DeleteBranch(ticket.BranchName)
				if err != nil {
					m.notify("Failed to delete branch: " + err.Error())
				}
			}
		}
	}

	m.globalStore.RemoveBlockerReferences(ticket.ID)
	m.globalStore.Delete(ticket.ID)
	m.refreshColumnTickets()
	m.globalStore.SaveAll()
	m.notify("Deleted: " + ticketTitle)
}

// toggleSelectedTicket is the Space-key handler. It moves the selected
// ticket to the opposite column (backlog ↔ in_progress) and is its
// own inverse — pressing Space twice on the same ticket leaves it
// in the original column.
//
// In the simplified 2-state model, Space replaces both the old
// "next status" and "previous status" keys (which were Space and
// `-`/backspace in the 4-state model). Direction is implicit: the
// current status determines the target.
//
// Side effects:
//   - If moving backlog → in_progress, sets up worktree/branch as needed
//   - If moving in_progress → backlog with a running agent, the
//     orphan-agent guard (checkOrphanAgentBeforeMove) blocks the move
//   - Refreshes the column view + persists the new status
func (m *Model) toggleSelectedTicket() (tea.Model, tea.Cmd) {
	ticket := m.selectedTicket()
	if ticket == nil {
		return m, nil
	}

	target := m.toggleTicketStatus(ticket.Status)
	if target == ticket.Status {
		// Same-status toggle is a no-op. In 2-state model this branch
		// is unreachable for valid input (both states have an opposite),
		// but the guard is kept defensive.
		return m, nil
	}

	// Caller-side orphan-agent guard: in_progress → backlog is
	// blocked if the agent is still working. The guard is required
	// by the state-machine design (CanTransitionTo is pure, no agent
	// coupling; the UI is responsible for this check).
	if err := m.checkOrphanAgentBeforeMove(ticket, target); err != nil {
		m.notify("Move rejected: " + err.Error())
		return m, nil
	}

	// Side effect: moving to in_progress requires worktree/branch setup
	// (the same setup that happens when the user presses Space to start
	// work on a backlog ticket).
	if target == board.StatusInProgress && ticket.WorktreePath == "" {
		if ticket.UseWorktree {
			if err := m.setupWorktree(ticket); err != nil {
				m.notify("Worktree failed: " + err.Error())
				return m, nil
			}
		} else {
			if err := m.setupMainRepoBranch(ticket); err != nil {
				m.notify("Branch setup failed: " + err.Error())
				return m, nil
			}
		}
	}

	if err := m.globalStore.Move(ticket.ID, target); err != nil {
		m.notify("Move rejected: " + err.Error())
		return m, nil
	}
	m.refreshColumnTickets()
	m.selectTicketByID(ticket.ID)
	m.saveTicket(ticket)
	m.notify("Moved to " + string(target))

	return m, nil
}

// toggleTicketStatus returns the opposite status in the 2-state model:
// backlog ↔ in_progress. Same-status returns current (defensive; in
// practice, the caller filters this out before doing side effects).
//
// This is a pure function on the receiver (doesn't read m.*) and is
// exercised directly by toggle_status_test.go.
func (m *Model) toggleTicketStatus(current board.TicketStatus) board.TicketStatus {
	switch current {
	case board.StatusBacklog:
		return board.StatusInProgress
	case board.StatusInProgress:
		return board.StatusBacklog
	default:
		return current
	}
}

// checkOrphanAgentBeforeMove is the caller-side orphan-agent guard.
// Returns an error if moving ticket to targetStatus would orphan a
// running pi subprocess (e.g., in_progress → backlog with AgentWorking).
//
// FOOT-3 (post-P3P4 audit): this check used to live inside
// board.Ticket.CanTransitionTo, which coupled the board package to
// agent semantics. The check moved here so the board package can be a
// PURE state-machine validator; callers (UI layer) handle runtime
// concerns like agent status.
func (m *Model) checkOrphanAgentBeforeMove(ticket *board.Ticket, targetStatus board.TicketStatus) error {
	if ticket.Status == board.StatusInProgress && targetStatus == board.StatusBacklog && ticket.AgentStatus == board.AgentWorking {
		return fmt.Errorf("cannot move ticket to backlog while agent is %s (stop the agent first)", ticket.AgentStatus)
	}
	return nil
}

func (m *Model) setupWorktree(ticket *board.Ticket) error {
	proj := m.globalStore.GetProjectForTicket(ticket)
	if proj == nil {
		return project.ErrProjectNotFound
	}

	mgr := m.worktreeMgrs[proj.ID]
	if mgr == nil {
		return git.ErrWorktreeManagerNotFound
	}

	branchName := m.generateBranchName(ticket, proj)
	baseBranch, _ := mgr.GetDefaultBranch()

	path, err := mgr.CreateWorktree(branchName, baseBranch)
	if err != nil {
		return err
	}

	ticket.WorktreePath = path
	ticket.BranchName = branchName
	ticket.BaseBranch = baseBranch
	return nil
}

func (m *Model) setupMainRepoBranch(ticket *board.Ticket) error {
	proj := m.globalStore.GetProjectForTicket(ticket)
	if proj == nil {
		return project.ErrProjectNotFound
	}

	mgr := m.worktreeMgrs[proj.ID]
	if mgr == nil {
		return git.ErrWorktreeManagerNotFound
	}

	branchName := m.generateBranchName(ticket, proj)
	baseBranch, _ := mgr.GetDefaultBranch()

	ticket.WorktreePath = proj.RepoPath
	ticket.BranchName = branchName
	ticket.BaseBranch = baseBranch
	return nil
}

func (m *Model) generateBranchNameFromTitle(title string, proj *project.Project) string {
	maxLen := m.getSlugMaxLength(proj)
	slug := board.Slugify(title, maxLen)

	template := m.getBranchTemplate(proj)
	prefix := m.getBranchPrefix(proj)

	result := strings.ReplaceAll(template, "{prefix}", prefix)
	result = strings.ReplaceAll(result, "{slug}", slug)

	return result
}

func (m *Model) generateBranchName(ticket *board.Ticket, proj *project.Project) string {
	if ticket.BranchName != "" {
		return ticket.BranchName
	}
	return m.generateBranchNameFromTitle(ticket.Title, proj)
}

func (m *Model) spawnAgent() (tea.Model, tea.Cmd) {
	ticket := m.selectedTicket()
	if ticket == nil {
		return m, nil
	}

	if ticket.Status != board.StatusInProgress {
		m.notify("Press Space to move to In Progress first")
		return m, nil
	}

	if _, exists := m.panes[ticket.ID]; exists {
		m.notify("Agent already running — press Enter to attach")
		return m, nil
	}

	proj := m.globalStore.GetProjectForTicket(ticket)
	if proj == nil {
		m.notify("Project not found for this ticket")
		return m, nil
	}

	if !ticket.UseWorktree {
		for otherID := range m.panes {
			if otherID == ticket.ID {
				continue
			}
			other, _ := m.globalStore.Get(otherID)
			if other != nil && !other.UseWorktree {
				otherProj := m.globalStore.GetProjectForTicket(other)
				if otherProj != nil && otherProj.ID == proj.ID {
					m.notify("Another main-repo agent is running in this project")
					return m, nil
				}
			}
		}
	}

	// awp is pi-only: use m.config.Pi directly. There is no agent selection
// at runtime. The previous multi-agent abstraction was removed
// 2026-06-22 (see AGENTS.md §3 Rule 1).

	m.mode = ModeSpawning
	m.spawningTicketID = ticket.ID


	return m, tea.Batch(m.spinner.Tick, m.prepareSpawn(ticket, proj))
}

func (m *Model) prepareSpawn(ticket *board.Ticket, proj *project.Project) tea.Cmd {
	ticketID := ticket.ID
	worktreePath := ticket.WorktreePath
	branchName := ticket.BranchName
	baseBranch := ticket.BaseBranch
	useWorktree := ticket.UseWorktree
	width, height := m.width, m.height-2

	mgr := m.worktreeMgrs[proj.ID]
	cfg := m.config

	return func() tea.Msg {
		defer observability.RecoverPanic("prepareSpawn")
		if mgr == nil {
			return spawnErrorMsg{ticketID: ticketID, err: git.ErrWorktreeManagerNotFound.Error()}
		}

		generatedBranch := branchName
		if generatedBranch == "" {
			maxLen := m.getSlugMaxLength(proj)
			slug := board.Slugify(ticket.Title, maxLen)
			template := m.getBranchTemplate(proj)
			prefix := m.getBranchPrefix(proj)
			generatedBranch = strings.ReplaceAll(template, "{prefix}", prefix)
			generatedBranch = strings.ReplaceAll(generatedBranch, "{slug}", slug)
		}

		base, _ := mgr.GetDefaultBranch()
		if baseBranch != "" {
			base = baseBranch
		}

		if useWorktree {
			if worktreePath == "" {
				path, err := mgr.CreateWorktree(generatedBranch, base)
				if err != nil {
					return spawnErrorMsg{ticketID: ticketID, err: "worktree failed: " + err.Error()}
				}
				worktreePath = path
			}
		} else {
			if err := mgr.SetupBranch(generatedBranch, base); err != nil {
				return spawnErrorMsg{ticketID: ticketID, err: "branch setup failed: " + err.Error()}
			}
			worktreePath = proj.RepoPath
		}
		branchName = generatedBranch
		baseBranch = base

		pane := terminal.New(string(ticketID), width, height, 0)
		pane.SetWorkdir(worktreePath)

		// Set session name for terminal identification (priority: AgentSessionID > branch > ticket)
		sessionName := string(ticketID)
		if branchName != "" {
			sessionName = branchName
		}
		if ticket.AgentSessionID != "" {
			sessionName = ticket.AgentSessionID
		}
		pane.SetSessionName(sessionName)

		isNewSession := ticket.AgentSpawnedAt == nil
		args := make([]string, len(cfg.Pi.Args))
		copy(args, cfg.Pi.Args)

		// pi uses its default interactive TUI mode (no --mode flag).
		// The agent runs interactively, user types into pi's TUI via
		// PTY. We just render pi's terminal output and forward keys.
		//
		// If resuming an existing session (AgentSpawnedAt != nil),
		// append --continue unless the user has already configured
		// it via Pi.Args.
		if isNewSession {
			if cfg.Pi.InitPrompt != "" {
				// Pass the ticket context as a POSITIONAL user message,
				// not as --append-system-prompt. Reason: pi in interactive
				// TUI mode only auto-executes on initial user messages
				// (see pi-mono packages/coding-agent/src/cli/args.ts where
				// any non-flag arg is pushed to result.messages, and
				// packages/coding-agent/src/modes/interactive/interactive-mode.ts
				// which processes initialMessages via
				// `await this.session.prompt(message)` on startup).
				// --append-system-prompt only adds system-level context
				// that pi sees passively — it does NOT trigger a turn.
				// User reported: "spawned agent 进入的时候 ticket 没执行" —
				// the ticket context was being delivered as system
				// context only, so pi booted and sat idle waiting for
				// the user to type. The InitPrompt template's closing
				// directive ("Begin by analyzing the ticket requirements
				// and proposing your approach") is meant as a USER
				// REQUEST, not a system reminder; positional arg is the
				// right mechanism for it.
				//
				// pi 0.80 does NOT recognize --init (regression-tested
				// in test/pi/spawn_args_test.go); it would exit with
				// "Unknown option: --init", which manifests in awp as
				// the spawned pane crashing immediately ("first spawn
				// 闪退"). The positional-arg mechanism is independent
				// of that historical bug.
				//
				// cfg.Pi.InitPrompt is a Go text/template string with
				// placeholders like {{.Title}}, {{.Description}},
				// {{.BranchName}}, {{.BaseBranch}}. Render it
				// against the ticket before passing to pi — otherwise
				// pi receives the raw template (e.g. "Title: {{.Title}}")
				// instead of the actual ticket data.
				//
				// Pass the EFFECTIVE branchName/baseBranch/worktreePath
				// (the values awp is about to use, not the ticket's
				// possibly-stale persisted values). Without this, a new
				// ticket with ticket.BranchName=="" would render
				// {{.BranchName}} as empty even though awp just
				// generated "task/<slug>" — pi then sees a misleading
				// prompt (regression-tested in init_prompt_test.go).
				rendered, err := renderInitPrompt(cfg.Pi.InitPrompt, ticket, branchName, baseBranch, worktreePath)
				if err != nil {
					return spawnErrorMsg{ticketID: ticketID, err: "render init prompt: " + err.Error()}
				}
				args = append(args, rendered)
			}
		} else {
			hasFlag := false
			for _, arg := range args {
				if arg == "--continue" || arg == "-c" {
					hasFlag = true
					break
				}
			}
			if !hasFlag {
				args = append(args, "--continue")
			}
		}

		return spawnReadyMsg{
			ticketID:     ticketID,
			pane:         pane,
			command:      cfg.Pi.Command,
			args:         args,
			worktreePath: worktreePath,
			branchName:   branchName,
			baseBranch:   baseBranch,
		}
	}
}

func (m *Model) stopAgent() (tea.Model, tea.Cmd) {
	ticket := m.selectedTicket()
	if ticket == nil {
		return m, nil
	}

	if pane, ok := m.panes[ticket.ID]; ok {
		pane.Stop()
		delete(m.panes, ticket.ID)
		m.turnDoneCaches.Delete(ticket.ID)
	}

	ticket.AgentStatus = board.AgentNone
	m.saveTicket(ticket)
	m.notify("Agent stopped")
	return m, nil
}

func (m *Model) selectedTicket() *board.Ticket {
	if len(m.columnTickets) <= m.activeColumn {
		return nil
	}
	tickets := m.columnTickets[m.activeColumn]
	if len(tickets) <= m.activeTicket {
		return nil
	}
	return tickets[m.activeTicket]
}

func (m *Model) selectTicketByID(ticketID board.TicketID) {
	for colIdx, tickets := range m.columnTickets {
		for ticketIdx, t := range tickets {
			if t.ID == ticketID {
				m.activeColumn = colIdx
				m.activeTicket = ticketIdx
				m.ensureTicketVisible()
				return
			}
		}
	}
}

func (m *Model) refreshColumnTickets() {
	m.columnTickets = make([][]*board.Ticket, len(m.columns))
	for i, col := range m.columns {
		allForStatus := m.globalStore.GetByStatus(col.Status)
		var filtered []*board.Ticket
		for _, t := range allForStatus {
			if !m.ticketMatchesFilter(t) {
				continue
			}
			filtered = append(filtered, t)
		}
		m.columnTickets[i] = filtered
	}

	if len(m.columnOffsets) != len(m.columns) {
		m.columnOffsets = make([]int, len(m.columns))
	}
}

func (m *Model) ticketMatchesFilter(t *board.Ticket) bool {
	if len(m.filterProjectIDs) > 0 && !m.filterProjectIDs[t.ProjectID] {
		return false
	}
	if m.filterQuery == "" {
		return true
	}

	query := strings.ToLower(m.filterQuery)

	if strings.HasPrefix(query, "@") {
		parts := strings.SplitN(query, " ", 2)
		projectName := strings.TrimPrefix(parts[0], "@")
		proj := m.globalStore.GetProjectForTicket(t)
		if proj == nil || !strings.Contains(strings.ToLower(proj.Name), projectName) {
			return false
		}
		if len(parts) == 1 {
			return true
		}
		query = strings.TrimSpace(parts[1])
	}

	title := strings.ToLower(t.Title)
	desc := strings.ToLower(t.Description)
	return strings.Contains(title, query) || strings.Contains(desc, query)
}

func (m *Model) notify(msg string) {
	m.notification = msg
	m.notifyTime = time.Now()
}

// notifyExit produces a TUI toast announcing that the given ticket's
// pi process has exited. Wording depends on focus state (was the
// user watching this pane?) and whether the exit was a clean shutdown
// or a crash.
//
// Wording matrix (matches SYSTEM_DESIGN.md §7.4.3):
//
//	                  │ focused                    │ non-focused
//	──────────────────┼────────────────────────────┼────────────────────────
//	clean exit (err=nil) │ "Agent exited"         ✓  │ "<title> exited"   ✓
//	crash  (err != nil)  │ "Agent failed: <err>"  ✗  │ "<title> failed"   ✗
//
// The view layer's icon picker (view.go:471-484) detects error
// notifications by "Failed" prefix or "failed" substring. We match
// that convention so the ✗ icon is rendered automatically.
func (m *Model) notifyExit(ticketID board.TicketID, exitErr error, wasFocused bool) {
	title := string(ticketID)
	if t, _ := m.globalStore.Get(ticketID); t != nil && t.Title != "" {
		title = t.Title
	}
	var msg string
	switch {
	case wasFocused && exitErr != nil:
		msg = "Agent failed: " + exitErr.Error()
	case wasFocused:
		msg = "Agent exited"
	case exitErr != nil:
		msg = title + " failed"
	default:
		msg = title + " exited"
	}
	m.notify(msg)
}

// handlePaneTurnDone is called from Update when pollTurnDonesAsync
// emits a paneTurnDoneMsg (PR 2 of task/awp). The poll loop has
// already done the heavy lifting: detected a "toolUse → stop"
// transition in pi's session JSONL and applied the focus filter —
// the handler here just decides what to put in the toast.
//
// Focus policy: if msg.paneID equals the currently-focused pane,
// the user can already see the state; do NOT spam them with a toast.
// Anything else (different pane, no focus at all) gets a toast with
// the ticket title (or ticket ID as fallback if the title is empty).
func (m *Model) handlePaneTurnDone(msg paneTurnDoneMsg) {
	if string(m.focusedPane) == msg.paneID {
		return
	}
	title := msg.title
	if title == "" {
		title = string(msg.ticketID)
	}
	m.notify(title + " finished a turn")
}

func (m *Model) saveTicket(ticket *board.Ticket) {
	if err := m.globalStore.Save(ticket); err != nil {
		m.notify("Failed to save: " + err.Error())
	}
}

func (m *Model) resetSpawnState(ticketID board.TicketID) {
	if ticket, _ := m.globalStore.Get(ticketID); ticket != nil {
		ticket.AgentSpawnedAt = nil
		ticket.AgentStatus = board.AgentNone
		m.saveTicket(ticket)
	}
	m.mode = ModeNormal
	m.spawningTicketID = ""

	// Stop and remove the pane. Without Stop, the altScreenConsumer
	// goroutine leaks (channel never closed) and the PTY fd stays
	// open. Symptom: subsequent spawns may stall because the previous
	// pane's resources weren't cleaned up.
	if pane, ok := m.panes[ticketID]; ok {
		_ = pane.Stop()
	}
	delete(m.panes, ticketID)
	m.turnDoneCaches.Delete(ticketID)
}

func (m *Model) RunningAgentCount() int {
	count := 0
	for _, pane := range m.panes {
		if pane.Running() {
			count++
		}
	}
	return count
}

func (m *Model) getBranchPrefix(proj *project.Project) string {
	if proj != nil && proj.Settings.BranchPrefix != "" {
		return proj.Settings.BranchPrefix
	}
	if m.config.Defaults.BranchPrefix != "" {
		return m.config.Defaults.BranchPrefix
	}
	return "task/"
}

func (m *Model) getBranchTemplate(proj *project.Project) string {
	if proj != nil && proj.Settings.BranchTemplate != "" {
		return proj.Settings.BranchTemplate
	}
	if m.config.Defaults.BranchTemplate != "" {
		return m.config.Defaults.BranchTemplate
	}
	return "{prefix}{slug}"
}

func (m *Model) getSlugMaxLength(proj *project.Project) int {
	if proj != nil && proj.Settings.SlugMaxLength > 0 {
		return proj.Settings.SlugMaxLength
	}
	if m.config.Defaults.SlugMaxLength > 0 {
		return m.config.Defaults.SlugMaxLength
	}
	return 40
}

func (m *Model) Cleanup() {
	for _, pane := range m.panes {
		if pane.Running() {
			pane.StopGraceful(3 * time.Second)
		}
	}
}

func (m *Model) pollAgentStatusesAsync() tea.Cmd {
	type paneInfo struct {
		ticketID     board.TicketID
		piState      board.PiState
		worktreePath string
		branchName   string
		running      bool
	}

	var panes []paneInfo
	for ticketID, pane := range m.panes {
		ticket, _ := m.globalStore.Get(ticketID)
		if ticket == nil {
			continue
		}
		worktreePath := pane.GetWorkdir()
		if worktreePath == "" {
			worktreePath = ticket.WorktreePath
		}
		panes = append(panes, paneInfo{
			ticketID:     ticketID,
			piState:      ticket.PiState,
			worktreePath: worktreePath,
			branchName:   ticket.BranchName,
			running:      pane.Running(),
		})
	}

	return func() tea.Msg {
		defer observability.RecoverPanic("collectAgentStatus")
		results := make(agentStatusResultMsg)
		for _, p := range panes {
			if !p.running {
				results[p.ticketID] = board.AgentNone
				continue
			}

			// Map PiState to AgentStatus for UI display. PiState is
			// the canonical source (driven by JSONL events from pi);
			// AgentStatus is the legacy field the UI reads.
			results[p.ticketID] = piStateToAgentStatus(p.piState)
		}
		return results
	}
}

// pollTurnDonesAsync — PR 2 of task/awp. Companion to
// pollAgentStatusesAsync: runs in a goroutine every 5 s (via the
// same tickAgentStatus cycle), checks each running pane's pi session
// JSONL for a "toolUse → stop" transition, and emits a
// paneTurnDoneMsg for every transition found.
//
// The actual heavy lifting (file stat, JSONL parse, edge detection)
// lives in this closure, NOT in Update. Update only sees a flat
// list of "this pane just finished a turn" events.
//
// Caching: per-pane state is held in m.turnDoneCaches (sync.Map).
// Each *pi.TurnDoneCache is itself goroutine-safe (sync.Mutex on
// every method), so concurrent access from this goroutine and any
// Update handler is safe.
func (m *Model) pollTurnDonesAsync() tea.Cmd {
	type paneSnap struct {
		ticketID     board.TicketID
		paneID       string
		title        string
		worktreePath string
		running      bool
	}

	var snaps []paneSnap
	for ticketID, pane := range m.panes {
		if !pane.Running() {
			continue
		}
		ticket, _ := m.globalStore.Get(ticketID)
		if ticket == nil {
			continue
		}
		worktreePath := pane.GetWorkdir()
		if worktreePath == "" {
			worktreePath = ticket.WorktreePath
		}
		snaps = append(snaps, paneSnap{
			ticketID:     ticketID,
			paneID:       string(ticketID),
			title:        ticket.Title,
			worktreePath: worktreePath,
			running:      true, // pane.Running() was true above
		})
	}

	return func() tea.Msg {
		defer observability.RecoverPanic("collectTurnDone")
		var fires []paneTurnDoneMsg
		for _, s := range snaps {
			jsonlPath, err := pi.LatestSessionJSONL(s.worktreePath)
			if err != nil || jsonlPath == "" {
				continue // pi hasn't created the session yet — skip
			}

			// Get-or-init the per-pane cache. Corrected 2026-07-07
			// after a sync.Map race panicked awp (see
			// turn_done_cache_race_test.go for the repro). The old
			// two-phase pattern stored a nil placeholder, which a
			// concurrent goroutine's LoadOrStore would see and return
			// (nil, true) without overwriting — causing
			// `actual.(*pi.TurnDoneCache)` to panic on nil.
			//
			// New pattern: Load first (no write), then LoadOrStore
			// with the freshly-built value. No nil placeholder.
			var cache *pi.TurnDoneCache
			if existing, ok := m.turnDoneCaches.Load(s.ticketID); ok && existing != nil {
				cache = existing.(*pi.TurnDoneCache)
			} else {
				fresh, ferr := pi.NewTurnDoneCacheFromFile(jsonlPath)
				if ferr != nil || fresh == nil {
					continue
				}
				actual, loaded := m.turnDoneCaches.LoadOrStore(s.ticketID, fresh)
				if loaded && actual != nil {
					cache = actual.(*pi.TurnDoneCache)
					_ = fresh // another poll beat us; discard our copy
				} else {
					cache = fresh
				}
			}
			if cache == nil {
				continue
			}
			// The cache's own sync.Mutex protects concurrent access
			// between this goroutine and any Update handler. The
			// sync.Map above protects the (ticketID -> cache) mapping;
			// the cache itself protects its fields. Two layers.

			stat, err := os.Stat(cache.Path())
			if err != nil {
				continue // file gone (rotated?) — next poll will pick up new path
			}
			if !cache.IsStale(stat.ModTime(), stat.Size()) {
				continue // unchanged; skip the parse
			}

			sr, err := pi.DetectLastStopReason(cache.Path())
			if err != nil {
				continue
			}
			if cache.Update(sr, stat.Size(), stat.ModTime()) {
				fires = append(fires, paneTurnDoneMsg{
					ticketID: s.ticketID,
					paneID:   s.paneID,
					title:    s.title,
				})
			}
		}
		if len(fires) == 0 {
			// Return an empty pollTurnDonesMsg (NOT nil) so the
			// dispatcher's type switch is explicit. Returning nil
			// would still work — Bubble Tea silently skips nil msgs
			// — but it's fragile: any future code that does
			// msg.(*pollTurnDonesMsg) on a nil would panic.
			return pollTurnDonesMsg{}
		}
		// Wrap multiple fires into a single Msg so the dispatch path
		// stays cheap. Update iterates and calls handlePaneTurnDone
		// per fire. We could also return fires[0] and chain the rest
		// via tea.Batch, but a single Msg is simpler and 10 fires
		// at once is fine for the toast queue (view.go:471 only
		// shows the latest one anyway).
		return pollTurnDonesMsg{fires: fires}
	}
}

// pollTurnDonesMsg is the per-cycle batch of turn-done events from
// pollTurnDonesAsync. A single Msg carries all events from one poll
// so we don't fan out N tea.Cmd back-to-back.
type pollTurnDonesMsg struct {
	fires []paneTurnDoneMsg
}

// piStateToAgentStatus translates pi's JSONL-driven PiState into the
// legacy AgentStatus enum the TUI uses for display. Both are
// defined in internal/board.
func piStateToAgentStatus(s board.PiState) board.AgentStatus {
	switch s {
	case board.PiStateStreaming, board.PiStateThinking, board.PiStateToolCall:
		return board.AgentWorking
	case board.PiStateAwaitingUser:
		return board.AgentWaiting
	case board.PiStateCompacting, board.PiStateRetrying:
		return board.AgentWorking
	case board.PiStateIdle, board.PiStateStarting, board.PiStateNone:
		return board.AgentIdle
	case board.PiStateError, board.PiStateExited:
		return board.AgentError
	default:
		return board.AgentIdle
	}
}

func (m *Model) handleTerminalMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	for _, pane := range m.panes {
		if cmd := pane.Update(msg); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return m, tea.Batch(cmds...)
}

type agentStatusMsg time.Time
type agentStatusResultMsg map[board.TicketID]board.AgentStatus
type notificationMsg time.Time
type shutdownCompleteMsg struct{}
type updateCheckMsg update.CheckResult

// paneTurnDoneMsg fires when the poll loop detects a "toolUse → stop"
// transition in a non-focused pane's pi session JSONL. PR 2 of
// task/awp. The handler converts this into a TUI toast subject to
// the focus policy (focused pane is silent).
type paneTurnDoneMsg struct {
	ticketID board.TicketID
	paneID   string
	title    string
}

type spawnReadyMsg struct {
	ticketID     board.TicketID
	pane         *terminal.Pane
	command      string
	args         []string
	worktreePath string
	branchName   string
	baseBranch   string
}

type spawnErrorMsg struct {
	ticketID board.TicketID
	err      string
}

// tickAgentStatus emits an agentStatusMsg after the given delay. The
// case agentStatusMsg handler re-arms unconditionally so the
// status-poll loop runs forever. Used by Init() and on every
// agentStatusMsg (model.go:483-488).
func tickAgentStatus(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return agentStatusMsg(t)
	})
}

// notificationDuration is how long a toast stays on screen before
// the periodic tick auto-dismisses it. Encoded as a constant so
// tests can reason about the threshold without magic numbers.
// See SYSTEM_DESIGN.md §7.4.4 for the full design rationale.
const notificationDuration = 3 * time.Second

// notificationTickInterval is how often the notification tick fires
// while a toast is visible. 500ms is fine for sub-second user
// perception and keeps CPU usage trivial.
// See SYSTEM_DESIGN.md §7.4.4 for the full design rationale.
const notificationTickInterval = 500 * time.Millisecond

// tickNotification emits a notificationMsg after the given delay.
// The model.Update handler for notificationMsg uses this to
// auto-dismiss toasts after notificationDuration.
//
// The tick is self-sustaining: the handler re-arms it unconditionally
// on every notificationMsg (matching the tickAgentStatus pattern).
// The cost is one 500ms timer + a single string compare per tick
// when no toast is on screen — negligible. Conditional re-arm
// ("only tick when state is X") breaks Init()-based one-shot ticks:
// if the state is empty when Init's first tick fires, the tick dies
// and is never restarted, recreating the original "feature does
// nothing" bug. See NOTIFY_DIAGNOSIS.md §6 for the regression
// history.
func tickNotification(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return notificationMsg(t)
	})
}

// renderInitPrompt executes cfg.Pi.InitPrompt (a Go text/template)
// against the ticket, returning the rendered string.
//
// Variables available in the template:
//
//   - .Title            — ticket title
//   - .Description      — ticket description
//   - .BranchName       — EFFECTIVE branch name (the one awp will
//                         actually use to create the worktree; for
//                         a new ticket this is the slugified title
//                         with the configured prefix, NOT whatever
//                         happens to be persisted on ticket.BranchName)
//   - .BaseBranch       — EFFECTIVE base branch (git default, or
//                         whatever the user pinned on the ticket)
//   - .WorktreePath     — EFFECTIVE worktree path (the directory
//                         pi will be started in)
//
// All other fields on board.Ticket are also accessible by name
// (Status, Priority, Labels, ID, ProjectID, CreatedAt, PiModel,
// PiState, etc. — see internal/board/board.go for the full list).
//
// BranchName/BaseBranch/WorktreePath are passed in explicitly
// rather than read from ticket.* because they may have been
// generated during prepareSpawn (e.g. from the title slug) and
// not yet persisted back onto the ticket. Rendering against a
// shallow clone with the effective values substituted gives pi
// the same context the user sees in the awp UI.
//
// If the template fails to parse or execute, the error is returned
// so the spawn flow surfaces it as a spawnErrorMsg (rather than
// silently passing a broken prompt to pi).
func renderInitPrompt(tmplStr string, ticket *board.Ticket, branchName, baseBranch, worktreePath string) (string, error) {
	// Shallow-clone the ticket so we don't mutate the caller's
	// struct (which may be the live pointer in GlobalTicketStore
	// and shared with the bubble tea Update loop). Substituting
	// effective values here keeps the rendered prompt in sync with
	// the worktree awp is actually about to create.
	view := *ticket
	if branchName != "" {
		view.BranchName = branchName
	}
	if baseBranch != "" {
		view.BaseBranch = baseBranch
	}
	if worktreePath != "" {
		view.WorktreePath = worktreePath
	}

	tmpl, err := template.New("init").Option("missingkey=zero").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, &view); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}
	return buf.String(), nil
}
