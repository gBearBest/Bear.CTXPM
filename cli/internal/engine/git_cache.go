package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const gitCacheLockWait = 30 * time.Second

func (a *App) gitCacheRepository(ctx context.Context, sourceURL string) (string, func(), error) {
	sum := sha256.Sum256([]byte(sourceURL))
	cacheKey := hex.EncodeToString(sum[:])
	cacheRoot := filepath.Join(a.Root, ".ctxpm", "cache", "git", cacheKey)
	if err := os.MkdirAll(cacheRoot, 0o755); err != nil {
		return "", nil, err
	}
	release, err := acquireCacheLock(ctx, filepath.Join(a.Root, ".ctxpm", "cache", "locks", cacheKey+".lock"))
	if err != nil {
		return "", nil, err
	}
	cacheRepo := filepath.Join(cacheRoot, "repository")
	if _, err := os.Stat(filepath.Join(cacheRepo, ".git")); errors.Is(err, os.ErrNotExist) {
		if _, err := runGit(ctx, "clone", "--filter=blob:none", "--no-checkout", "--quiet", sourceURL, cacheRepo); err != nil {
			release()
			return "", nil, err
		}
	} else if err != nil {
		release()
		return "", nil, err
	} else if _, err := runGit(ctx, "-C", cacheRepo, "fetch", "--quiet", "--filter=blob:none", "origin"); err != nil {
		release()
		return "", nil, err
	}
	return cacheRepo, release, nil
}

func acquireCacheLock(ctx context.Context, path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(gitCacheLockWait)
	for {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = file.Close()
			return func() { _ = os.Remove(path) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > gitCacheLockWait {
			_ = os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for ctxpm git cache lock")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}
