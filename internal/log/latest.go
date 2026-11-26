package log

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	takeoverWait = time.Second
	writeWait    = 100 * time.Millisecond
	lockPoll     = 2 * time.Millisecond
	maxToken     = 64
)

var errLockBusy = errors.New("latest.lock is held by another process")

// latestWriter is this process's claim on latest.log. The newest process to start owns the file;
// latest.lock holds the owner token, and every write checks it under the OS lock, so an older
// process never writes into a newer one's log (docs/contracts/cli/logs.md section 3).
type latestWriter struct {
	lock, file *os.File
	token      string
	size       int64
}

func lockWithin(f *os.File, wait time.Duration) error {
	deadline := time.Now().Add(wait)
	for {
		ok, err := tryLock(f)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		if time.Now().After(deadline) {
			return errLockBusy
		}
		time.Sleep(lockPoll)
	}
}

// takeLatest claims latest.log for token and starts it with head.
func takeLatest(dir, token string, head []byte) (*latestWriter, error) {
	lock, err := openFile(filepath.Join(dir, lockName), os.O_CREATE|os.O_RDWR)
	if err != nil {
		return nil, fmt.Errorf("latest.log: %w", err)
	}
	if err := lockWithin(lock, takeoverWait); err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("latest.log: %w", err)
	}
	defer unlock(lock)
	w := &latestWriter{lock: lock, token: token}
	if err := w.claim(dir, head); err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("latest.log: %w", err)
	}
	return w, nil
}

// claim runs under the lock: new token, empty file, the lines kept so far.
func (w *latestWriter) claim(dir string, head []byte) error {
	f, err := openFile(filepath.Join(dir, LatestName), os.O_CREATE|os.O_WRONLY)
	if err != nil {
		return err
	}
	var n int
	err = w.lock.Truncate(0)
	if err == nil {
		_, err = w.lock.WriteAt([]byte(w.token), 0)
	}
	if err == nil {
		err = f.Truncate(0)
	}
	if err == nil {
		n, err = f.Write(head)
	}
	if err != nil {
		_ = f.Close()
		return err
	}
	w.file, w.size = f, int64(n)
	return nil
}

// write appends line while this process still owns the file. A lock that stays busy skips the line
// here only; another owner ends the claim for good.
func (w *latestWriter) write(line []byte) {
	if !w.acquire() {
		return
	}
	defer unlock(w.lock)
	if w.size+int64(len(line)) <= maxSessionBytes {
		n, _ := w.file.Write(line)
		w.size += int64(n)
	}
}

// owns reports whether latest.log is still this process's.
func (w *latestWriter) owns() bool {
	if !w.acquire() {
		return false
	}
	unlock(w.lock)
	return true
}

// acquire takes the lock and checks the token. When it returns true the caller holds the lock;
// when the token is another's the claim is dropped.
func (w *latestWriter) acquire() bool {
	if w.file == nil || lockWithin(w.lock, writeWait) != nil {
		return false
	}
	b := make([]byte, maxToken)
	n, _ := w.lock.ReadAt(b, 0)
	if string(b[:n]) == w.token {
		return true
	}
	unlock(w.lock)
	w.close()
	return false
}

func (w *latestWriter) close() {
	if w.file != nil {
		_ = w.file.Close()
		_ = w.lock.Close()
		w.file, w.lock = nil, nil
	}
}
