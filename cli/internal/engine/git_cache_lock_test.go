package engine

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestCacheLockChildProcess(t *testing.T) {
	path := os.Getenv("CTXPM_TEST_CACHE_LOCK")
	if path == "" {
		return
	}
	release, err := acquireCacheLock(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	fmt.Println("locked")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}

func TestCacheLockWaitsForLiveProcessAndRecoversAfterCrash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.lock")
	cmd := exec.Command(os.Args[0], "-test.run=^TestCacheLockChildProcess$")
	cmd.Env = append(os.Environ(), "CTXPM_TEST_CACHE_LOCK="+path)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stdin.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(stdout).ReadString('\n'); ready <- line }()
	select {
	case line := <-ready:
		if line != "locked\n" {
			t.Fatalf("child failed to lock: %q", line)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("child did not acquire lock")
	}
	// A live owner remains protected regardless of the lock file age.
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	if release, err := acquireCacheLock(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		if release != nil {
			release()
		}
		t.Fatalf("contended lock: %v, want deadline exceeded", err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	ctx2, cancel2 := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel2()
	release, err := acquireCacheLock(ctx2, path)
	if err != nil {
		t.Fatalf("lock after process death: %v", err)
	}
	release()
	release()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("lock file should remain: %v", err)
	}
}

func TestCacheLockHonorsAlreadyCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if release, err := acquireCacheLock(ctx, filepath.Join(t.TempDir(), "lock")); !errors.Is(err, context.Canceled) {
		if release != nil {
			release()
		}
		t.Fatalf("got %v, want canceled", err)
	}
}
