package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Inzaniak/rts/internal/core"
	"github.com/Inzaniak/rts/internal/editor"
	"github.com/Inzaniak/rts/internal/service"
)

type mode int

const (
	modeList mode = iota
	modeDetail
	modeFilter
	modeCreate
	modeConfirmDelete
	modeHelp
)

type model struct {
	service      *service.Service
	project      string
	resources    []core.Resource
	filtered     []core.Resource
	harnessTabs  []harnessTab
	kindTabs     []kindTab
	scopeTabs    []scopeTab
	harnessIndex int
	kindIndex    int
	scopeIndex   int
	filterFocus  bool
	filterRow    int
	cursor       int
	detailOffset int
	mode         mode
	helpReturn   mode
	input        string
	status       string
	width        int
	height       int
	noColor      bool
}

type tab[T any] struct {
	label string
	value T
}

type harnessTab = tab[core.Harness]
type kindTab = tab[core.Kind]
type scopeTab = tab[core.Scope]

type editorDone struct {
	err error
}

func Run(svc *service.Service, project string, noColor bool) error {
	resources, err := svc.Inventory(project, service.Filters{})
	if err != nil {
		return err
	}
	m := newModel(svc, project, resources, noColor)
	_, err = tea.NewProgram(m).Run()
	return err
}

func newModel(svc *service.Service, project string, resources []core.Resource, noColor bool) *model {
	m := &model{
		service: svc, project: project, resources: resources, noColor: noColor,
		harnessTabs: buildHarnessTabs(svc),
		kindTabs:    buildKindTabs(resources),
		scopeTabs:   buildScopeTabs(resources),
	}
	m.applyFilter()
	return m
}

func (m *model) Init() tea.Cmd { return nil }

func (m *model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case editorDone:
		if msg.err != nil {
			m.status = "editor: " + msg.err.Error()
			return m, nil
		}
		m.reload()
		m.status = "Resource edited"
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case tea.MouseClickMsg:
		m.handleMouse(msg)
		return m, nil
	case tea.MouseWheelMsg:
		m.handleMouseWheel(msg)
		return m, nil
	}
	return m, nil
}

func (m *model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if msg.Keystroke() == "shift+tab" {
		key = "shift+tab"
	}
	if m.mode == modeHelp {
		switch key {
		case "?", "esc", "enter":
			m.mode = m.helpReturn
		case "q", "ctrl+c":
			return m, tea.Quit
		}
		return m, nil
	}
	if m.mode == modeFilter || m.mode == modeCreate {
		switch key {
		case "esc":
			m.mode, m.input = modeList, ""
		case "backspace":
			if len(m.input) > 0 {
				runes := []rune(m.input)
				m.input = string(runes[:len(runes)-1])
			}
			m.applyFilter()
		case "enter":
			if m.mode == modeFilter {
				m.mode = modeList
			} else {
				m.createSelected()
			}
		default:
			if text := msg.Key().Text; text != "" {
				m.input += text
				if m.mode == modeFilter {
					m.applyFilter()
				}
			}
		}
		return m, nil
	}
	if m.mode == modeConfirmDelete {
		switch key {
		case "y", "Y":
			m.deleteSelected()
		case "n", "N", "esc":
			m.mode = modeList
		}
		return m, nil
	}
	if m.filterFocus {
		switch key {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "tab", "shift+tab", "backtab", "enter", "esc":
			m.filterFocus = false
			m.status = "Resource list"
		case "up", "k":
			m.filterRow = (m.filterRow + 2) % 3
			m.status = "Filter: " + m.activeFilterName()
		case "down", "j":
			m.filterRow = (m.filterRow + 1) % 3
			m.status = "Filter: " + m.activeFilterName()
		case "left":
			m.cycleActiveFilter(-1)
		case "right":
			m.cycleActiveFilter(1)
		case "space":
			m.toggleCurrent()
		}
		return m, nil
	}
	switch key {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "tab", "shift+tab", "backtab":
		m.filterFocus = true
		m.mode = modeList
		m.detailOffset = 0
		m.status = "Filters: ↑/↓ category • ←/→ value • tab return"
	case "down":
		if m.mode == modeDetail {
			m.scrollDetail(1)
		} else if m.cursor+1 < len(m.filtered) {
			m.cursor++
			m.detailOffset = 0
		}
	case "j":
		if m.mode != modeDetail && m.cursor+1 < len(m.filtered) {
			m.cursor++
			m.detailOffset = 0
		}
	case "up":
		if m.mode == modeDetail {
			m.scrollDetail(-1)
		} else if m.cursor > 0 {
			m.cursor--
			m.detailOffset = 0
		}
	case "k":
		if m.mode != modeDetail && m.cursor > 0 {
			m.cursor--
			m.detailOffset = 0
		}
	case "left":
		if m.mode == modeDetail {
			m.switchDetail(-1)
		}
	case "right":
		if m.mode == modeDetail {
			m.switchDetail(1)
		}
	case "g", "home":
		if m.mode == modeDetail {
			m.detailOffset = 0
		} else {
			m.cursor = 0
			m.detailOffset = 0
		}
	case "G", "end":
		if m.mode == modeDetail {
			m.scrollDetailToEnd()
		} else if len(m.filtered) > 0 {
			m.cursor = len(m.filtered) - 1
			m.detailOffset = 0
		}
	case "pgdown":
		m.scrollDetail(m.detailPageSize())
	case "pgup":
		m.scrollDetail(-m.detailPageSize())
	case "enter":
		if len(m.filtered) > 0 {
			if m.mode == modeDetail {
				m.mode = modeList
			} else {
				m.mode = modeDetail
				m.detailOffset = 0
			}
		}
	case "esc":
		m.mode = modeList
		m.detailOffset = 0
	case "/":
		m.mode, m.input = modeFilter, ""
	case "n":
		if len(m.filtered) > 0 {
			m.mode, m.input = modeCreate, ""
		}
	case "d":
		if selected := m.selected(); selected != nil && selected.Has(core.CanDelete) {
			m.mode = modeConfirmDelete
		}
	case "space":
		m.toggleCurrent()
	case "e":
		return m, m.editSelected()
	case "r":
		m.reload()
	case "?":
		m.helpReturn = m.mode
		m.mode = modeHelp
	}
	return m, nil
}

func (m *model) toggleCurrent() {
	if selected := m.selected(); selected != nil && selected.Enabled != nil && selected.Has(core.CanEnable) {
		m.toggleSelected(!*selected.Enabled)
	}
}

func (m *model) handleMouse(msg tea.MouseClickMsg) {
	if msg.Button != tea.MouseLeft {
		return
	}
	switch m.mode {
	case modeFilter, modeCreate, modeConfirmDelete, modeHelp:
		return
	}
	switch msg.Y {
	case 2:
		labels := tabLabels(m.harnessTabs)
		if index := tabIndexAt("Harness", labels, m.harnessIndex, m.width, msg.X); index >= 0 {
			m.selectHarness(index)
			m.focusFilterRow(0)
		}
	case 3:
		labels := tabLabels(m.kindTabs)
		if index := tabIndexAt("Kind", labels, m.kindIndex, m.width, msg.X); index >= 0 {
			m.selectKind(index)
			m.focusFilterRow(1)
		}
	case 4:
		labels := tabLabels(m.scopeTabs)
		if index := tabIndexAt("Scope", labels, m.scopeIndex, m.width, msg.X); index >= 0 {
			m.selectScope(index)
			m.focusFilterRow(2)
		}
	}
}

func (m *model) handleMouseWheel(msg tea.MouseWheelMsg) {
	const wheelStep = 3

	delta := 0
	switch msg.Button {
	case tea.MouseWheelUp:
		delta = -wheelStep
	case tea.MouseWheelDown:
		delta = wheelStep
	default:
		return
	}

	switch m.mode {
	case modeDetail:
		m.scrollDetail(delta)
	case modeList:
		if len(m.filtered) == 0 {
			return
		}
		m.cursor = min(len(m.filtered)-1, max(0, m.cursor+delta))
		m.detailOffset = 0
		m.filterFocus = false
	}
}

func (m *model) focusFilterRow(row int) {
	m.filterFocus = true
	m.filterRow = row
	m.status = "Filter: " + m.activeFilterName() + " • ←/→ value • tab return"
}

func (m *model) View() tea.View {
	content := m.render()
	view := tea.NewView(content)
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	view.WindowTitle = "RTS Harness Manager"
	return view
}

func (m *model) render() string {
	if m.mode == modeHelp {
		return m.renderHelp()
	}
	title := m.style(true, "#7D56F4").Render("RTS  Harness Configuration Manager")
	subtitle := "user + project inventory"
	if m.project != "" {
		subtitle = m.project
	}
	header := title + "\n" + m.style(false, "#888888").Render(subtitle) +
		"\n" + m.renderTabs("Harness", tabLabels(m.harnessTabs), m.harnessIndex, m.filterFocus && m.filterRow == 0) +
		"\n" + m.renderTabs("Kind", tabLabels(m.kindTabs), m.kindIndex, m.filterFocus && m.filterRow == 1) +
		"\n" + m.renderTabs("Scope", tabLabels(m.scopeTabs), m.scopeIndex, m.filterFocus && m.filterRow == 2)
	if m.mode == modeDetail {
		return header + "\n\n" + m.renderDetail()
	}
	var rows []string
	rows = append(rows, m.style(true, "#AAAAAA").Render(
		fmt.Sprintf("  %-10s %-14s %-9s %-5s %-24s %s", "HARNESS", "KIND", "SCOPE", "STATE", "NAME", "PATH"),
	))
	maxRows := m.height - 11
	if maxRows < 5 {
		maxRows = 5
	}
	start := 0
	if m.cursor >= maxRows {
		start = m.cursor - maxRows + 1
	}
	end := min(len(m.filtered), start+maxRows)
	for index := start; index < end; index++ {
		resource := m.filtered[index]
		marker := "  "
		if index == m.cursor {
			marker = "› "
		}
		disabled := resource.Enabled != nil && !*resource.Enabled
		state := "ON"
		if disabled {
			state = "OFF"
		}
		row := fmt.Sprintf("%s%-10s %-14s %-9s %-5s %-24s %s",
			marker, resource.Harness, resource.Kind, resource.Scope, state, truncate(resource.Name, 24),
			truncate(resource.Path, max(20, m.width-73)))
		if index == m.cursor {
			foreground := "#FFFFFF"
			if disabled {
				foreground = "#FF5F56"
			}
			row = m.style(true, foreground).Background(lipgloss.Color("#5A3EA6")).Render(row)
		} else if disabled {
			row = m.style(true, "#FF5F56").Render(row)
		}
		rows = append(rows, row)
	}
	if len(m.filtered) == 0 {
		rows = append(rows, "\n  No resources found.")
	}
	footer := m.status
	switch m.mode {
	case modeFilter:
		footer = "Search: " + m.input + "█"
	case modeCreate:
		selected := m.selected()
		if selected != nil {
			footer = fmt.Sprintf("New %s name for %s/%s: %s█", selected.Kind, selected.Harness, selected.Scope, m.input)
		}
	case modeConfirmDelete:
		footer = "Delete selected resource? This creates a backup. [y/N]"
	default:
		if footer == "" {
			footer = "tab filters • j/k move • enter details • / search • ? help • q quit"
		}
	}
	return header + "\n\n" + strings.Join(rows, "\n") + "\n\n" + m.style(false, "#888888").Render(footer)
}

func (m *model) renderHelp() string {
	title := m.style(true, "#7D56F4").Render("RTS  Keyboard Shortcuts")
	section := func(name string) string {
		return m.style(true, "#AAAAAA").Render(name)
	}
	shortcut := func(keys, action string) string {
		return fmt.Sprintf("  %-22s %s", keys, action)
	}
	lines := []string{
		title,
		"",
		section("Global"),
		shortcut("?", "Open or close this screen"),
		shortcut("q / ctrl+c", "Quit"),
		shortcut("tab / shift+tab", "Move focus to or from filters"),
		shortcut("/", "Search resources"),
		shortcut("r", "Reload native files"),
		"",
		section("Resource list"),
		shortcut("j / k, ↓ / ↑", "Move selection"),
		shortcut("mouse wheel", "Move selection"),
		shortcut("g / home", "Select first resource"),
		shortcut("G / end", "Select last resource"),
		shortcut("enter", "Open details"),
		shortcut("n", "Create a peer resource"),
		shortcut("e", "Edit selected resource"),
		shortcut("d", "Delete selected resource"),
		shortcut("space", "Enable or disable resource"),
		"",
		section("Filters"),
		shortcut("↑ / ↓, k / j", "Select filter category"),
		shortcut("← / →", "Change filter value"),
		shortcut("space", "Cycle current filter value"),
		shortcut("enter / esc / tab", "Return to resource list"),
		"",
		section("Detail view"),
		shortcut("↑ / ↓", "Scroll one line"),
		shortcut("mouse wheel", "Scroll three lines"),
		shortcut("page up / page down", "Scroll one page"),
		shortcut("g / home, G / end", "Jump to top or bottom"),
		shortcut("← / →", "Switch resource"),
		shortcut("enter / esc", "Return to resource list"),
		"",
		section("Prompts"),
		shortcut("enter", "Submit search or resource name"),
		shortcut("backspace", "Delete previous character"),
		shortcut("esc", "Cancel"),
		shortcut("y / n", "Confirm or reject a change"),
		"",
		m.style(false, "#888888").Render("? / esc / enter close help • q quit"),
	}
	return strings.Join(lines, "\n")
}

func (m *model) renderDetail() string {
	resource := m.selected()
	if resource == nil {
		return "No resource selected."
	}
	lines := []string{
		m.style(true, "#FFFFFF").Render(resource.Name),
		fmt.Sprintf("%s / %s / %s / %s", resource.Harness, resource.Surface, resource.Kind, resource.Scope),
		resource.Path,
		fmt.Sprintf("Fingerprint: %s", truncate(resource.Fingerprint, 16)),
	}
	if resource.ReadOnly {
		lines = append(lines, m.style(true, "#D19A66").Render("Read-only"))
	}
	if len(resource.Warnings) > 0 {
		lines = append(lines, "Warnings: "+strings.Join(resource.Warnings, "; "))
	}
	body, err := m.service.ReadRedacted(*resource)
	if err != nil {
		lines = append(lines, "\n"+err.Error())
	} else {
		viewport, percentage := m.renderDetailViewport(contentLines(body))
		lines = append(lines, "\n"+viewport)
		lines = append(lines, fmt.Sprintf(
			"\n↑/↓ scroll • ←/→ switch • pgup/pgdown page • %d%% • enter/esc back • space enable/disable • e edit • d delete",
			percentage,
		))
	}
	return strings.Join(lines, "\n")
}

func (m *model) detailPageSize() int {
	return max(8, m.height-14)
}

func contentLines(content []byte) []string {
	text := strings.TrimSuffix(string(content), "\n")
	if text == "" {
		return []string{""}
	}
	return strings.Split(text, "\n")
}

func (m *model) scrollDetail(delta int) {
	resource := m.selected()
	if m.mode != modeDetail || resource == nil {
		return
	}
	body, err := m.service.ReadRedacted(*resource)
	if err != nil {
		return
	}
	maxOffset := max(0, len(contentLines(body))-m.detailPageSize())
	m.detailOffset = min(maxOffset, max(0, m.detailOffset+delta))
}

func (m *model) scrollDetailToEnd() {
	resource := m.selected()
	if m.mode != modeDetail || resource == nil {
		return
	}
	body, err := m.service.ReadRedacted(*resource)
	if err != nil {
		return
	}
	m.detailOffset = max(0, len(contentLines(body))-m.detailPageSize())
}

func (m *model) switchDetail(delta int) {
	if m.mode != modeDetail || len(m.filtered) == 0 {
		return
	}
	m.cursor = wrappedIndex(m.cursor, delta, len(m.filtered))
	m.detailOffset = 0
	m.status = "Resource: " + m.filtered[m.cursor].Name
}

func (m *model) renderDetailViewport(lines []string) (string, int) {
	pageSize := m.detailPageSize()
	maxOffset := max(0, len(lines)-pageSize)
	offset := min(maxOffset, max(0, m.detailOffset))
	end := min(len(lines), offset+pageSize)
	visible := lines[offset:end]
	trackHeight := len(visible)
	if trackHeight == 0 {
		visible = []string{""}
		trackHeight = 1
	}
	thumbSize := trackHeight
	thumbStart := 0
	if len(lines) > pageSize {
		thumbSize = max(1, trackHeight*trackHeight/len(lines))
		thumbStart = offset * (trackHeight - thumbSize) / maxOffset
	}
	contentWidth := max(1, m.width-2)
	rendered := make([]string, trackHeight)
	for index, line := range visible {
		line = ansi.Truncate(line, contentWidth, "")
		padding := strings.Repeat(" ", max(0, contentWidth-ansi.StringWidth(line)))
		bar := m.style(false, "#666666").Render("│")
		if index >= thumbStart && index < thumbStart+thumbSize {
			bar = m.style(true, "#7D56F4").Render("┃")
		}
		rendered[index] = line + padding + " " + bar
	}
	percentage := 100
	if maxOffset > 0 {
		percentage = offset * 100 / maxOffset
	}
	return strings.Join(rendered, "\n"), percentage
}

func (m *model) selected() *core.Resource {
	if len(m.filtered) == 0 || m.cursor < 0 || m.cursor >= len(m.filtered) {
		return nil
	}
	return &m.filtered[m.cursor]
}

func (m *model) applyFilter() {
	var harness core.Harness
	if m.harnessIndex >= 0 && m.harnessIndex < len(m.harnessTabs) {
		harness = m.harnessTabs[m.harnessIndex].value
	}
	var kind core.Kind
	if m.kindIndex >= 0 && m.kindIndex < len(m.kindTabs) {
		kind = m.kindTabs[m.kindIndex].value
	}
	var scope core.Scope
	if m.scopeIndex >= 0 && m.scopeIndex < len(m.scopeTabs) {
		scope = m.scopeTabs[m.scopeIndex].value
	}
	query := strings.ToLower(m.input)
	m.filtered = nil
	for _, resource := range m.resources {
		if harness != "" && resource.Harness != harness {
			continue
		}
		if kind != "" && resource.Kind != kind {
			continue
		}
		if scope != "" && resource.Scope != scope {
			continue
		}
		haystack := strings.ToLower(string(resource.Harness) + " " + string(resource.Kind) + " " + resource.Name + " " + resource.Path)
		if query == "" || strings.Contains(haystack, query) {
			m.filtered = append(m.filtered, resource)
		}
	}
	m.cursor = 0
	m.detailOffset = 0
}

func (m *model) cycleHarnessBy(delta int) {
	if len(m.harnessTabs) == 0 {
		return
	}
	m.selectHarness(wrappedIndex(m.harnessIndex, delta, len(m.harnessTabs)))
}

func (m *model) cycleKindBy(delta int) {
	if len(m.kindTabs) == 0 {
		return
	}
	m.selectKind(wrappedIndex(m.kindIndex, delta, len(m.kindTabs)))
}

func (m *model) cycleScopeBy(delta int) {
	if len(m.scopeTabs) == 0 {
		return
	}
	m.selectScope(wrappedIndex(m.scopeIndex, delta, len(m.scopeTabs)))
}

func wrappedIndex(index, delta, length int) int {
	return (index%length + delta%length + length) % length
}

func (m *model) cycleActiveFilter(delta int) {
	switch m.filterRow {
	case 0:
		m.cycleHarnessBy(delta)
	case 1:
		m.cycleKindBy(delta)
	case 2:
		m.cycleScopeBy(delta)
	}
	m.status = "Filter: " + m.activeFilterName()
}

func (m *model) activeFilterName() string {
	switch m.filterRow {
	case 1:
		return "Kind"
	case 2:
		return "Scope"
	default:
		return "Harness"
	}
}

func (m *model) selectHarness(index int) {
	if index < 0 || index >= len(m.harnessTabs) {
		return
	}
	m.harnessIndex = index
	m.mode = modeList
	m.applyFilter()
	m.status = "Harness: " + m.harnessTabs[m.harnessIndex].label
}

func (m *model) selectKind(index int) {
	if index < 0 || index >= len(m.kindTabs) {
		return
	}
	m.kindIndex = index
	m.mode = modeList
	m.applyFilter()
	m.status = "Resource kind: " + m.kindTabs[m.kindIndex].label
}

func (m *model) selectScope(index int) {
	if index < 0 || index >= len(m.scopeTabs) {
		return
	}
	m.scopeIndex = index
	m.mode = modeList
	m.applyFilter()
	m.status = "Scope: " + m.scopeTabs[m.scopeIndex].label
}

func (m *model) reload() {
	selectedID := ""
	selectedKey := ""
	previousCursor := m.cursor
	if selected := m.selected(); selected != nil {
		selectedID = selected.ID
		selectedKey = selected.Key()
	}
	resources, err := m.service.Inventory(m.project, service.Filters{})
	if err != nil {
		m.status = err.Error()
		return
	}
	selectedKind := m.kindTabs[m.kindIndex].value
	selectedScope := m.scopeTabs[m.scopeIndex].value
	m.resources = resources
	m.kindTabs = buildKindTabs(resources)
	m.scopeTabs = buildScopeTabs(resources)
	m.kindIndex = 0
	for index, tab := range m.kindTabs {
		if tab.value == selectedKind {
			m.kindIndex = index
			break
		}
	}
	m.scopeIndex = 0
	for index, tab := range m.scopeTabs {
		if tab.value == selectedScope {
			m.scopeIndex = index
			break
		}
	}
	m.applyFilter()
	m.cursor = min(previousCursor, max(0, len(m.filtered)-1))
	for index, resource := range m.filtered {
		if (selectedID != "" && resource.ID == selectedID) ||
			(selectedID == "" && selectedKey != "" && resource.Key() == selectedKey) {
			m.cursor = index
			break
		}
	}
	m.status = fmt.Sprintf("Reloaded %d resources", len(resources))
}

func (m *model) createSelected() {
	selected := m.selected()
	if selected == nil || strings.TrimSpace(m.input) == "" {
		return
	}
	change, err := m.service.PlanCreate(core.Request{
		Harness: selected.Harness, Kind: selected.Kind, Scope: selected.Scope,
		Name: strings.TrimSpace(m.input), Project: m.project,
	})
	if err == nil {
		_, err = m.service.Apply(context.Background(), change)
	}
	if err != nil {
		m.status = err.Error()
		m.mode = modeList
		return
	}
	m.mode, m.input = modeList, ""
	m.reload()
	m.status = "Resource created"
}

func (m *model) deleteSelected() {
	selected := m.selected()
	if selected == nil {
		return
	}
	change, err := m.service.PlanDelete(*selected)
	if err == nil {
		_, err = m.service.Apply(context.Background(), change)
	}
	if err != nil {
		m.status = err.Error()
		m.mode = modeList
		return
	}
	m.mode = modeList
	m.reload()
	m.status = "Resource deleted; backup created"
}

func (m *model) toggleSelected(enabled bool) {
	selected := m.selected()
	if selected == nil {
		return
	}
	change, err := m.service.PlanToggle(*selected, enabled)
	if err == nil {
		_, err = m.service.Apply(context.Background(), change)
	}
	if err != nil {
		m.status = err.Error()
		return
	}
	m.reload()
	if enabled {
		m.status = "Resource enabled"
	} else {
		m.status = "Resource disabled and stored by RTS"
	}
}

func (m *model) editSelected() tea.Cmd {
	selected := m.selected()
	if selected == nil || !selected.Has(core.CanUpdate) {
		m.status = "Selected resource is read-only"
		return nil
	}
	command, err := editor.Command(service.EditablePath(*selected))
	if err != nil {
		m.status = err.Error()
		return nil
	}
	return tea.ExecProcess(command, func(err error) tea.Msg {
		return editorDone{err: err}
	})
}

func (m *model) renderTabs(prefix string, labels []string, active int, focused bool) string {
	prefixColor := "#888888"
	if focused {
		prefixColor = "#7D56F4"
	}
	parts := []string{m.style(true, prefixColor).Underline(focused).Render(prefix + ":")}
	start, end := visibleTabRange(labels, active, m.width-len(prefix)-3)
	if start > 0 {
		parts = append(parts, m.style(false, "#888888").Render("…"))
	}
	for index := start; index < end; index++ {
		label := labels[index]
		style := m.style(index == active, "#AAAAAA").Padding(0, 1)
		if index == active {
			if !m.noColor {
				style = style.Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#5A3EA6"))
			}
		}
		parts = append(parts, style.Render(label))
	}
	if end < len(labels) {
		parts = append(parts, m.style(false, "#888888").Render("…"))
	}
	return strings.Join(parts, " ")
}

func tabLabels[T any](tabs []tab[T]) []string {
	labels := make([]string, len(tabs))
	for index, tab := range tabs {
		labels[index] = tab.label
	}
	return labels
}

func tabIndexAt(prefix string, labels []string, active, width, x int) int {
	start, end := visibleTabRange(labels, active, width-lipgloss.Width(prefix)-3)
	position := lipgloss.Width(prefix + ":")
	if start < end {
		position++ // strings.Join separator after the prefix.
	}
	if start > 0 {
		position += lipgloss.Width("…") + 1
	}
	for index := start; index < end; index++ {
		tabWidth := lipgloss.Width(labels[index]) + 2
		if x >= position && x < position+tabWidth {
			return index
		}
		position += tabWidth
		if index+1 < end || end < len(labels) {
			position++
		}
	}
	return -1
}

func visibleTabRange(labels []string, active, width int) (int, int) {
	if len(labels) == 0 {
		return 0, 0
	}
	if active < 0 || active >= len(labels) {
		active = 0
	}
	if width <= 0 {
		return 0, len(labels)
	}
	start, end := active, active+1
	used := len(labels[active]) + 2
	for {
		expanded := false
		if start > 0 {
			size := len(labels[start-1]) + 3
			if used+size <= width {
				start--
				used += size
				expanded = true
			}
		}
		if end < len(labels) {
			size := len(labels[end]) + 3
			if used+size <= width {
				end++
				used += size
				expanded = true
			}
		}
		if !expanded {
			break
		}
	}
	return start, end
}

func buildHarnessTabs(svc *service.Service) []harnessTab {
	drivers := make(map[core.Harness]core.Driver)
	for _, driver := range svc.Registry.Drivers() {
		drivers[driver.ID()] = driver
	}
	result := []harnessTab{{label: "All"}}
	preferred := []core.Harness{core.Claude, core.Codex, core.Grok, core.Antigravity, core.OpenCode, core.Copilot}
	for _, harness := range preferred {
		if driver, ok := drivers[harness]; ok {
			result = append(result, harnessTab{label: driver.DisplayName(), value: harness})
			delete(drivers, harness)
		}
	}
	for _, driver := range svc.Registry.Drivers() {
		if _, ok := drivers[driver.ID()]; ok {
			result = append(result, harnessTab{label: driver.DisplayName(), value: driver.ID()})
		}
	}
	return result
}

func buildKindTabs(resources []core.Resource) []kindTab {
	present := make(map[core.Kind]bool)
	for _, resource := range resources {
		present[resource.Kind] = true
	}
	result := []kindTab{{label: "All"}}
	priority := []core.Kind{core.KindMCP, core.KindSkill}
	for _, kind := range append(priority, core.Kinds...) {
		if !present[kind] || hasKindTab(result, kind) {
			continue
		}
		result = append(result, kindTab{label: kindLabel(kind), value: kind})
	}
	return result
}

func buildScopeTabs(resources []core.Resource) []scopeTab {
	present := make(map[core.Scope]bool)
	for _, resource := range resources {
		present[resource.Scope] = true
	}
	result := []scopeTab{{label: "All"}}
	priority := []core.Scope{
		core.ScopeUser, core.ScopeProject, core.ScopeLocal,
		core.ScopePlugin, core.ScopeManaged,
	}
	for _, scope := range priority {
		if present[scope] {
			result = append(result, scopeTab{label: scopeLabel(scope), value: scope})
		}
	}
	return result
}

func scopeLabel(scope core.Scope) string {
	value := string(scope)
	if value == "" {
		return "All"
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

func hasKindTab(tabs []kindTab, kind core.Kind) bool {
	for _, tab := range tabs {
		if tab.value == kind {
			return true
		}
	}
	return false
}

func kindLabel(kind core.Kind) string {
	switch kind {
	case core.KindMCP:
		return "MCP"
	case core.KindSkill:
		return "Skills"
	case core.KindInstructions:
		return "Instructions"
	case core.KindSettings:
		return "Settings"
	}
	value := strings.ReplaceAll(string(kind), "-", " ")
	if value == "" {
		return "All"
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

func (m *model) style(bold bool, color string) lipgloss.Style {
	style := lipgloss.NewStyle().Bold(bold)
	if !m.noColor {
		style = style.Foreground(lipgloss.Color(color))
	}
	return style
}

func truncate(value string, length int) string {
	runes := []rune(value)
	if len(runes) <= length {
		return value
	}
	if length < 2 {
		return string(runes[:length])
	}
	return string(runes[:length-1]) + "…"
}
