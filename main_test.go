package main

import "testing"

func TestParseLabels(t *testing.T) {
	got := parseLabels(" Work, urgent,work,  home ")
	want := []string{"home", "urgent", "work"}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestParsePriority(t *testing.T) {
	if p := parsePriority("1"); p != 1 {
		t.Fatalf("expected 1, got %d", p)
	}
	if p := parsePriority("9"); p != 4 {
		t.Fatalf("expected fallback 4, got %d", p)
	}
}

func TestNormalizeDueDate(t *testing.T) {
	if d := normalizeDueDate("2026-12-01"); d != "2026-12-01" {
		t.Fatalf("unexpected date: %s", d)
	}
	if d := normalizeDueDate("01-12-2026"); d != "" {
		t.Fatalf("invalid format must be empty, got: %s", d)
	}
}

func TestFilterTodosByQueryAndDone(t *testing.T) {
	todos := []Todo{
		{ID: 1, Text: "Pay rent", Note: "bank transfer", Project: "home", Priority: 1},
		{ID: 2, Text: "Deploy release", Labels: []string{"work", "urgent"}, Project: "work", Done: true},
	}
	items := filterTodos(todos, "project", "work", true, "urgent")
	if len(items) != 1 || items[0].ID != 2 {
		t.Fatalf("expected done task #2 in query results, got %+v", items)
	}

	items = filterTodos(todos, "inbox", "", false, "rent")
	if len(items) != 0 {
		t.Fatalf("rent task is in home project, should not be in inbox: %+v", items)
	}
}

func TestCurrentBackURL(t *testing.T) {
	got := currentBackURL("project", "work space", true, "fix bug")
	if got != "/?view=project&project=work+space&show_done=1&q=fix+bug" {
		t.Fatalf("unexpected back url: %s", got)
	}
}

func TestAdvanceDueDate(t *testing.T) {
	next, ok := advanceDueDate("2026-04-15", "weekly")
	if !ok || next != "2026-04-22" {
		t.Fatalf("unexpected recurrence shift: ok=%v next=%s", ok, next)
	}
}

func TestNormalizeRecurrence(t *testing.T) {
	if normalizeRecurrence(" DAILY ") != "daily" {
		t.Fatalf("expected daily normalization")
	}
	if normalizeRecurrence("yearly") != "" {
		t.Fatalf("unsupported recurrence must be empty")
	}
}
