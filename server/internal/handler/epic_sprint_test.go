package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
)

func TestValidEpicStatuses(t *testing.T) {
	valid := []string{"planned", "in_progress", "completed", "cancelled"}
	for _, s := range valid {
		if !validEpicStatus(s) {
			t.Errorf("expected %q to be valid epic status", s)
		}
	}
	invalid := []string{"", "active", "done", "unknown"}
	for _, s := range invalid {
		if validEpicStatus(s) {
			t.Errorf("expected %q to be invalid epic status", s)
		}
	}
}

func TestValidSprintStatuses(t *testing.T) {
	valid := []string{"planned", "active", "completed", "cancelled"}
	for _, s := range valid {
		if !validSprintStatus(s) {
			t.Errorf("expected %q to be valid sprint status", s)
		}
	}
	invalid := []string{"", "in_progress", "done", "unknown"}
	for _, s := range invalid {
		if validSprintStatus(s) {
			t.Errorf("expected %q to be invalid sprint status", s)
		}
	}
}

func TestEpicColorValidation(t *testing.T) {
	validColors := []struct {
		input    string
		expected string
	}{
		{"#3b82f6", "#3b82f6"},
		{"#FFFFFF", "#ffffff"},
		{"10b981", "#10b981"},
		{" #6366F1 ", "#6366f1"},
	}

	for _, tc := range validColors {
		got, err := normalizeColor(tc.input)
		if err != nil {
			t.Errorf("normalizeColor(%q) unexpected error: %v", tc.input, err)
		}
		if got != tc.expected {
			t.Errorf("normalizeColor(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}

	invalidColors := []string{
		"",
		"blue",
		"rgb(0,0,0)",
		"#12345",
		"#1234567",
		"javascript:alert(1)",
		"<script>",
		"#12345z",
	}

	for _, input := range invalidColors {
		if _, err := normalizeColor(input); err == nil {
			t.Errorf("normalizeColor(%q) expected error, got nil", input)
		}
	}
}

func TestValidateLeadPair(t *testing.T) {
	h := &Handler{}
	wsUUID := util.MustParseUUID("00000000-0000-0000-0000-000000000001")
	leadUUID := util.MustParseUUID("00000000-0000-0000-0000-000000000002")

	// Both unset: valid
	status, msg := h.validateLeadPair(context.Background(), nil, wsUUID, pgtype.Text{Valid: false}, pgtype.UUID{Valid: false})
	if status != 0 {
		t.Errorf("expected status 0 for unset lead pair, got %d (%s)", status, msg)
	}

	// Only type set: 400
	status, _ = h.validateLeadPair(context.Background(), nil, wsUUID, pgtype.Text{String: "member", Valid: true}, pgtype.UUID{Valid: false})
	if status != http.StatusBadRequest {
		t.Errorf("expected 400 when only lead_type is set, got %d", status)
	}

	// Only ID set: 400
	status, _ = h.validateLeadPair(context.Background(), nil, wsUUID, pgtype.Text{Valid: false}, leadUUID)
	if status != http.StatusBadRequest {
		t.Errorf("expected 400 when only lead_id is set, got %d", status)
	}

	// Invalid lead_type: 400
	status, _ = h.validateLeadPair(context.Background(), nil, wsUUID, pgtype.Text{String: "invalid", Valid: true}, leadUUID)
	if status != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid lead_type, got %d", status)
	}
}
