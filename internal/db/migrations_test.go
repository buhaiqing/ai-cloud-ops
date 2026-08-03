package db

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestListMigrationFiles_Ordering(t *testing.T) {
	dir := t.TempDir()
	// intentionally scrambled on disk; expect alphabetical output
	for _, n := range []string{"0003_c.sql", "0001_a.sql", "0002_b.sql", "README.md", "0004_d.sql"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("-- stub"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got := listMigrationFiles(dir)
	want := []string{"0001_a.sql", "0002_b.sql", "0003_c.sql", "0004_d.sql"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ordering wrong\n got: %v\nwant: %v", got, want)
	}
}

func TestListMigrationFiles_FilterNonSQL(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a.sql", "b.txt", "c.sql"} {
		_ = os.WriteFile(filepath.Join(dir, n), []byte(""), 0o644)
	}
	got := listMigrationFiles(dir)
	want := []string{"a.sql", "c.sql"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("filter wrong\n got: %v\nwant: %v", got, want)
	}
}

func TestListMigrationFiles_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	if got := listMigrationFiles(dir); len(got) != 0 {
		t.Fatalf("expected empty, got %v", got)
	}
}

func TestListMigrationFiles_MissingDir(t *testing.T) {
	if got := listMigrationFiles("/nonexistent/path/xyz"); got != nil {
		t.Fatalf("expected nil for missing dir, got %v", got)
	}
}
