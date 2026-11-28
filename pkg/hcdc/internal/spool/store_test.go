//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package spool

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// openTestStore 打开测试缓冲目录，并注册关闭清理。
func openTestStore(t *testing.T, dir string) *Store {
	t.Helper()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	return store
}

// TestStoreRejectsCorruption 验证缓冲区拒绝校验失败的持久记录。
func TestStoreRejectsCorruption(t *testing.T) {
	dir := t.TempDir()
	store := openTestStore(t, dir)
	if err := store.Append(1, []byte("payload")); err != nil {
		t.Fatal(err)
	}
	path := store.recordPath(1)
	file, err := os.OpenFile(path, os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.WriteAt([]byte("P"), int64(encodedOverhead))
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatalf("corrupt record: write=%v close=%v", writeErr, closeErr)
	}
	if _, err := store.Read(1); err == nil {
		t.Fatal("read accepted a bad payload checksum")
	}
	if err := store.Append(1, []byte("payload")); err == nil {
		t.Fatal("append replaced a corrupt published record")
	}
}

// TestStoreRecoversAfterProcessKill 验证异常退出后保留已发布记录，并清理未发布临时文件。
func TestStoreRecoversAfterProcessKill(t *testing.T) {
	const childDirKey = "CDC_SPOOL_TEST_CHILD_DIR"
	if childDir := os.Getenv(childDirKey); childDir != "" {
		store, err := Open(childDir)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Append(1, []byte("published")); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(childDir, pendingPrefix+"partial"), []byte("partial"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Fprintln(os.Stdout, "published"); err != nil {
			t.Fatal(err)
		}
		var stop [1]byte
		_, err = os.Stdin.Read(stop[:])
		t.Fatalf("spool writer stopped without receiving a kill signal: %v", err)
	}

	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestStoreRecoversAfterProcessKill$")
	child.Env = append(os.Environ(), childDirKey+"="+dir)
	output, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	input, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if child.ProcessState == nil {
			cancel()
			_ = child.Wait()
		}
	}()
	ready, err := bufio.NewReader(output).ReadString('\n')
	if err != nil || ready != "published\n" {
		t.Fatalf("spool owner did not signal readiness: %q, %v", ready, err)
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	var exitError *exec.ExitError
	if err := child.Wait(); !errors.As(err, &exitError) {
		t.Fatalf("killed spool owner exited with %v", err)
	}

	recovered := openTestStore(t, dir)
	if _, err := os.Stat(filepath.Join(dir, pendingPrefix+"partial")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("unpublished file after recovery: %v", err)
	}
	if payload, err := recovered.Read(1); err != nil || string(payload) != "published" {
		t.Fatalf("read confirmed record = %q, %v", payload, err)
	}
}
