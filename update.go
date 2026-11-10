package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const releaseDownloads = "https://github.com/KrishBnsl/elsewhere/releases/latest/download"
const maxReleaseBytes = 512 << 20

var buildVersion = "dev"

func releaseAsset() (asset, suffix string, err error) {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "darwin/arm64":
		return "elsewhere-macos-arm64.tar.gz", "", nil
	case "linux/arm64":
		return "elsewhere-linux-arm64.tar.gz", "", nil
	case "linux/amd64":
		return "elsewhere-linux-x86_64.tar.gz", "", nil
	case "windows/amd64":
		return "elsewhere-windows-x86_64.zip", ".exe", nil
	}
	return "", "", fmt.Errorf("no release bundle for %s/%s; rebuild from source to update", runtime.GOOS, runtime.GOARCH)
}

func executablePath() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(path)
}

func updateRelease() error {
	asset, suffix, err := releaseAsset()
	if err != nil {
		return err
	}
	current, err := executablePath()
	if err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Base(current), "elsewhere"+suffix) {
		return fmt.Errorf("expected an installed elsewhere binary, got %s", current)
	}
	fmt.Println("Checking the latest Elsewhere and Melisma release…")
	return installRelease(filepath.Dir(current), releaseDownloads, asset, suffix)
}

func getReleaseFile(client *http.Client, address string) (io.ReadCloser, error) {
	reply, err := client.Get(address)
	if err != nil {
		return nil, err
	}
	if reply.StatusCode != http.StatusOK {
		reply.Body.Close()
		return nil, fmt.Errorf("%s: %s", address, reply.Status)
	}
	return reply.Body, nil
}

func downloadRelease(base, asset, folder string) error {
	client := &http.Client{Timeout: 5 * time.Minute}
	checksum, err := getReleaseFile(client, base+"/"+asset+".sha256")
	if err != nil {
		return err
	}
	sum, err := io.ReadAll(io.LimitReader(checksum, 4096))
	checksum.Close()
	if err != nil {
		return err
	}
	fields := strings.Fields(string(sum))
	if len(fields) == 0 {
		return errors.New("empty release checksum")
	}
	want, err := hex.DecodeString(fields[0])
	if err != nil || len(want) != sha256.Size {
		return errors.New("invalid release checksum")
	}
	body, err := getReleaseFile(client, base+"/"+asset)
	if err != nil {
		return err
	}
	defer body.Close()
	file, err := os.Create(filepath.Join(folder, asset))
	if err != nil {
		return err
	}
	digest := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(file, digest), io.LimitReader(body, maxReleaseBytes+1))
	if err := errors.Join(copyErr, file.Close()); err != nil {
		return err
	}
	if size > maxReleaseBytes || !bytes.Equal(want, digest.Sum(nil)) {
		return errors.New("release bundle exceeds the size limit or has a checksum mismatch")
	}
	return nil
}

func unpackRelease(folder, asset string, programs []string) error {
	wanted := func(name string) bool {
		return name == programs[0] || name == programs[1]
	}
	write := func(name string, src io.Reader) error {
		out, err := os.OpenFile(filepath.Join(folder, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0755)
		if err != nil {
			return err
		}
		n, err := io.Copy(out, io.LimitReader(src, maxReleaseBytes+1))
		if err := errors.Join(err, out.Close()); err != nil {
			return err
		}
		if n == 0 || n > maxReleaseBytes {
			return fmt.Errorf("invalid size for %s", name)
		}
		return nil
	}
	path := filepath.Join(folder, asset)
	if strings.HasSuffix(asset, ".zip") {
		z, err := zip.OpenReader(path)
		if err != nil {
			return err
		}
		defer z.Close()
		for _, entry := range z.File {
			if !wanted(entry.Name) || !entry.Mode().IsRegular() {
				continue
			}
			r, err := entry.Open()
			if err != nil {
				return err
			}
			err = errors.Join(write(entry.Name, r), r.Close())
			if err != nil {
				return err
			}
		}
		return nil
	}

	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()
	t := tar.NewReader(gz)
	for {
		entry, err := t.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if wanted(entry.Name) && entry.Typeflag == tar.TypeReg {
			if err := write(entry.Name, t); err != nil {
				return err
			}
		}
	}
}

func fileDigest(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return nil, err
	}
	return hash.Sum(nil), nil
}

func installRelease(bin, base, asset, suffix string) error {
	lock, err := os.OpenFile(filepath.Join(bin, ".elsewhere-update.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := lockFile(lock); err != nil {
		return fmt.Errorf("another update may be running: %w", err)
	}
	// Windows can hold the old executable open until the updater process exits.
	// Retry cleanup of completed updates after taking the same installation lock.
	if runtime.GOOS == "windows" {
		entries, _ := os.ReadDir(bin)
		for _, entry := range entries {
			if entry.IsDir() && strings.HasPrefix(entry.Name(), ".elsewhere-update-") {
				path := filepath.Join(bin, entry.Name())
				cleanUpdateBackup(path)
			}
		}
	}
	staging, err := os.MkdirTemp(bin, ".elsewhere-update-")
	if err != nil {
		return err
	}
	keepStaging := false
	defer func() {
		if !keepStaging {
			_ = os.RemoveAll(staging)
		}
	}()
	if err := downloadRelease(base, asset, staging); err != nil {
		return err
	}
	programs := []string{"elsewhere" + suffix, "dhunn" + suffix}
	if err := unpackRelease(staging, asset, programs); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(staging, asset)); err != nil {
		return err
	}
	same := true
	for _, name := range programs {
		candidate, err := fileDigest(filepath.Join(staging, name))
		if err != nil {
			return fmt.Errorf("missing or unreadable %s in the release: %w", name, err)
		}
		installed, err := fileDigest(filepath.Join(bin, name))
		same = same && err == nil && bytes.Equal(candidate, installed)
	}
	if same {
		fmt.Println("Elsewhere and Melisma are already up to date.")
		return nil
	}

	for i, name := range programs {
		flag := "--help"
		if i == 0 {
			flag = "--version"
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		err := exec.CommandContext(ctx, filepath.Join(staging, name), flag).Run()
		cancel()
		if err != nil {
			return fmt.Errorf("cannot run the release's %s %s: %w", name, flag, err)
		}
	}

	keepStaging, err = replacePrograms(bin, staging, programs)
	if err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		if err := os.WriteFile(filepath.Join(staging, "complete"), nil, 0600); err == nil {
			keepStaging = true
			cleanUpdateBackup(staging)
		}
	}
	fmt.Printf("Updated Elsewhere and Melisma in %s. Restart any open player.\n", bin)
	return nil
}

func cleanUpdateBackup(path string) {
	marker := filepath.Join(path, "complete")
	if _, err := os.Stat(marker); err != nil {
		return
	}
	for _, name := range []string{"elsewhere.exe.old", "dhunn.exe.old"} {
		if err := os.Remove(filepath.Join(path, name)); err != nil && !os.IsNotExist(err) {
			return
		}
	}
	_ = os.Remove(marker)
	_ = os.Remove(path)
}

// Move the old binaries aside before installing either replacement. Moving the running
// binary also allows self-replacement on Windows when the image is mapped.
func replacePrograms(bin, staging string, programs []string) (keepStaging bool, err error) {
	var backedUp, installed []string
	defer func() {
		if err == nil {
			return
		}
		var rollback error
		for _, name := range installed {
			rollback = errors.Join(rollback, os.Remove(filepath.Join(bin, name)))
		}
		for _, name := range backedUp {
			rollback = errors.Join(rollback, os.Rename(filepath.Join(staging, name+".old"), filepath.Join(bin, name)))
		}
		if rollback != nil {
			keepStaging = true
			err = errors.Join(err, fmt.Errorf("rollback failed; backups are in %s: %w", staging, rollback))
		}
	}()
	for _, name := range programs {
		target := filepath.Join(bin, name)
		info, statErr := os.Lstat(target)
		if os.IsNotExist(statErr) {
			continue
		}
		if statErr != nil {
			return false, statErr
		}
		if !info.Mode().IsRegular() {
			return false, fmt.Errorf("cannot replace %s: expected a regular file", target)
		}
		if err := os.Rename(target, filepath.Join(staging, name+".old")); err != nil {
			return false, err
		}
		backedUp = append(backedUp, name)
	}
	for _, name := range programs {
		if err := os.Rename(filepath.Join(staging, name), filepath.Join(bin, name)); err != nil {
			return false, err
		}
		installed = append(installed, name)
	}
	return false, nil
}
