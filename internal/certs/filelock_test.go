//go:build unix || windows

package certs

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestLockCertDirLocksTheSameFileAsBefore is acceptance criterion A6 of
// PLAN 0165, and it guards a silent failure.
//
// fsutil.Acquire appends ".lock" to what it is given, and certLockName already
// ends in ".lock". Hand it certLockName and it locks ".certs.lock.lock" — a file
// nothing else takes. Nothing errors, nothing logs, and two processes generating
// certificates simultaneously stop excluding each other while both believe they
// are locked. The only visible symptom is a key and certificate that do not match,
// much later.
//
// It also pins cross-version compatibility: an older binary locks ".certs.lock",
// so a newer one that locked anything else would not exclude it (C2).
func TestLockCertDirLocksTheSameFileAsBefore(t *testing.T) {
	dir := t.TempDir()

	unlock, err := lockCertDir(dir)
	if err != nil {
		t.Fatalf("lockCertDir: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".certs.lock")); err != nil {
		t.Errorf("the lock file .certs.lock was not created: %v\n\n"+
			"Every earlier release locks this exact name; locking another means a new "+
			"binary and an old one no longer exclude each other.", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".certs.lock.lock")); err == nil {
		t.Error("locked .certs.lock.lock — the suffix was applied twice, so this lock " +
			"excludes nothing (see credstore/lockfile_test.go for the same bug)")
	}

	unlock()
}

// TestLockCertDirExcludes proves the delegation actually locks, rather than
// merely creating a file and returning a working unlock func.
func TestLockCertDirExcludes(t *testing.T) {
	dir := t.TempDir()

	var mu sync.Mutex
	inside, maxInside := 0, 0

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock, err := lockCertDir(dir)
			if err != nil {
				t.Errorf("lockCertDir: %v", err)
				return
			}
			defer unlock()

			mu.Lock()
			inside++
			if inside > maxInside {
				maxInside = inside
			}
			mu.Unlock()

			time.Sleep(20 * time.Millisecond)

			mu.Lock()
			inside--
			mu.Unlock()
		}()
	}
	wg.Wait()

	if maxInside != 1 {
		t.Errorf("max concurrent cert generators = %d, want 1", maxInside)
	}
}

// TestLockCertDirReleases proves the returned unlock frees the lock, so a second
// Ensure in the same process cannot deadlock against the first.
func TestLockCertDirReleases(t *testing.T) {
	dir := t.TempDir()
	for i := range 3 {
		unlock, err := lockCertDir(dir)
		if err != nil {
			t.Fatalf("lockCertDir call %d: %v", i, err)
		}
		unlock()
	}
}
