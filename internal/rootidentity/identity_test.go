package rootidentity

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReattachOriginalDirectory(t *testing.T) {
	parent := t.TempDir()
	path := filepath.Join(parent, "disk")
	if _, err := Bind(path); err == nil {
		t.Fatal("bound a missing root")
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	id, err := Bind(path)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Bind(path)
	if err != nil || id != again {
		t.Fatalf("unstable binding: %q, %v", again, err)
	}
	if err := Check(path, id); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(parent, "away")
	if err := os.Rename(path, original); err != nil {
		t.Fatal(err)
	}
	if err := Check(path, id); err == nil {
		t.Fatal("accepted absent disk")
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := Check(path, id); err == nil {
		t.Fatal("accepted replacement directory")
	}
	names, err := os.ReadDir(path)
	if err != nil || len(names) != 0 {
		t.Fatalf("modified replacement: %v, %v", names, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(original, path); err != nil {
		t.Fatal(err)
	}
	if err := Check(path, id); err != nil {
		t.Fatal(err)
	}
	var parsed identity
	if err := json.Unmarshal([]byte(id), &parsed); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(path, markerPrefix+parsed.Token)
	file, err := os.OpenFile(marker, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = file.WriteString("not a marker")
	if err := errors.Join(err, file.Close()); err != nil {
		t.Fatal(err)
	}
	if err := Check(path, id); err == nil {
		t.Fatal("accepted modified marker")
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(marker, 0700); err != nil {
		t.Fatal(err)
	}
	if err := Check(path, id); err == nil {
		t.Fatal("accepted a directory as the marker")
	}
}

func TestVolumeMismatchEvenWithMatchingMarker(t *testing.T) {
	path := t.TempDir()
	id, err := Bind(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved identity
	if err := json.Unmarshal([]byte(id), &saved); err != nil {
		t.Fatal(err)
	}
	saved.Volume = "different-volume"
	wrong, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	if err := Check(path, string(wrong)); !errors.Is(err, ErrDifferentRoot) {
		t.Fatalf("changed volume error = %v", err)
	}
	if err := Check(path, id); err != nil {
		t.Fatal(err)
	}
}
