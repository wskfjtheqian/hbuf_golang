package hdedup

import (
	"bufio"
	"encoding/binary"
	"os"
	"sync"
)

// WAL 表示一个写前日志（Write-Ahead Log）结构，用于持久化存储键和哈希值
type WAL struct {
	mu     sync.Mutex
	file   *os.File
	writer *bufio.Writer
}

// OpenWAL 打开或创建一个写前日志文件，并返回一个 WAL 实例
func OpenWAL(path string) (*WAL, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	return &WAL{
		file:   file,
		writer: bufio.NewWriter(file),
	}, nil
}

// Append 向写前日志中追加一个键和其对应的哈希值
func (w *WAL) Append(key string, hash uint32) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	keyBytes := []byte(key)

	if err := binary.Write(w.writer, binary.LittleEndian, uint16(len(keyBytes))); err != nil {
		return err
	}
	if _, err := w.writer.Write(keyBytes); err != nil {
		return err
	}
	if err := binary.Write(w.writer, binary.LittleEndian, hash); err != nil {
		return err
	}

	return w.writer.Flush()
}

// Replay 重放写前日志中的所有条目，并对每个条目调用提供的处理器函数
func (w *WAL) Replay(handler func(string, uint32)) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, err := w.file.Seek(0, 0); err != nil {
		return err
	}

	reader := bufio.NewReader(w.file)

	for {
		var keyLen uint16
		err := binary.Read(reader, binary.LittleEndian, &keyLen)
		if err != nil {
			break
		}

		keyBytes := make([]byte, keyLen)
		if _, err := reader.Read(keyBytes); err != nil {
			break
		}

		var hash uint32
		if err := binary.Read(reader, binary.LittleEndian, &hash); err != nil {
			break
		}

		handler(string(keyBytes), hash)
	}
	return nil
}
