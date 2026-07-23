// Package tui renders a k9s-style full-screen host browser: a header with
// context and keybindings, a collapsible group tree, and a live filter.
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/alexandrfiner/sshhh/internal/config"
)

var (
	accent = lipgloss.Color("62")  // indigo
	pink   = lipgloss.Color("205") // key caps / accents
	subtle = lipgloss.Color("244") // muted text
	bright = lipgloss.Color("230") // on-accent text

	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(bright).Background(accent).Padding(0, 1)
	labelStyle = lipgloss.NewStyle().Foreground(pink)
	mutedStyle = lipgloss.NewStyle().Foreground(subtle)
	capStyle   = lipgloss.NewStyle().Foreground(pink).Bold(true)
	groupStyle = lipgloss.NewStyle().Bold(true).Foreground(pink)
	selStyle   = lipgloss.NewStyle().Foreground(bright).Background(accent).Bold(true)
	onStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true) // green
	offStyle   = lipgloss.NewStyle().Foreground(subtle)
)

// Run opens the browser over all hosts, optionally seeding the filter with
// initialFilter, and returns the chosen host (nil if the user quit) along with
// the final root / dry-run toggle states.
func Run(all []config.Host, configPath, initialFilter string, root, dryRun bool) (*config.Host, bool, bool, error) {
	m := newModel(all, configPath, initialFilter, root, dryRun)
	res, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		return nil, root, dryRun, err
	}
	fm := res.(*model)
	return fm.chosen, fm.root, fm.dryRun, nil
}

type group struct {
	name  string
	hosts []config.Host
}

type rowKind int

const (
	rowGroup rowKind = iota
	rowHost
)

// row is one visible line: either a group header or a host under a group.
type row struct {
	kind rowKind
	g    int // group index
	h    int // host index within the group (host rows only)
}

type model struct {
	groups     []group
	expanded   []bool
	visible    []row
	cursor     int // index into visible
	top        int // scroll offset (index into visible)
	filter     textinput.Model
	inputMode  bool
	query      string
	root       bool
	dryRun     bool
	configPath string
	width      int
	bodyHeight int
	colAlias   int
	colName    int
	colHost    int
	chosen     *config.Host
}

func newModel(all []config.Host, configPath, initialFilter string, root, dryRun bool) *model {
	ti := textinput.New()
	ti.Prompt = "/"
	ti.Placeholder = "filter by name / group / tag"
	ti.SetValue(initialFilter)

	m := &model{
		groups:     buildGroups(all),
		configPath: configPath,
		root:       root,
		dryRun:     dryRun,
		filter:     ti,
		width:      100,
		bodyHeight: 20,
	}
	m.expanded = make([]bool, len(m.groups))
	for i := range m.expanded {
		m.expanded[i] = true // start expanded; `a` collapses everything at once
	}
	m.colAlias, m.colName, m.colHost = columnWidths(all)
	m.setQuery(initialFilter)
	return m
}

func (m *model) Init() tea.Cmd { return nil }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		headerH := lipgloss.Height(m.header())
		m.bodyHeight = max(3, msg.Height-headerH-1)
		m.clampScroll()
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.inputMode {
			return m.updateInput(msg)
		}
		return m.updateBrowse(msg)
	}
	return m, nil
}

func (m *model) updateBrowse(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "esc":
		if m.query != "" {
			m.filter.SetValue("")
			m.setQuery("")
			return m, nil
		}
		return m, tea.Quit
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "pgup":
		m.move(-m.bodyHeight)
	case "pgdown":
		m.move(m.bodyHeight)
	case "home", "g":
		m.cursor = 0
		m.clampScroll()
	case "end", "G":
		m.cursor = len(m.visible) - 1
		m.clampScroll()
	case "right", "l":
		m.setExpanded(true)
	case "left", "h":
		m.collapseCurrent()
	case "enter":
		return m.activate()
	case " ":
		m.toggleCurrent()
	case "a":
		m.toggleAll()
	case "/":
		m.inputMode = true
		m.filter.Focus()
		m.filter.CursorEnd()
	case "r":
		m.root = !m.root
	case "d":
		m.dryRun = !m.dryRun
	}
	return m, nil
}

func (m *model) updateInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.inputMode = false
		m.filter.Blur()
		return m, nil
	case "esc":
		m.inputMode = false
		m.filter.Blur()
		m.filter.SetValue("")
		m.setQuery("")
		return m, nil
	case "up":
		m.move(-1)
		return m, nil
	case "down":
		m.move(1)
		return m, nil
	}
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	m.setQuery(m.filter.Value())
	return m, cmd
}

// activate handles Enter: toggle a group, or connect to a host.
func (m *model) activate() (tea.Model, tea.Cmd) {
	if len(m.visible) == 0 {
		return m, nil
	}
	r := m.visible[m.cursor]
	if r.kind == rowGroup {
		m.toggleCurrent()
		return m, nil
	}
	h := m.groups[r.g].hosts[r.h]
	m.chosen = &h
	return m, tea.Quit
}

func (m *model) move(delta int) {
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.visible) {
		m.cursor = len(m.visible) - 1
	}
	m.clampScroll()
}

func (m *model) clampScroll() {
	if m.cursor < m.top {
		m.top = m.cursor
	}
	if m.cursor >= m.top+m.bodyHeight {
		m.top = m.cursor - m.bodyHeight + 1
	}
	if m.top < 0 {
		m.top = 0
	}
}

func (m *model) currentGroup() int {
	if len(m.visible) == 0 {
		return -1
	}
	return m.visible[m.cursor].g
}

func (m *model) setExpanded(v bool) {
	if gi := m.currentGroup(); gi >= 0 {
		m.expanded[gi] = v
		m.rebuild()
	}
}

func (m *model) toggleCurrent() {
	if gi := m.currentGroup(); gi >= 0 {
		m.expanded[gi] = !m.expanded[gi]
		m.rebuild()
	}
}

// collapseCurrent collapses the current group and moves the cursor to its header.
func (m *model) collapseCurrent() {
	gi := m.currentGroup()
	if gi < 0 {
		return
	}
	m.expanded[gi] = false
	m.rebuild()
	for i, r := range m.visible {
		if r.kind == rowGroup && r.g == gi {
			m.cursor = i
			break
		}
	}
	m.clampScroll()
}

func (m *model) toggleAll() {
	anyCollapsed := false
	for _, e := range m.expanded {
		if !e {
			anyCollapsed = true
			break
		}
	}
	for i := range m.expanded {
		m.expanded[i] = anyCollapsed
	}
	m.rebuild()
}

// setQuery applies a filter, force-expanding matching groups and parking the
// cursor on the first matching host.
func (m *model) setQuery(q string) {
	m.query = q
	m.rebuild()
	if q == "" {
		return
	}
	for i, r := range m.visible {
		if r.kind == rowHost {
			m.cursor = i
			break
		}
	}
	m.clampScroll()
}

func (m *model) rebuild() {
	m.visible = m.visible[:0]
	for gi := range m.groups {
		idx := m.matchingIdx(gi)
		if m.query != "" && len(idx) == 0 {
			continue
		}
		m.visible = append(m.visible, row{kind: rowGroup, g: gi, h: -1})
		if m.expanded[gi] || m.query != "" {
			for _, hi := range idx {
				m.visible = append(m.visible, row{kind: rowHost, g: gi, h: hi})
			}
		}
	}
	if m.cursor >= len(m.visible) {
		m.cursor = len(m.visible) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.clampScroll()
}

func (m *model) matchingIdx(gi int) []int {
	out := make([]int, 0, len(m.groups[gi].hosts))
	for i, h := range m.groups[gi].hosts {
		if hostMatches(h, m.query) {
			out = append(out, i)
		}
	}
	return out
}

func (m *model) View() string {
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteByte('\n')

	end := min(m.top+m.bodyHeight, len(m.visible))
	for i := m.top; i < end; i++ {
		b.WriteString(m.renderRow(i))
		b.WriteByte('\n')
	}
	for i := end - m.top; i < m.bodyHeight; i++ {
		b.WriteByte('\n') // pad so the footer stays put
	}

	if m.inputMode {
		b.WriteString(m.filter.View())
	} else {
		b.WriteString(m.status())
	}
	return b.String()
}

func (m *model) renderRow(i int) string {
	r := m.visible[i]
	selected := i == m.cursor

	if r.kind == rowGroup {
		g := m.groups[r.g]
		indicator := "▸"
		if m.expanded[r.g] || m.query != "" {
			indicator = "▾"
		}
		count := len(g.hosts)
		if m.query != "" {
			count = len(m.matchingIdx(r.g))
		}
		text := fmt.Sprintf("%s %s (%d)", indicator, g.name, count)
		if selected {
			return selStyle.Width(m.width).Render(text)
		}
		return groupStyle.Render(text)
	}

	h := m.groups[r.g].hosts[r.h]
	addr := h.Addr
	if h.Port != 0 && h.Port != 22 {
		addr = fmt.Sprintf("%s:%d", addr, h.Port)
	}
	tags := strings.Join(h.Tags, ",")
	alias := pad(h.Alias, m.colAlias)
	name := pad(h.Name, m.colName)
	addrCol := pad(addr, m.colHost)

	if selected {
		text := fmt.Sprintf("  %s  %s  %s  %s", alias, name, addrCol, tags)
		return selStyle.Width(m.width).Render(text)
	}
	return "  " + alias + "  " + name + "  " + mutedStyle.Render(addrCol) + "  " + mutedStyle.Render(tags)
}

func (m *model) header() string {
	filterVal := m.filter.Value()
	if filterVal == "" {
		filterVal = mutedStyle.Render("—")
	}
	info := lipgloss.JoinVertical(lipgloss.Left,
		labelStyle.Render("Config ")+m.configPath,
		labelStyle.Render("Groups ")+fmt.Sprintf("%d", len(m.groups))+labelStyle.Render("   Hosts ")+fmt.Sprintf("%d", m.hostCount()),
		labelStyle.Render("Filter ")+filterVal,
		labelStyle.Render("Root   ")+onOff(m.root)+"   "+labelStyle.Render("Dry-run ")+onOff(m.dryRun),
	)
	hints := lipgloss.JoinVertical(lipgloss.Left,
		capStyle.Render("↑↓/jk")+" navigate    "+capStyle.Render("enter")+" connect / toggle",
		capStyle.Render("→←/hl")+" expand/fold  "+capStyle.Render("a")+"     toggle all",
		capStyle.Render("/")+"     filter      "+capStyle.Render("r")+"     toggle root",
		capStyle.Render("esc")+"   clear/back   "+capStyle.Render("d")+"     toggle dry-run",
	)
	body := lipgloss.JoinHorizontal(lipgloss.Top, info, "    ", hints)
	return lipgloss.JoinVertical(lipgloss.Left, titleStyle.Render("sshhh"), "", body, "")
}

func (m *model) status() string {
	return mutedStyle.Render(fmt.Sprintf(" %d hosts  ·  / filter · enter connect · a fold all · q quit", m.hostCount()))
}

func (m *model) hostCount() int {
	n := 0
	for _, g := range m.groups {
		n += len(g.hosts)
	}
	return n
}

func buildGroups(hosts []config.Host) []group {
	var gs []group
	idx := map[string]int{}
	for _, h := range hosts {
		i, ok := idx[h.Group]
		if !ok {
			i = len(gs)
			idx[h.Group] = i
			gs = append(gs, group{name: h.Group})
		}
		gs[i].hosts = append(gs[i].hosts, h)
	}
	return gs
}

func columnWidths(hosts []config.Host) (alias, name, host int) {
	alias, name, host = len("ALIAS"), len("NAME"), len("HOST")
	for _, h := range hosts {
		addr := h.Addr
		if h.Port != 0 && h.Port != 22 {
			addr = fmt.Sprintf("%s:%d", addr, h.Port)
		}
		alias = max(alias, len(h.Alias))
		name = max(name, len(h.Name))
		host = max(host, len(addr))
	}
	return min(alias, 10), min(name, 24), min(host, 22)
}

func hostMatches(h config.Host, q string) bool {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return true
	}
	hay := strings.ToLower(strings.Join([]string{
		h.Group, h.Alias, h.Name, strings.Join(h.Tags, " "),
	}, " "))
	return strings.Contains(hay, q)
}

// filterHosts returns the subset of hosts matching q (used by tests/callers).
func filterHosts(hosts []config.Host, q string) []config.Host {
	if strings.TrimSpace(q) == "" {
		return hosts
	}
	var out []config.Host
	for _, h := range hosts {
		if hostMatches(h, q) {
			out = append(out, h)
		}
	}
	return out
}

func pad(s string, w int) string {
	if len(s) > w {
		if w > 1 {
			return s[:w-1] + "…"
		}
		return s[:w]
	}
	return s + strings.Repeat(" ", w-len(s))
}

func onOff(b bool) string {
	if b {
		return onStyle.Render("on")
	}
	return offStyle.Render("off")
}
