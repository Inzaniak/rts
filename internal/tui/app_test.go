package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Inzaniak/rts/internal/adapters"
	"github.com/Inzaniak/rts/internal/core"
	"github.com/Inzaniak/rts/internal/service"
	"github.com/Inzaniak/rts/internal/store"
)

func TestTabbedFiltersCompose(t *testing.T) {
	resources := []core.Resource{
		{Harness: core.Claude, Kind: core.KindSkill, Scope: core.ScopeUser, Name: "review"},
		{Harness: core.Claude, Kind: core.KindMCP, Scope: core.ScopeProject, Name: "docs"},
		{Harness: core.Codex, Kind: core.KindMCP, Scope: core.ScopeProject, Name: "search"},
	}
	svc := &service.Service{Registry: core.NewRegistry(adapters.All()...)}
	m := newModel(svc, "", resources, true)

	if m.harnessTabs[1].value != core.Claude {
		t.Fatalf("first harness tab = %q, want Claude", m.harnessTabs[1].value)
	}
	for m.harnessTabs[m.harnessIndex].value != core.Claude {
		m.cycleHarnessBy(1)
	}
	for m.kindTabs[m.kindIndex].value != core.KindMCP {
		m.cycleKindBy(1)
	}
	for m.scopeTabs[m.scopeIndex].value != core.ScopeProject {
		m.cycleScopeBy(1)
	}
	if len(m.filtered) != 1 || m.filtered[0].Name != "docs" {
		t.Fatalf("combined tabs returned %#v", m.filtered)
	}
}

func TestKeyboardFilterNavigation(t *testing.T) {
	resources := []core.Resource{
		{Harness: core.Claude, Kind: core.KindMCP, Scope: core.ScopeProject, Name: "docs"},
		{Harness: core.Codex, Kind: core.KindSkill, Scope: core.ScopeUser, Name: "review"},
	}
	svc := &service.Service{Registry: core.NewRegistry(adapters.All()...)}
	m := newModel(svc, "", resources, true)

	m.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	if !m.filterFocus || m.filterRow != 0 {
		t.Fatalf("tab did not focus filters: focus=%t row=%d", m.filterFocus, m.filterRow)
	}
	m.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyRight}))
	if m.harnessTabs[m.harnessIndex].value != core.Claude {
		t.Fatalf("right did not change harness: %d", m.harnessIndex)
	}
	m.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	m.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyRight}))
	if m.filterRow != 1 || m.kindTabs[m.kindIndex].value != core.KindMCP {
		t.Fatalf("kind navigation failed: row=%d kind=%d", m.filterRow, m.kindIndex)
	}
	m.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	m.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyRight}))
	if m.filterRow != 2 || m.scopeTabs[m.scopeIndex].value != core.ScopeUser {
		t.Fatalf("scope navigation failed: row=%d scope=%d", m.filterRow, m.scopeIndex)
	}
	m.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	if m.filterFocus {
		t.Fatal("second tab did not return focus to the resource list")
	}
}

func TestMouseClicksSelectTabs(t *testing.T) {
	resources := []core.Resource{
		{Harness: core.Claude, Kind: core.KindMCP, Scope: core.ScopeProject, Name: "docs"},
		{Harness: core.Claude, Kind: core.KindSkill, Scope: core.ScopeUser, Name: "review"},
		{Harness: core.Codex, Kind: core.KindSkill, Scope: core.ScopePlugin, Name: "search"},
	}
	svc := &service.Service{Registry: core.NewRegistry(adapters.All()...)}
	m := newModel(svc, "", resources, true)
	m.width = 120

	m.handleMouse(tea.MouseClickMsg(tea.Mouse{X: 16, Y: 2, Button: tea.MouseLeft}))
	if m.harnessTabs[m.harnessIndex].value != core.Claude {
		t.Fatalf("harness click selected index %d", m.harnessIndex)
	}
	m.handleMouse(tea.MouseClickMsg(tea.Mouse{X: 19, Y: 3, Button: tea.MouseLeft}))
	if m.kindTabs[m.kindIndex].value != core.KindSkill {
		t.Fatalf("kind click selected index %d", m.kindIndex)
	}
	m.handleMouse(tea.MouseClickMsg(tea.Mouse{X: 14, Y: 4, Button: tea.MouseLeft}))
	if m.scopeTabs[m.scopeIndex].value != core.ScopeUser || !m.filterFocus || m.filterRow != 2 {
		t.Fatalf("scope click selected index %d with focus row %d", m.scopeIndex, m.filterRow)
	}
	if len(m.filtered) != 1 || m.filtered[0].Name != "review" {
		t.Fatalf("clicked tabs returned %#v", m.filtered)
	}
	if m.View().MouseMode != tea.MouseModeCellMotion {
		t.Fatal("mouse reporting is not enabled")
	}
}

func TestMouseWheelMovesListSelection(t *testing.T) {
	resources := make([]core.Resource, 8)
	for index := range resources {
		resources[index] = core.Resource{Name: fmt.Sprintf("resource-%d", index)}
	}
	svc := &service.Service{Registry: core.NewRegistry(adapters.All()...)}
	m := newModel(svc, "", resources, true)
	m.filterFocus = true

	m.Update(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelDown}))
	if m.cursor != 3 || m.filterFocus {
		t.Fatalf("wheel down: cursor=%d focus=%t, want cursor=3 focus=false", m.cursor, m.filterFocus)
	}
	m.Update(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelUp}))
	if m.cursor != 0 {
		t.Fatalf("wheel up cursor = %d, want 0", m.cursor)
	}
	m.Update(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelUp}))
	if m.cursor != 0 {
		t.Fatalf("wheel up did not clamp at top: %d", m.cursor)
	}
	m.cursor = len(resources) - 2
	m.Update(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelDown}))
	if m.cursor != len(resources)-1 {
		t.Fatalf("wheel down did not clamp at bottom: %d", m.cursor)
	}
}

func TestQuestionMarkOpensDedicatedShortcutScreen(t *testing.T) {
	svc := &service.Service{Registry: core.NewRegistry(adapters.All()...)}
	m := newModel(svc, "", []core.Resource{{Name: "docs"}}, true)

	m.handleKey(tea.KeyPressMsg(tea.Key{Code: '?', Text: "?"}))
	if m.mode != modeHelp {
		t.Fatalf("question mark mode = %d, want help", m.mode)
	}
	rendered := m.render()
	for _, want := range []string{"Keyboard Shortcuts", "Resource list", "Filters", "Detail view", "Prompts"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("help screen is missing %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "HARNESS") {
		t.Fatalf("help is not a dedicated screen:\n%s", rendered)
	}

	m.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if m.mode != modeList {
		t.Fatalf("escape returned to mode %d, want list", m.mode)
	}
}

func TestShortcutScreenReturnsToDetail(t *testing.T) {
	svc := &service.Service{Registry: core.NewRegistry(adapters.All()...)}
	m := newModel(svc, "", []core.Resource{{Name: "docs"}}, true)
	m.mode = modeDetail

	m.handleKey(tea.KeyPressMsg(tea.Key{Code: '?', Text: "?"}))
	m.handleKey(tea.KeyPressMsg(tea.Key{Code: '?', Text: "?"}))
	if m.mode != modeDetail {
		t.Fatalf("question mark returned to mode %d, want detail", m.mode)
	}
}

func TestScopeTabsUseStablePriority(t *testing.T) {
	tabs := buildScopeTabs([]core.Resource{
		{Scope: core.ScopeManaged},
		{Scope: core.ScopePlugin},
		{Scope: core.ScopeProject},
		{Scope: core.ScopeUser},
	})
	want := []core.Scope{"", core.ScopeUser, core.ScopeProject, core.ScopePlugin, core.ScopeManaged}
	if len(tabs) != len(want) {
		t.Fatalf("tabs = %#v", tabs)
	}
	for index := range want {
		if tabs[index].value != want[index] {
			t.Fatalf("scope tab %d = %q, want %q", index, tabs[index].value, want[index])
		}
	}
}

func TestResourceTableShowsColoredStateColumn(t *testing.T) {
	off := false
	on := true
	resources := []core.Resource{
		{Harness: core.Codex, Kind: core.KindMCP, Scope: core.ScopeUser, Name: "disabled", Path: "/tmp/disabled", Enabled: &off},
		{Harness: core.Codex, Kind: core.KindMCP, Scope: core.ScopeUser, Name: "enabled", Path: "/tmp/enabled", Enabled: &on},
	}
	svc := &service.Service{Registry: core.NewRegistry(adapters.All()...)}
	m := newModel(svc, "", resources, false)
	m.width = 120
	m.height = 30
	m.cursor = 1

	rendered := m.render()
	if !strings.Contains(rendered, "STATE") || !strings.Contains(rendered, "OFF") || !strings.Contains(rendered, "ON") {
		t.Fatalf("state column is missing:\n%s", rendered)
	}
	if strings.Contains(rendered, "[off]") {
		t.Fatalf("legacy off suffix is still rendered:\n%s", rendered)
	}
	disabledRow := fmt.Sprintf("  %-10s %-14s %-9s %-5s %-24s %s",
		core.Codex, core.KindMCP, core.ScopeUser, "OFF", "disabled", "/tmp/disabled")
	if !strings.Contains(rendered, m.style(true, "#FF5F56").Render(disabledRow)) {
		t.Fatalf("disabled row is not red:\n%s", rendered)
	}
}

func TestTabHitTestingIgnoresOverflowEllipsis(t *testing.T) {
	labels := []string{"All", "Settings", "Instructions", "MCP", "Skills", "Commands"}
	if index := tabIndexAt("Kind", labels, 4, 22, 6); index != -1 {
		t.Fatalf("overflow ellipsis selected tab %d", index)
	}
}

func TestSkillDetailPageScrollingAndPercentage(t *testing.T) {
	skill := filepath.Join(t.TempDir(), "long-skill")
	if err := os.MkdirAll(skill, 0o755); err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	for index := range 25 {
		fmt.Fprintf(&body, "line %02d\n", index)
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte(body.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	resources := []core.Resource{{
		Harness: core.Codex, Kind: core.KindSkill, Name: "long-skill", Path: skill,
		Format: "skill-directory", Metadata: map[string]any{"mainFile": "SKILL.md"},
	}}
	svc := &service.Service{Registry: core.NewRegistry(adapters.All()...)}
	m := newModel(svc, "", resources, true)
	m.mode = modeDetail
	m.width = 60
	m.height = 20

	m.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	if m.detailOffset != 1 {
		t.Fatalf("down offset = %d, want 1", m.detailOffset)
	}
	m.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyUp}))
	if m.detailOffset != 0 {
		t.Fatalf("up offset = %d, want 0", m.detailOffset)
	}
	m.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyPgDown}))
	if m.detailOffset != 8 {
		t.Fatalf("page down offset = %d, want 8", m.detailOffset)
	}
	for range 10 {
		m.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyPgDown}))
	}
	if m.detailOffset != 17 {
		t.Fatalf("bottom offset = %d, want 17", m.detailOffset)
	}
	rendered := m.renderDetail()
	if !strings.Contains(rendered, "100%") || !strings.Contains(rendered, "┃") {
		t.Fatalf("detail lacks scrollbar or percentage:\n%s", rendered)
	}
	m.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyPgUp}))
	if m.detailOffset != 9 {
		t.Fatalf("page up offset = %d, want 9", m.detailOffset)
	}
	m.applyFilter()
	if m.detailOffset != 0 {
		t.Fatalf("filter did not reset detail offset: %d", m.detailOffset)
	}
}

func TestMouseWheelScrollsDetail(t *testing.T) {
	skill := filepath.Join(t.TempDir(), "long-skill")
	if err := os.MkdirAll(skill, 0o755); err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	for index := range 25 {
		fmt.Fprintf(&body, "line %02d\n", index)
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte(body.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	resources := []core.Resource{{
		Harness: core.Codex, Kind: core.KindSkill, Name: "long-skill", Path: skill,
		Format: "skill-directory", Metadata: map[string]any{"mainFile": "SKILL.md"},
	}}
	svc := &service.Service{Registry: core.NewRegistry(adapters.All()...)}
	m := newModel(svc, "", resources, true)
	m.mode = modeDetail
	m.width = 60
	m.height = 20

	m.Update(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelDown}))
	if m.detailOffset != 3 {
		t.Fatalf("wheel down offset = %d, want 3", m.detailOffset)
	}
	m.Update(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelUp}))
	if m.detailOffset != 0 {
		t.Fatalf("wheel up offset = %d, want 0", m.detailOffset)
	}
	m.Update(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelLeft}))
	if m.detailOffset != 0 {
		t.Fatalf("horizontal wheel changed offset to %d", m.detailOffset)
	}
}

func TestDetailLeftRightSwitchResources(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first.md")
	second := filepath.Join(root, "second.md")
	if err := os.WriteFile(first, []byte("first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("second\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resources := []core.Resource{
		{Harness: core.Codex, Kind: core.KindInstructions, Name: "first", Path: first},
		{Harness: core.Codex, Kind: core.KindInstructions, Name: "second", Path: second},
	}
	svc := &service.Service{Registry: core.NewRegistry(adapters.All()...)}
	m := newModel(svc, "", resources, true)
	m.mode = modeDetail
	m.detailOffset = 4

	m.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyRight}))
	if m.cursor != 1 || m.detailOffset != 0 || m.selected().Name != "second" {
		t.Fatalf("right switch failed: cursor=%d offset=%d selected=%#v", m.cursor, m.detailOffset, m.selected())
	}
	m.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeyLeft}))
	if m.cursor != 0 || m.selected().Name != "first" {
		t.Fatalf("left switch failed: cursor=%d selected=%#v", m.cursor, m.selected())
	}
}

func TestSpaceDisablesAndEnablesSelectedMCP(t *testing.T) {
	root := t.TempDir()
	codexHome := filepath.Join(root, "codex")
	configRoot := filepath.Join(root, "rts")
	t.Setenv("HOME", root)
	t.Setenv("CODEX_HOME", codexHome)
	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(codexHome, "config.toml")
	if err := os.WriteFile(configPath, []byte(`
[mcp_servers.docs]
url = "https://example.com/mcp"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	state, err := store.Open(filepath.Join(configRoot, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	svc := service.New(adapters.All(), state, configRoot)
	resources, err := svc.Inventory(t.Context(), "", service.Filters{
		Harness: core.Codex, Kind: core.KindMCP,
	})
	if err != nil {
		t.Fatal(err)
	}
	m := newModel(svc, "", resources, true)

	m.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeySpace}))
	raw, _ := os.ReadFile(configPath)
	if strings.Contains(string(raw), "mcp_servers.docs") {
		t.Fatalf("space did not disable MCP entry:\n%s", raw)
	}
	disabledIndex := -1
	for index, resource := range m.filtered {
		if resource.Harness == core.Codex && resource.Kind == core.KindMCP && resource.Name == "docs" &&
			resource.Enabled != nil && !*resource.Enabled {
			disabledIndex = index
			break
		}
	}
	if disabledIndex < 0 {
		t.Fatalf("disabled MCP entry is missing from TUI inventory: %#v", m.filtered)
	}
	m.cursor = disabledIndex
	m.filterFocus = true
	m.handleKey(tea.KeyPressMsg(tea.Key{Code: tea.KeySpace}))
	raw, _ = os.ReadFile(configPath)
	if !strings.Contains(string(raw), "https://example.com/mcp") {
		t.Fatalf("space did not re-enable MCP entry:\n%s", raw)
	}
}

func TestKindTabsPrioritizeMCPAndSkills(t *testing.T) {
	tabs := buildKindTabs([]core.Resource{
		{Kind: core.KindSettings},
		{Kind: core.KindSkill},
		{Kind: core.KindMCP},
	})
	want := []core.Kind{"", core.KindMCP, core.KindSkill, core.KindSettings}
	if len(tabs) != len(want) {
		t.Fatalf("tabs = %#v", tabs)
	}
	for index := range want {
		if tabs[index].value != want[index] {
			t.Fatalf("tab %d = %q, want %q", index, tabs[index].value, want[index])
		}
	}
}

func TestVisibleTabRangeKeepsActiveTabVisible(t *testing.T) {
	labels := []string{"All", "Settings", "Instructions", "MCP", "Skills", "Commands"}
	start, end := visibleTabRange(labels, 4, 22)
	if start > 4 || end <= 4 {
		t.Fatalf("active tab is outside range [%d:%d]", start, end)
	}
	width := 0
	for _, label := range labels[start:end] {
		width += len(label) + 3
	}
	if width > 25 {
		t.Fatalf("tab range is unexpectedly wide: %d", width)
	}
}
