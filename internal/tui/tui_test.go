package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/alexandrfiner/sshhh/internal/config"
)

func sampleHosts() []config.Host {
	return []config.Host{
		{Alias: "db1", Group: "aws-prod", Name: "Postgres primary", Addr: "10.0.1.20", Tags: []string{"db"}},
		{Alias: "api1", Group: "gcp-prod", Name: "API server 1", Addr: "10.128.0.10"},
		{Alias: "web1", Group: "digitalocean-dev", Name: "Dev web", Addr: "203.0.113.10"},
	}
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func send(m *model, msg tea.Msg) *model {
	next, _ := m.Update(msg)
	return next.(*model)
}

func visibleHosts(m *model) []string {
	var out []string
	for _, r := range m.visible {
		if r.kind == rowHost {
			out = append(out, m.groups[r.g].hosts[r.h].Alias)
		}
	}
	return out
}

func countGroups(m *model) int {
	n := 0
	for _, r := range m.visible {
		if r.kind == rowGroup {
			n++
		}
	}
	return n
}

func TestViewRendersWithoutTTY(t *testing.T) {
	m := newModel(sampleHosts(), "/tmp/config.yaml", "", false, false)
	m = send(m, tea.WindowSizeMsg{Width: 100, Height: 30})
	out := m.View()
	for _, want := range []string{"sshhh", "aws-prod", "db1", "/tmp/config.yaml"} {
		if !strings.Contains(out, want) {
			t.Errorf("view missing %q\n---\n%s", want, out)
		}
	}
}

func TestExpandCollapseAll(t *testing.T) {
	m := newModel(sampleHosts(), "/tmp/config.yaml", "", false, false)
	if got := len(visibleHosts(m)); got != 3 {
		t.Fatalf("expected 3 hosts visible when expanded, got %d", got)
	}
	m = send(m, key("a")) // collapse all
	if got := len(visibleHosts(m)); got != 0 {
		t.Fatalf("expected 0 hosts visible when collapsed, got %d", got)
	}
	if got := countGroups(m); got != 3 {
		t.Fatalf("expected 3 group headers, got %d", got)
	}
	m = send(m, key("a")) // expand all again
	if got := len(visibleHosts(m)); got != 3 {
		t.Fatalf("expected 3 hosts visible after re-expand, got %d", got)
	}
}

func TestEnterTogglesGroup(t *testing.T) {
	m := newModel(sampleHosts(), "/tmp/config.yaml", "", false, false)
	// cursor starts on the first group header
	m = send(m, key("enter")) // collapse that group
	if got := len(visibleHosts(m)); got != 2 {
		t.Fatalf("expected 2 hosts after collapsing first group, got %d", got)
	}
}

func TestFilterNarrowsAndSelects(t *testing.T) {
	m := newModel(sampleHosts(), "/tmp/config.yaml", "", false, false)
	m = send(m, tea.WindowSizeMsg{Width: 100, Height: 30})

	m = send(m, key("/")) // enter filter mode
	if !m.inputMode {
		t.Fatal("expected input mode after /")
	}
	for _, r := range "dev" { // matches only the digitalocean-dev group
		m = send(m, key(string(r)))
	}
	if got := visibleHosts(m); len(got) != 1 || got[0] != "web1" {
		t.Fatalf("filter did not narrow to web1: %v", got)
	}

	m = send(m, key("enter")) // confirm filter, leave input mode
	if m.inputMode {
		t.Fatal("expected to leave input mode after enter")
	}
	m = send(m, key("enter")) // connect to the parked host
	if m.chosen == nil || m.chosen.Alias != "web1" {
		t.Fatalf("expected chosen web1, got %+v", m.chosen)
	}
}

func TestToggles(t *testing.T) {
	m := newModel(sampleHosts(), "/tmp/config.yaml", "", false, false)
	m = send(m, key("r"))
	m = send(m, key("d"))
	if !m.root || !m.dryRun {
		t.Fatalf("expected root and dryRun on, got root=%v dryRun=%v", m.root, m.dryRun)
	}
}

func TestFilterMatchesGroupAndTag(t *testing.T) {
	if got := filterHosts(sampleHosts(), "prod"); len(got) != 2 {
		t.Fatalf("expected 2 prod hosts, got %d", len(got))
	}
	if got := filterHosts(sampleHosts(), "db"); len(got) != 1 {
		t.Fatalf("expected 1 db-tagged host, got %d", len(got))
	}
}
