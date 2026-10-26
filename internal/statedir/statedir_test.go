package statedir

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"testing"
)

func TestResolve(t *testing.T) {
	override := filepath.Join(t.TempDir(), "override")
	t.Setenv("LUGARN_COMMS_HOME", override)
	if got, err := Resolve(); err != nil || got != override {
		t.Fatalf("Resolve = %q, %v", got, err)
	}
	t.Setenv("LUGARN_COMMS_HOME", "")
	home := t.TempDir()
	var want string
	if runtime.GOOS == "windows" {
		t.Setenv("LOCALAPPDATA", home)
		want = filepath.Join(home, "Lugarn", "comms")
	} else {
		t.Setenv("HOME", home)
		want = filepath.Join(home, ".lugarn", "comms")
	}
	if got, err := Resolve(); err != nil || got != want {
		t.Fatalf("Resolve = %q, %v; want %q", got, err, want)
	}
	if runtime.GOOS == "windows" {
		t.Setenv("LOCALAPPDATA", "")
		if _, err := Resolve(); err == nil {
			t.Fatal("missing LOCALAPPDATA accepted")
		}
	}
}

func TestEnsureAndWriteSecret(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "state")
	if err := Ensure(dir); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{dir, filepath.Join(dir, "launch")} {
		assertPrivate(t, path, true)
	}
	if runtime.GOOS != "windows" {
		for _, path := range []string{dir, filepath.Join(dir, "launch")} {
			if err := os.Chmod(path, 0777); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := Ensure(dir); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{dir, filepath.Join(dir, "launch")} {
		assertPrivate(t, path, true)
	}
	path := filepath.Join(dir, "launch", "test.token")
	for _, value := range []string{"first secret\n", "replacement\n"} {
		if err := WriteSecret(path, []byte(value)); err != nil {
			t.Fatal(err)
		}
		assertPrivate(t, path, false)
		got, err := os.ReadFile(path)
		if err != nil || string(got) != value {
			t.Fatalf("read: %q, %v", got, err)
		}
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary files leaked: %v, %v", entries, err)
	}
}

func TestWriteSecretFailureCleansTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "directory")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := WriteSecret(path, []byte("secret")); err == nil {
		t.Fatal("overwrote directory")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary files leaked: %v, %v", entries, err)
	}
}

func TestWriteSecretAtomicReaders(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	if err := WriteSecret(path, []byte("aaaaaaaa")); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	done := make(chan struct{})
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
			}
			data, err := os.ReadFile(path)
			// Windows rename may be refused while a reader holds an open handle,
			// and a reader may be refused (ERROR_SHARING_VIOLATION 32,
			// ERROR_ACCESS_DENIED 5) while the file is being replaced. Successful
			// reads must still always see a complete value.
			if err != nil {
				if runtime.GOOS == "windows" && (errors.Is(err, syscall.Errno(32)) || errors.Is(err, syscall.Errno(5))) {
					continue
				}
				t.Errorf("read: %v", err)
				return
			}
			if string(data) != "aaaaaaaa" && string(data) != "bbbbbbbb" {
				t.Errorf("partial write: %q", data)
				return
			}
		}
	}()
	for i := 0; i < 20; i++ {
		if err := WriteSecret(path, []byte("bbbbbbbb")); err != nil {
			// ERROR_SHARING_VIOLATION (32) is possible when a Windows reader
			// denies delete sharing. The existing complete file must survive.
			if runtime.GOOS != "windows" || (!os.IsPermission(err) && !errors.Is(err, syscall.Errno(32))) {
				t.Error(err)
			}
		}
	}
	close(done)
	wg.Wait()
}

func TestEnsureRejectsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(path); err == nil {
		t.Fatal("file accepted as state directory")
	}
}
