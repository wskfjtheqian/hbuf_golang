package hlock_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hlock"
)

// TestNewLocalLock 测试正常创建锁的情况
func TestNewLocalLock(t *testing.T) {
	key := "test_key"
	lock1 := hlock.NewLocalLock(key)
	lock2 := hlock.NewLocalLock(key)

	// 如果是同一个key，应该返回同一个锁实例
	if lock1 != lock2 {
		t.Errorf("expected the same lock instance, got different ones")
	}
}

// TestLockUnlock 测试正常加锁解锁的情况
func TestLockUnlock(t *testing.T) {
	key := "test_key"
	lock := hlock.NewLocalLock(key)
	ctx := context.Background()

	// 测试加锁
	if err := lock.Lock(ctx); err != nil {
		t.Errorf("expected nil error, got %v", err)
	}

	// 测试解锁
	if err := lock.Unlock(ctx); err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

// TestTryLock 成功尝试加锁的情况
func TestTryLock(t *testing.T) {
	key := "test_key"
	lock := hlock.NewLocalLock(key)
	ctx := context.Background()

	locked, err := lock.TryLock(ctx)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if !locked {
		t.Errorf("expected lock to be acquired, got %v", locked)
	}

	// 测试解锁
	if err := lock.Unlock(ctx); err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

// TestTryLockFailed 尝试加锁失败的情况
func TestTryLockFailed(t *testing.T) {
	key := "test_key"
	lock := hlock.NewLocalLock(key)
	ctx := context.Background()

	// 首先加锁
	if err := lock.Lock(ctx); err != nil {
		t.Errorf("expected nil error, got %v", err)
	}

	// 再次尝试加锁，应该失败
	locked, err := lock.TryLock(ctx)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if locked {
		t.Errorf("expected lock to not be acquired, got %v", locked)
	}

	// 测试解锁
	if err := lock.Unlock(ctx); err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

// TestWithLocalGetOrPopulate 成功获取值的情况
func TestWithLocalGetOrPopulate(t *testing.T) {
	ctx := context.Background()
	key := "test_key"

	get := func(ctx context.Context) (string, bool, error) {
		return "value", true, nil
	}

	populate := func(ctx context.Context) (string, error) {
		return "value", nil
	}

	val, err := hlock.WithLocalGetOrPopulate(ctx, key, get, populate)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if val != "value" {
		t.Errorf("expected value to be 'value', got %v", val)
	}
}

// TestWithLocalGetOrPopulatePopulate 测试需要调用populate函数的情况
func TestWithLocalGetOrPopulatePopulate(t *testing.T) {
	ctx := context.Background()
	key := "test_key"

	get := func(ctx context.Context) (string, bool, error) {
		return "", false, nil
	}

	populate := func(ctx context.Context) (string, error) {
		return "value", nil
	}

	val, err := hlock.WithLocalGetOrPopulate(ctx, key, get, populate)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if val != "value" {
		t.Errorf("expected value to be 'value', got %v", val)
	}
}

// TestWithLocalGetOrPopulateError 测试get函数返回错误的情况
func TestWithLocalGetOrPopulateError(t *testing.T) {
	ctx := context.Background()
	key := "test_key"

	get := func(ctx context.Context) (string, bool, error) {
		return "", false, errors.New("get error")
	}

	populate := func(ctx context.Context) (string, error) {
		return "value", nil
	}

	_, err := hlock.WithLocalGetOrPopulate(ctx, key, get, populate)
	if err == nil {
		t.Errorf("expected error, got nil")
	}
}

// TestWithLocalGetOrPopulatePopulateError 测试populate函数返回错误的情况
func TestWithLocalGetOrPopulatePopulateError(t *testing.T) {
	ctx := context.Background()
	key := "test_key"

	get := func(ctx context.Context) (string, bool, error) {
		return "", false, nil
	}

	populate := func(ctx context.Context) (string, error) {
		return "", errors.New("populate error")
	}

	_, err := hlock.WithLocalGetOrPopulate(ctx, key, get, populate)
	if err == nil {
		t.Errorf("expected error, got nil")
	}
}

// TestWithLocal 成功执行函数的情况
func TestWithLocal(t *testing.T) {
	ctx := context.Background()
	key := "test_key"

	fun := func(ctx context.Context) (string, error) {
		return "value", nil
	}

	val, err := hlock.WithLocal(ctx, key, fun)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if val != "value" {
		t.Errorf("expected value to be 'value', got %v", val)
	}
}

// TestWithLocalError 测试执行函数返回错误的情况
func TestWithLocalError(t *testing.T) {
	ctx := context.Background()
	key := "test_key"

	fun := func(ctx context.Context) (string, error) {
		return "", errors.New("function error")
	}

	_, err := hlock.WithLocal(ctx, key, fun)
	if err == nil {
		t.Errorf("expected error, got nil")
	}
}

// TestWithTryLocalLock 成功执行函数的情况
func TestWithTryLocalLock(t *testing.T) {
	ctx := context.Background()
	key := "test_key"

	fun := func(ctx context.Context) (string, error) {
		return "value", nil
	}

	val, err := hlock.WithTryLocalLock(ctx, key, fun)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if val != "value" {
		t.Errorf("expected value to be 'value', got %v", val)
	}
}

// TestWithTryLocalLockError 测试执行函数返回错误的情况
func TestWithTryLocalLockError(t *testing.T) {
	ctx := context.Background()
	key := "test_key"

	fun := func(ctx context.Context) (string, error) {
		return "", errors.New("function error")
	}

	_, err := hlock.WithTryLocalLock(ctx, key, fun)
	if err == nil {
		t.Errorf("expected error, got nil")
	}
}

// TestWithLocalGetOrPopulateConcurrent 测试并发情况
func TestWithLocalGetOrPopulateConcurrent(t *testing.T) {
	ctx := context.Background()
	key := "test_key"

	var wg sync.WaitGroup
	var results []string

	get := func(ctx context.Context) (string, bool, error) {
		return "", false, nil
	}

	populate := func(ctx context.Context) (string, error) {
		time.Sleep(10 * time.Millisecond) // 模拟耗时操作
		return "value", nil
	}

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			val, err := hlock.WithLocalGetOrPopulate(ctx, key, get, populate)
			if err != nil {
				t.Errorf("expected nil error, got %v", err)
			}
			results = append(results, val)
		}()
	}

	wg.Wait()

	// 检查所有结果是否一致
	for _, val := range results {
		if val != "value" {
			t.Errorf("expected value to be 'value', got %v", val)
		}
	}
}
