package hlog

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

var MaxSize uint64 = 1024 * 1024 * 1800

type SyncWriter interface {
	Flush() error
	Sync() error

	Write(level Level, p []byte) (n int, err error)
}

type consoleWriter struct {
	out io.Writer
}

func newConsoleWriter(out io.Writer) *consoleWriter {
	return &consoleWriter{
		out: out,
	}
}

func (c *consoleWriter) Flush() error {
	return nil
}

func (c *consoleWriter) Sync() error {
	return nil
}

func (c *consoleWriter) Write(level Level, p []byte) (n int, err error) {
	return c.out.Write(p)
}

type fileWriter struct {
	file   *os.File
	out    *bufio.Writer
	nbytes uint64
	level  Level
}

func newFileWriter(level Level) *fileWriter {
	ret := &fileWriter{level: level}
	ret.rotateFile(time.Now())
	return ret
}

func (f *fileWriter) Flush() error {
	return f.out.Flush()
}

func (f *fileWriter) Sync() error {
	return f.file.Sync()
}

func (f *fileWriter) Write(level Level, p []byte) (n int, err error) {
	if level < f.level {
		return 0, err
	}
	if f.nbytes+uint64(len(p)) >= MaxSize {
		if err := f.rotateFile(time.Now()); err != nil {
			return 0, err
		}
	}
	n, err = f.out.Write(p)
	f.nbytes += uint64(n)
	return n, err

}

const bufferSize = 256 * 1024

// rotateFile closes the syncBuffer's file and starts a new one.
func (f *fileWriter) rotateFile(now time.Time) error {
	var err error
	file, _, err := create(f.level.String(), now)
	if f.file != nil {
		f.Flush()
		f.file.Close()
	}

	f.file = file
	f.nbytes = 0
	if err != nil {
		return err
	}
	f.out = bufio.NewWriterSize(f.file, bufferSize)
	return err
}

func create(tag string, now time.Time) (*os.File, string, error) {
	//onceLogDirs.Do(createLogDirs)
	//if len(logDirs) == 0 {
	//	return nil, "", errors.New("log: no log dirs")
	//}
	//name, link := logName(tag, t)
	//var lastErr error
	//for _, dir := range logDirs {
	//	fname := filepath.Join(dir, name)
	//	f, err := os.Create(fname)
	//	if err == nil {
	//		symlink := filepath.Join(dir, link)
	//		os.Remove(symlink)        // ignore err
	//		os.Symlink(name, symlink) // ignore err
	//		if *logLink != "" {
	//			lsymlink := filepath.Join(*logLink, link)
	//			os.Remove(lsymlink)         // ignore err
	//			os.Symlink(fname, lsymlink) // ignore err
	//		}
	//		return f, fname, nil
	//	}
	//	lastErr = err
	//}

	dir := os.TempDir()
	name, link := logName(tag, now)
	dir = filepath.Join(dir, name) + ".log"
	file, err := os.Create(dir)
	if err != nil {
		return nil, "", err
	}

	return file, link, nil
}

// the name for the symlink for tag.
func logName(tag string, t time.Time) (name, link string) {
	name = fmt.Sprintf("%s.%s.%s.log.%s.%04d%02d%02d-%02d%02d%02d.%d",
		"program",
		"host",
		"userName",
		tag,
		t.Year(),
		t.Month(),
		t.Day(),
		t.Hour(),
		t.Minute(),
		t.Second(),
		"pid")
	return name, "program" + "." + tag
}
