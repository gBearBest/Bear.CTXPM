package engine

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

var (
	kernel32Lock     = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = kernel32Lock.NewProc("LockFileEx")
	procUnlockFileEx = kernel32Lock.NewProc("UnlockFileEx")
)

const (
	lockfileFailImmediately               = 0x00000001
	lockfileExclusiveLock                 = 0x00000002
	errorLockViolation      syscall.Errno = 33
)

func tryLockCacheFile(file *os.File) (bool, error) {
	var overlapped syscall.Overlapped
	result, _, err := procLockFileEx.Call(file.Fd(), lockfileExclusiveLock|lockfileFailImmediately, 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
	if result != 0 {
		return true, nil
	}
	if err == errorLockViolation {
		return false, nil
	}
	return false, fmt.Errorf("lock git cache: %w", err)
}

func unlockCacheFile(file *os.File) error {
	var overlapped syscall.Overlapped
	result, _, err := procUnlockFileEx.Call(file.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
	if result == 0 {
		return fmt.Errorf("unlock git cache: %w", err)
	}
	return nil
}
