package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileCreatesFoldersAndReplacesAtomically(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a", "b", "state")
	for _, want := range []string{"first", "second"} {
		if err := WriteFile(file, []byte(want)); err != nil {
			t.Fatal(err)
		}
		if got, err := os.ReadFile(file); err != nil || string(got) != want {
			t.Fatalf("got %q, %v; want %q", got, err, want)
		}
	}
	entries, _ := os.ReadDir(filepath.Dir(file))
	if len(entries) != 1 {
		t.Fatalf("temporary files were left behind: %v", entries)
	}
}

func TestLockFileIsExclusive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	first, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if err := LockFile(first); err != nil {
		t.Fatal(err)
	}
	if err := LockFile(second); err == nil {
		t.Fatal("a second handle should not get the lock")
	}
}
