package id

// Generator Id生成器接口
type Generator interface {
	NextId() (int64, error)

	NextIds(count int) ([]int64, error)
}
