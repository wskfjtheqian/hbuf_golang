package htwheel_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/htwheel"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hutl"
)

func TestHtWheel(t *testing.T) {
	s := htwheel.NewScheduler()

	fmt.Println("start:", time.Now())

	// 添加任务
	s.Schedule(1, hutl.NowTime().Add(5*time.Second), func(id uint64, t time.Time) {
		fmt.Println("task1 executed:", time.Now())
	})

	// 查询
	if t, ok := s.Get(1); ok {
		fmt.Println("found task expire:", time.Unix(0, t.Expire()))
	}

	// TTL
	if ttl, ok := s.TTL(1); ok {
		fmt.Println("ttl:", ttl)
	}

	// 更新（提前执行）
	time.Sleep(2 * time.Second)
	s.Update(1, hutl.NowTime().Add(1*time.Second))

	time.Sleep(5 * time.Second)
	s.Stop()
}
