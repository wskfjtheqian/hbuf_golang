package utl

import (
	"context"
	"sync"
	"testing"
	"time"
)

// 测试 KeyLock 的单元测试
func TestKeyLock(t *testing.T) {
	// 定义一个简单的函数用于测试
	testFunc := func(ctx context.Context) error {
		// 模拟一些工作
		time.Sleep(2 * time.Second)
		return nil
	}

	// 测试成功的情况
	t.Run("HappyPath", func(t *testing.T) {
		ctx := context.Background()
		err := KeyLock(ctx, "testKey", testFunc)
		if err != nil {
			t.Errorf("期望成功，但错误为: %v", err)
		}
	})

	// 测试重复加锁的情况
	t.Run("RecursiveLock", func(t *testing.T) {
		ctx := context.Background()
		err := KeyLock(ctx, "testKey", func(ctx context.Context) error {
			// 在同一个 key 上再次加锁
			return KeyLock(ctx, "testKey", testFunc)
		})
		if err != nil {
			t.Errorf("期望成功，但错误为: %v", err)
		}
	})

	// 测试传入nil上下文的情况
	t.Run("NilContext", func(t *testing.T) {
		err := KeyLock(nil, "testKey", testFunc)
		if err == nil {
			t.Errorf("期望非nil错误，但错误为nil")
		}
	})

	// 测试上下文中已有锁的情况
	t.Run("AlreadyLocked", func(t *testing.T) {
		ctx := context.Background()
		KeyLock(ctx, "testKey", testFunc)        // 第一次加锁
		err := KeyLock(ctx, "testKey", testFunc) // 再次加锁
		if err != nil {
			t.Errorf("期望成功，但错误为: %v", err)
		}
	})

	// 测试无效的 key
	t.Run("InvalidKey", func(t *testing.T) {
		ctx := context.Background()
		err := KeyLock(ctx, "", testFunc) // 使用空 key
		if err == nil {
			t.Errorf("期望非nil错误，但错误为nil")
		}
	})

	// 测试关闭的情况下
	t.Run("LockingWhileCleaning", func(t *testing.T) {
		ctx := context.Background()
		NewKeyLock("cleanupTest")    // 手动创建一个锁
		time.Sleep(time.Second * 35) // 等待清理的 goroutine 触发
		err := KeyLock(ctx, "cleanupTest", testFunc)
		if err != nil {
			t.Errorf("期望成功，但错误为: %v", err)
		}
	})
}

// 测试 KeyLock 的并发用例
func TestKeyLockConcurrency(t *testing.T) {
	var wg sync.WaitGroup
	ctx := context.Background()
	key := "concurrentKey"

	// 并发执行多个加锁操作
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := KeyLock(ctx, key, func(ctx context.Context) error {
				time.Sleep(100 * time.Millisecond)
				return nil
			})
			if err != nil {
				t.Errorf("协程 %d 期望成功，但错误为: %v", i, err)
			}
		}(i)
	}

	wg.Wait()
}

// 测试 KeyLock 的并发递归加锁
func TestKeyLockConcurrentRecursive(t *testing.T) {
	var wg sync.WaitGroup
	ctx := context.Background()
	key := "recursiveKey"

	// 并发执行多个递归加锁操作
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := KeyLock(ctx, key, func(ctx context.Context) error {
				return KeyLock(ctx, key, func(ctx context.Context) error {
					time.Sleep(100 * time.Millisecond)
					return nil
				})
			})
			if err != nil {
				t.Errorf("协程 %d 期望成功，但错误为: %v", i, err)
			}
		}(i)
	}

	wg.Wait()
}

// 测试 KeyLock 的并发无效 key
func TestKeyLockConcurrentInvalidKey(t *testing.T) {
	var wg sync.WaitGroup
	ctx := context.Background()

	// 并发执行多个无效 key 的加锁操作
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := KeyLock(ctx, "", func(ctx context.Context) error {
				time.Sleep(100 * time.Millisecond)
				return nil
			})
			if err == nil {
				t.Errorf("协程 %d 期望非nil错误，但错误为nil", i)
			}
		}(i)
	}

	wg.Wait()
}

// 测试 KeyLock 的并发清理
func TestKeyLockConcurrentCleanup(t *testing.T) {
	var wg sync.WaitGroup
	ctx := context.Background()
	key := "cleanupKey"

	// 手动创建一个锁
	NewKeyLock(key)

	// 等待清理的 goroutine 触发
	time.Sleep(time.Second * 35)

	// 并发执行多个加锁操作
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := KeyLock(ctx, key, func(ctx context.Context) error {
				time.Sleep(100 * time.Millisecond)
				return nil
			})
			if err != nil {
				t.Errorf("协程 %d 期望成功，但错误为: %v", i, err)
			}
		}(i)
	}

	wg.Wait()
}
