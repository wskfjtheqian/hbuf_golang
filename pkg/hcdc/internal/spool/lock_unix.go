//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package spool

import (
	"os"

	"golang.org/x/sys/unix"
)

// lockFile 为缓冲目录的锁文件取得非阻塞排他锁。
func lockFile(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
}
