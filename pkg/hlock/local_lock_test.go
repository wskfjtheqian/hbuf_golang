package hlock_test

import (
	"context"
	"errors"
	"testing"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hlock"
)

// 测试NewLocalLock函数
func TestNewLocalLock(t *testing.T) {
	// 测试正常创建锁
	key := "test_key"
	lock1 := hlock.NewLocalLock(key)
	if lock1 == nil {
		t.Errorf("预期创建锁成功，但实际返回nil")
	}

	// 测试重复创建锁
	lock2 := hlock.NewLocalLock(key)
	if lock1 != lock2 {
		t.Errorf("预期返回同一个锁实例，但实际返回了不同的锁实例")
	}
}

// 测试Lock函数
func TestLock(t *testing.T) {
	ctx := context.Background()
	key := "test_lock"
	lock := hlock.NewLocalLock(key)

	// 测试正常加锁
	err := lock.Lock(ctx)
	if err != nil {
		t.Errorf("预期加锁成功，但实际返回错误: %v", err)
	}

	// 测试重复加锁（应阻塞，但此处无法模拟阻塞，只能假设不会出错）
	err = lock.Lock(ctx)
	if err != nil {
		t.Errorf("预期加锁成功，但实际返回错误: %v", err)
	}
}

// 测试Unlock函数
func TestUnlock(t *testing.T) {
	ctx := context.Background()
	key := "test_unlock"
	lock := hlock.NewLocalLock(key)

	// 测试正常解锁
	lock.Lock(ctx)
	err := lock.Unlock(ctx)
	if err != nil {
		t.Errorf("预期解锁成功，但实际返回错误: %v", err)
	}

	// 测试解锁未锁定的锁
	err = lock.Unlock(ctx)
	if err != nil {
		t.Errorf("预期解锁成功，但实际返回错误: %v", err)
	}
}

// 测试TryLock函数
func TestTryLock(t *testing.T) {
	ctx := context.Background()
	key := "test_try_lock"
	lock := hlock.NewLocalLock(key)

	// 测试正常尝试加锁
	locked, err := lock.TryLock(ctx)
	if err != nil {
		t.Errorf("预期尝试加锁成功，但实际返回错误: %v", err)
	}
	if !locked {
		t.Errorf("预期尝试加锁成功，但实际未加锁")
	}

	// 测试重复尝试加锁
	locked, err = lock.TryLock(ctx)
	if err != nil {
		t.Errorf("预期尝试加锁成功，但实际返回错误: %v", err)
	}
	if locked {
		t.Errorf("预期尝试加锁失败，但实际加锁成功")
	}
}

// 测试WithLocalGetOrPopulate函数
func TestWithLocalGetOrPopulate(t *testing.T) {
	ctx := context.Background()
	key := "test_with_local_get_or_populate"

	// 测试happy path
	val, err := hlock.WithLocalGetOrPopulate[int](ctx, key,
		func(ctx context.Context) (int, bool, error) {
			return 0, false, nil
		},
		func(ctx context.Context) (int, error) {
			return 42, nil
		},
	)
	if err != nil {
		t.Errorf("预期操作成功，但实际返回错误: %v", err)
	}
	if val != 42 {
		t.Errorf("预期返回值为42，但实际返回%v", val)
	}

	// 测试get成功
	val, err = hlock.WithLocalGetOrPopulate[int](ctx, key,
		func(ctx context.Context) (int, bool, error) {
			return 100, true, nil
		},
		func(ctx context.Context) (int, error) {
			return 0, errors.New("should not be called")
		},
	)
	if err != nil {
		t.Errorf("预期操作成功，但实际返回错误: %v", err)
	}
	if val != 100 {
		t.Errorf("预期返回值为100，但实际返回%v", val)
	}

	// 测试populate失败
	val, err = hlock.WithLocalGetOrPopulate[int](ctx, key,
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

	// 测试get失败
	val, err = hlock.WithLocalGetOrPopulate[int](ctx, key,
		func(ctx context.Context) (int, bool, error) {
			return 0, false, errors.New("get failed")
		},
		func(ctx context.Context) (int, error) {
			return 0, errors.New("should not be called")
		},
	)
	if err == nil {
		t.Errorf("预期get失败，但实际未返回错误")
	}
}

// 测试WithLocal函数
func TestWithLocal(t *testing.T) {
	ctx := context.Background()
	key := "test_with_local"

	// 测试happy path
	val, err := hlock.WithLocal[int](ctx, key, func(ctx context.Context) (int, error) {
		return 42, nil
	})
	if err != nil {
		t.Errorf("预期操作成功，但实际返回错误: %v", err)
	}
	if val != 42 {
		t.Errorf("预期返回值为42，但实际返回%v", val)
	}

	// 测试fun失败
	val, err = hlock.WithLocal[int](ctx, key, func(ctx context.Context) (int, error) {
		return 0, errors.New("function failed")
	})
	if err == nil {
		t.Errorf("预期fun失败，但实际未返回错误")
	}
}

// 测试WithLocalTry函数
func TestWithLocalTry(t *testing.T) {
	ctx := context.Background()
	key := "test_with_local_try"

	// 测试happy path
	val, err := hlock.WithLocalTry[int](ctx, key, func(ctx context.Context) (int, error) {
		return 42, nil
	})
	if err != nil {
		t.Errorf("预期操作成功，但实际返回错误: %v", err)
	}
	if val != 42 {
		t.Errorf("预期返回值为42，但实际返回%v", val)
	}

	// 测试fun失败
	val, err = hlock.WithLocalTry[int](ctx, key, func(ctx context.Context) (int, error) {
		return 0, errors.New("function failed")
	})
	if err == nil {
		t.Errorf("预期fun失败，但实际未返回错误")
	}

}
