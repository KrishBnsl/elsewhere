package app

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestUpdateVerifiesAndStagesBundle(t *testing.T) {
	var contents bytes.Buffer
	z := zip.NewWriter(&contents)
	for _, name := range []string{"elsewhere.exe", "dhunn.exe", "../outside"} {
		entry, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Fprint(entry, name); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}

	const asset = "test.zip"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + asset + ".sha256":
			fmt.Fprintf(w, "%x  %s\n", sha256.Sum256(contents.Bytes()), asset)
		case "/" + asset:
			w.Write(contents.Bytes())
		case "/bad/" + asset + ".sha256":
			fmt.Fprintf(w, "%064d\n", 0)
		case "/bad/" + asset:
			w.Write(contents.Bytes())
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	folder := t.TempDir()
	if err := downloadRelease(server.URL, asset, folder); err != nil {
		t.Fatal(err)
	}
	programs := []string{"elsewhere.exe", "dhunn.exe"}
	if err := unpackRelease(folder, asset, programs); err != nil {
		t.Fatal(err)
	}
	for _, program := range programs {
		data, err := os.ReadFile(filepath.Join(folder, program))
		if err != nil || string(data) != program {
			t.Fatalf("staged %s: %q, %v", program, data, err)
		}
	}
	if _, err := os.Stat(filepath.Join(folder, "..", "outside")); !os.IsNotExist(err) {
		t.Fatalf("unexpected archive entry escaped staging: %v", err)
	}
	if err := downloadRelease(server.URL+"/bad", asset, folder); err == nil {
		t.Fatal("accepted a bundle with an incorrect checksum")
	}
}

func TestUpdateRollsBackIfSecondReplacementFails(t *testing.T) {
	bin, staging := t.TempDir(), t.TempDir()
	programs := []string{"elsewhere", "dhunn"}
	for _, name := range programs {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("old "+name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(staging, "elsewhere"), []byte("new elsewhere"), 0755); err != nil {
		t.Fatal(err)
	}
	if keep, err := replacePrograms(bin, staging, programs); keep || err == nil {
		t.Fatalf("expected a successful rollback after replacement failed, got %t, %v", keep, err)
	}
	for _, name := range programs {
		data, err := os.ReadFile(filepath.Join(bin, name))
		if err != nil || string(data) != "old "+name {
			t.Fatalf("%s was not restored: %q, %v", name, data, err)
		}
	}
}

func TestUpdateInstallsBothProgramsAndCanRunAgain(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("this fixture uses Unix executable scripts")
	}
	var contents bytes.Buffer
	gz := gzip.NewWriter(&contents)
	archive := tar.NewWriter(gz)
	bin := t.TempDir()
	programs := []string{"elsewhere", "dhunn"}
	for _, name := range programs {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("old"), 0755); err != nil {
			t.Fatal(err)
		}
		script := "#!/bin/sh\nexit 0\n# " + name + "\n"
		if err := archive.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(script))}); err != nil {
			t.Fatal(err)
		}
		if _, err := archive.Write([]byte(script)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	const asset = "release.tar.gz"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/" + asset + ".sha256":
			fmt.Fprintf(w, "%x\n", sha256.Sum256(contents.Bytes()))
		case "/" + asset:
			w.Write(contents.Bytes())
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	for i := 0; i < 2; i++ {
		if err := installRelease(bin, server.URL, asset, ""); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range programs {
		data, err := os.ReadFile(filepath.Join(bin, name))
		if err != nil || !bytes.HasSuffix(data, []byte("# "+name+"\n")) {
			t.Fatalf("updated %s: %q, %v", name, data, err)
		}
	}
}
