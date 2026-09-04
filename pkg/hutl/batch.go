package hutl

// NewBatchProcess 分批处理数据
func NewBatchProcess[T any](batchNum int, batchFun func(batch []T) error) *BatchProcess[T] {
	return &BatchProcess[T]{
		batchNum: batchNum,
		batchFun: batchFun,
	}
}

type BatchProcess[T any] struct {
	data     []T
	batchNum int
	batchFun func(batch []T) error
}

func (bp *BatchProcess[T]) AddData(data T) error {
	bp.data = append(bp.data, data)
	if len(bp.data) >= bp.batchNum {
		if err := bp.batchFun(bp.data); err != nil {
			return err
		}
		bp.data = []T{}
	}
	return nil
}

func (bp *BatchProcess[T]) Finish() error {
	if len(bp.data) > 0 {
		if err := bp.batchFun(bp.data); err != nil {
			return err
		}
	}
	return nil
}
