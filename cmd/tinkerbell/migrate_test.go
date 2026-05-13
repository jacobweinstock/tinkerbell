package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestMigrateRequiresWorkdir(t *testing.T) {
	var out, errOut bytes.Buffer
	err := runMigrate(context.Background(), &migrateConfig{Report: "tui"}, &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "--workdir is required") {
		t.Fatalf("expected --workdir is required error, got %v", err)
	}
}

func TestMigrateRejectsBadReportFormat(t *testing.T) {
	var out, errOut bytes.Buffer
	err := runMigrate(context.Background(), &migrateConfig{Workdir: t.TempDir(), Report: "yaml"}, &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "--report") {
		t.Fatalf("expected --report validation error, got %v", err)
	}
}

func TestMigrateCommandShape(t *testing.T) {
	cmd := newMigrateCommand()
	if cmd.Name != "migrate" {
		t.Fatalf("name = %q", cmd.Name)
	}
	if cmd.Flags == nil {
		t.Fatal("Flags must not be nil")
	}
	if cmd.Exec == nil {
		t.Fatal("Exec must not be nil")
	}
}
