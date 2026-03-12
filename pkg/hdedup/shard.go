package hdedup

import (
	"os"
	"path/filepath"

	"github.com/RoaringBitmap/roaring"
)

// Shard 表示一个分片，包含一个键、一个位图以及文件路径信息
type Shard struct {
	key    string
	bitmap *roaring.Bitmap
	path   string
	dirty  bool
}

// LoadShard 加载一个分片，如果该分片对应的文件不存在，则会创建一个新的位图
func LoadShard(baseDir, key string) (*Shard, error) {
	err := os.MkdirAll(baseDir, 0755)
	if err != nil {
		return nil, err
	}

	path := filepath.Join(baseDir, key+".bitmap")

	bm := roaring.New()

	if file, err := os.Open(path); err == nil {
		defer file.Close()
		_, err := bm.ReadFrom(file)
		if err != nil {
			return nil, err
		}
	}

	return &Shard{
		key:    key,
		bitmap: bm,
		path:   path,
	}, nil
}

// Add 向分片的位图中添加一个值，如果位图发生变化则返回true，并标记分片为脏
func (s *Shard) Add(v uint32) {
	s.bitmap.Add(v)
}

// Save 如果分片被标记为脏，则将位图保存到文件中
func (s *Shard) Save() error {
	if !s.dirty {
		return nil
	}
	file, err := os.Create(s.path)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = s.bitmap.WriteTo(file)
	s.dirty = false
	return err
}

// Count 返回分片中位图的基数，即集合中不同元素的数量
func (s *Shard) Count() uint64 {
	return s.bitmap.GetCardinality()
}
