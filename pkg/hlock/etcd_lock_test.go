package hlock_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hlock"
)

// newTestEtcdContext 返回携带 etcd 连接的测试上下文，环境不可用时跳过测试
func newTestEtcdContext(t *testing.T) context.Context {
	t.Helper()
	if testEtcdSkip != "" {
		t.Skipf("跳过集成测试: %s", testEtcdSkip)
	}
	return testEtcdCtx
}

// TestNewEtcdLock 测试创建 etcd 锁
func TestNewEtcdLock(t *testing.T) {
	// 无 etcd 上下文时应报错
	_, err := hlock.NewEtcdLock(context.Background(), "test_new_etcd_lock")
	if err == nil {
		t.Errorf("预期无 etcd 上下文时创建锁失败，但实际成功")
	}

	ctx := newTestEtcdContext(t)

	// 正常创建
	lock, err := hlock.NewEtcdLock(ctx, "test_new_etcd_lock")
	if err != nil {
		t.Fatalf("预期创建锁成功，但实际返回错误: %v", err)
	}
	if lock == nil {
		t.Fatalf("预期返回锁实例，但实际返回 nil")
	}
}

// TestEtcdLock 测试 etcd 锁的加锁与解锁
func TestEtcdLock(t *testing.T) {
	ctx := newTestEtcdContext(t)
	key := "test_etcd_lock"
	lock, err := hlock.NewEtcdLock(ctx, key)
	if err != nil {
		t.Fatalf("预期创建锁成功，但实际返回错误: %v", err)
	}

	// 正常加锁
	if err := lock.Lock(ctx); err != nil {
		t.Fatalf("预期加锁成功，但实际返回错误: %v", err)
	}

	// 持锁期间其他实例加锁应失败
	other, err := hlock.NewEtcdLock(ctx, key)
	if err != nil {
		t.Fatalf("预期创建锁成功，但实际返回错误: %v", err)
	}
	locked, err := other.TryLock(ctx)
	if err != nil {
		t.Fatalf("预期尝试加锁无错误，但实际返回错误: %v", err)
	}
	if locked {
		t.Errorf("预期持锁期间尝试加锁失败，但实际成功")
	}

	// 正常解锁
	if err := lock.Unlock(ctx); err != nil {
		t.Fatalf("预期解锁成功，但实际返回错误: %v", err)
	}

	// 解锁后其他实例可加锁
	locked, err = other.TryLock(ctx)
	if err != nil {
		t.Fatalf("预期尝试加锁无错误，但实际返回错误: %v", err)
	}
	if !locked {
		t.Errorf("预期解锁后尝试加锁成功，但实际失败")
	}
	_ = other.Unlock(ctx)
}

// TestEtcdLockTimeout 测试 etcd 锁等待期间 ctx 超时
func TestEtcdLockTimeout(t *testing.T) {
	ctx := newTestEtcdContext(t)
	key := "test_etcd_lock_timeout"

	lock, err := hlock.NewEtcdLock(ctx, key)
	if err != nil {
		t.Fatalf("预期创建锁成功，但实际返回错误: %v", err)
	}
	if err := lock.Lock(ctx); err != nil {
		t.Fatalf("预期加锁成功，但实际返回错误: %v", err)
	}
	defer lock.Unlock(ctx)

	timeoutCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	other, err := hlock.NewEtcdLock(ctx, key)
	if err != nil {
		t.Fatalf("预期创建锁成功，但实际返回错误: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- other.Lock(timeoutCtx)
	}()

	select {
	case err = <-errCh:
		if err == nil {
			t.Errorf("预期超时返回错误，实际返回 nil")
		}
	case <-time.After(5 * time.Second):
		t.Errorf("预期及时超时返回，实际未返回")
	}
}

// TestWithEtcd 测试 WithEtcd 加锁执行
func TestWithEtcd(t *testing.T) {
	ctx := newTestEtcdContext(t)
	key := "test_with_etcd"

	// 测试happy path
	val, err := hlock.WithEtcd[int](ctx, key, func(ctx context.Context) (int, error) {
		return 42, nil
	})
	if err != nil {
		t.Fatalf("预期操作成功，但实际返回错误: %v", err)
	}
	if val != 42 {
		t.Errorf("预期返回值为42，但实际返回%v", val)
	}

	// 测试fun失败
	val, err = hlock.WithEtcd[int](ctx, key, func(ctx context.Context) (int, error) {
		return 0, errors.New("function failed")
	})
	if err == nil {
		t.Errorf("预期fun失败，但实际未返回错误")
	}
}

// TestWithEtcdGetOrPopulate 测试 WithEtcdGetOrPopulate
func TestWithEtcdGetOrPopulate(t *testing.T) {
	ctx := newTestEtcdContext(t)
	key := "test_with_etcd_get_or_populate"

	// 测试happy path（get 未命中走 populate）
	val, err := hlock.WithEtcdGetOrPopulate[int](ctx, key,
		func(ctx context.Context) (int, bool, error) {
			return 0, false, nil
		},
		func(ctx context.Context) (int, error) {
			return 42, nil
		},
	)
	if err != nil {
		t.Fatalf("预期操作成功，但实际返回错误: %v", err)
	}
	if val != 42 {
		t.Errorf("预期返回值为42，但实际返回%v", val)
	}

	// 测试get命中（不调用populate）
	val, err = hlock.WithEtcdGetOrPopulate[int](ctx, key,
		func(ctx context.Context) (int, bool, error) {
			return 100, true, nil
		},
		func(ctx context.Context) (int, error) {
			return 0, errors.New("should not be called")
		},
	)
	if err != nil {
		t.Fatalf("预期操作成功，但实际返回错误: %v", err)
	}
	if val != 100 {
		t.Errorf("预期返回值为100，但实际返回%v", val)
	}

	// 测试populate失败
	val, err = hlock.WithEtcdGetOrPopulate[int](ctx, key,
		func(ctx context.Context) (int, bool, error) {
			return 0, false, nil
		},
		func(ctx context.Context) (int, error) {
			return 0, errors.New("populate failed")
		},
	)
	if err == nil {
		t.Errorf("预期populate失败，但实际未返回错误")
	}
}

// TestWithEtcdTry 测试 WithEtcdTry
func TestWithEtcdTry(t *testing.T) {
	ctx := newTestEtcdContext(t)
	key := "test_with_etcd_try"

	// 测试happy path
	val, err := hlock.WithEtcdTry[int](ctx, key, func(ctx context.Context) (int, error) {
		return 42, nil
	})
	if err != nil {
		t.Fatalf("预期操作成功，但实际返回错误: %v", err)
	}
	if val != 42 {
		t.Errorf("预期返回值为42，但实际返回%v", val)
	}

	// 持锁期间尝试加锁应失败
	lock, err := hlock.NewEtcdLock(ctx, key)
	if err != nil {
		t.Fatalf("预期创建锁成功，但实际返回错误: %v", err)
	}
	if err := lock.Lock(ctx); err != nil {
		t.Fatalf("预期加锁成功，但实际返回错误: %v", err)
	}
	defer lock.Unlock(ctx)

	val, err = hlock.WithEtcdTry[int](ctx, key, func(ctx context.Context) (int, error) {
		return 0, errors.New("should not be called")
	})
	if err == nil {
		t.Errorf("预期持锁期间尝试加锁失败，但实际未返回错误")
	}
}

// TestWithEtcdConcurrent 测试多 goroutine 并发 WithEtcd 加锁互斥
func TestWithEtcdConcurrent(t *testing.T) {
	ctx := newTestEtcdContext(t)
	key := "test_with_etcd_concurrent"

	const goroutines = 10
	counter := 0
	errs := make(chan error, goroutines)
	done := make(chan struct{})

	for i := 0; i < goroutines; i++ {
		go func() {
			_, err := hlock.WithEtcd[int](ctx, key, func(ctx context.Context) (int, error) {
				counter++
				time.Sleep(50 * time.Millisecond)
				return counter, nil
			})
			errs <- err
		}()
	}

	for i := 0; i < goroutines; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("预期并发加锁成功，但返回错误: %v", err)
		}
	}
	close(done)

	if counter != goroutines {
		t.Errorf("预期 counter 为 %d，但实际为 %d", goroutines, counter)
	}
}
