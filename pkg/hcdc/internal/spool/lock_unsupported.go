//go:build !(darwin || dragonfly || freebsd || linux || netbsd || openbsd)

package spool

import (
	"errors"
	"os"
	"runtime"
)

// lockFile 在无法提供缓冲目录排他所有权的平台上返回错误。
func lockFile(_ *os.File) error {
	return errors.New("durable spool directory ownership is unavailable on " + runtime.GOOS)
}
