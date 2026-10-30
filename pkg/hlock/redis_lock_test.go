package hlock_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlock"
)

// newTestRedisContext 返回携带 Redis 连接的测试上下文，环境不可用时跳过测试
func newTestRedisContext(t *testing.T) context.Context {
	t.Helper()
	if testRedisSkip != "" {
		t.Skipf("跳过集成测试: %s", testRedisSkip)
	}
	return testRedisCtx
}

// newTestRedisKey 生成唯一的测试 key
func newTestRedisKey(prefix string) string {
	return fmt.Sprintf("%s_%s", prefix, uuid.NewString())
}

// TestNewRedisLock 测试创建 redis 锁
func TestNewRedisLock(t *testing.T) {
	// 无 redis 上下文时应报错
	_, err := hlock.NewRedisLock(context.Background(), "test_new_redis_lock")
	if err == nil {
		t.Errorf("预期无 redis 上下文时创建锁失败，但实际成功")
	}

	ctx := newTestRedisContext(t)

	// 正常创建
	lock, err := hlock.NewRedisLock(ctx, "test_new_redis_lock")
	if err != nil {
		t.Fatalf("预期创建锁成功，但实际返回错误: %v", err)
	}
	if lock == nil {
		t.Fatalf("预期返回锁实例，但实际返回 nil")
	}

	// 自定义 TTL
	lock, err = hlock.NewRedisLock(ctx, "test_new_redis_lock_ttl", hlock.WithRedisLockTTL(time.Second*2))
	if err != nil {
		t.Fatalf("预期创建锁成功，但实际返回错误: %v", err)
	}
	if lock == nil {
		t.Fatalf("预期返回锁实例，但实际返回 nil")
	}
}

// TestRedisLock 测试 redis 锁的加锁与解锁
func TestRedisLock(t *testing.T) {
	ctx := newTestRedisContext(t)
	key := newTestRedisKey("test_redis_lock")

	lock, err := hlock.NewRedisLock(ctx, key)
	if err != nil {
		t.Fatalf("预期创建锁成功，但实际返回错误: %v", err)
	}

	// 正常加锁
	if err := lock.Lock(ctx); err != nil {
		t.Fatalf("预期加锁成功，但实际返回错误: %v", err)
	}

	// 持锁期间其他实例尝试加锁应失败
	other, err := hlock.NewRedisLock(ctx, key)
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

// TestRedisUnlockWrongValue 测试非持有者解锁不会误删锁
func TestRedisUnlockWrongValue(t *testing.T) {
	ctx := newTestRedisContext(t)
	key := newTestRedisKey("test_redis_unlock_wrong_value")

	lock, err := hlock.NewRedisLock(ctx, key)
	if err != nil {
		t.Fatalf("预期创建锁成功，但实际返回错误: %v", err)
	}
	other, err := hlock.NewRedisLock(ctx, key)
	if err != nil {
		t.Fatalf("预期创建锁成功，但实际返回错误: %v", err)
	}

	// lock 持锁
	if err := lock.Lock(ctx); err != nil {
		t.Fatalf("预期加锁成功，但实际返回错误: %v", err)
	}

	// other 未持锁时解锁不应影响 lock 持有的锁
	if err := other.Unlock(ctx); err != nil {
		t.Fatalf("预期解锁无错误，但实际返回错误: %v", err)
	}
	locked, err := other.TryLock(ctx)
	if err != nil {
		t.Fatalf("预期尝试加锁无错误，但实际返回错误: %v", err)
	}
	if locked {
		t.Errorf("预期非持有者解锁不影响锁，但 other 尝试加锁成功")
	}

	// 持有者解锁后锁释放
	if err := lock.Unlock(ctx); err != nil {
		t.Fatalf("预期解锁成功，但实际返回错误: %v", err)
	}
	locked, err = other.TryLock(ctx)
	if err != nil {
		t.Fatalf("预期尝试加锁无错误，但实际返回错误: %v", err)
	}
	if !locked {
		t.Errorf("预期持有者解锁后尝试加锁成功，但实际失败")
	}
	_ = other.Unlock(ctx)
}

// TestRedisLockTimeout 测试 redis 锁等待期间 ctx 超时
func TestRedisLockTimeout(t *testing.T) {
	ctx := newTestRedisContext(t)
	key := newTestRedisKey("test_redis_lock_timeout")

	lock, err := hlock.NewRedisLock(ctx, key)
	if err != nil {
		t.Fatalf("预期创建锁成功，但实际返回错误: %v", err)
	}
	if err := lock.Lock(ctx); err != nil {
		t.Fatalf("预期加锁成功，但实际返回错误: %v", err)
	}
	defer lock.Unlock(ctx)

	timeoutCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()

	other, err := hlock.NewRedisLock(ctx, key)
	if err != nil {
		t.Fatalf("预期创建锁成功，但实际返回错误: %v", err)
	}

	start := time.Now()
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
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("预期超时及时返回，但耗时 %v", elapsed)
	}
}

// TestRedisLockContextCancel 测试 redis 锁等待期间 ctx 取消
func TestRedisLockContextCancel(t *testing.T) {
	ctx := newTestRedisContext(t)
	key := newTestRedisKey("test_redis_lock_cancel")

	lock, err := hlock.NewRedisLock(ctx, key)
	if err != nil {
		t.Fatalf("预期创建锁成功，但实际返回错误: %v", err)
	}
	if err := lock.Lock(ctx); err != nil {
		t.Fatalf("预期加锁成功，但实际返回错误: %v", err)
	}
	defer lock.Unlock(ctx)

	cancelCtx, cancel := context.WithCancel(ctx)
	other, err := hlock.NewRedisLock(ctx, key)
	if err != nil {
		t.Fatalf("预期创建锁成功，但实际返回错误: %v", err)
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- other.Lock(cancelCtx)
	}()

	// 等待 goroutine 进入等待状态后取消
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err = <-errCh:
		if err == nil {
			t.Errorf("预期取消返回错误，实际返回 nil")
		}
	case <-time.After(5 * time.Second):
		t.Errorf("预期及时取消返回，实际未返回")
	}
}

// TestRedisLockWatchdog 测试 watchdog 续期机制
func TestRedisLockWatchdog(t *testing.T) {
	ctx := newTestRedisContext(t)
	key := newTestRedisKey("test_redis_watchdog")

	lock, err := hlock.NewRedisLock(ctx, key, hlock.WithRedisLockTTL(time.Second))
	if err != nil {
		t.Fatalf("预期创建锁成功，但实际返回错误: %v", err)
	}
	if err := lock.Lock(ctx); err != nil {
		t.Fatalf("预期加锁成功，但实际返回错误: %v", err)
	}

	// 等待超过一个 TTL 周期，watchdog 应完成续期
	time.Sleep(1500 * time.Millisecond)

	other, err := hlock.NewRedisLock(ctx, key)
	if err != nil {
		t.Fatalf("预期创建锁成功，但实际返回错误: %v", err)
	}
	locked, err := other.TryLock(ctx)
	if err != nil {
		t.Fatalf("预期尝试加锁无错误，但实际返回错误: %v", err)
	}
	if locked {
		_ = other.Unlock(ctx)
		t.Errorf("预期 watchdog 续期后锁仍被持有，但 other 尝试加锁成功")
	}

	if err := lock.Unlock(ctx); err != nil {
		t.Fatalf("预期解锁成功，但实际返回错误: %v", err)
	}
}

// TestRedisLockTimeoutWatchdog 测试超时选项下 watchdog 仍用原始 ctx 续期：
// 修复过"Lock 派生超时 ctx 后 defer cancel 在返回时取消，导致 watchdog 立即失效"的问题。
func TestRedisLockTimeoutWatchdog(t *testing.T) {
	ctx := newTestRedisContext(t)
	key := newTestRedisKey("test_redis_timeout_watchdog")

	// timeout 300ms + TTL 300ms，持锁远超 timeout 时长，验证续期不失效
	lock, err := hlock.NewRedisLock(ctx, key,
		hlock.WithRedisLockTimeout(300*time.Millisecond),
		hlock.WithRedisLockTTL(300*time.Millisecond))
	if err != nil {
		t.Fatalf("预期创建锁成功，但实际返回错误: %v", err)
	}
	if err := lock.Lock(ctx); err != nil {
		t.Fatalf("预期加锁成功，但实际返回错误: %v", err)
	}

	// 等待远超 timeout/TTL 的时长，若 watchdog 失效锁早已过期
	time.Sleep(1500 * time.Millisecond)

	other, err := hlock.NewRedisLock(ctx, key)
	if err != nil {
		t.Fatalf("预期创建锁成功，但实际返回错误: %v", err)
	}
	locked, err := other.TryLock(ctx)
	if err != nil {
		t.Fatalf("预期尝试加锁无错误，但实际返回错误: %v", err)
	}
	if locked {
		_ = other.Unlock(ctx)
		t.Errorf("预期 watchdog 持续续期后锁仍被持有，但 other 尝试加锁成功")
	}

	if err := lock.Unlock(ctx); err != nil {
		t.Fatalf("预期解锁成功，但实际返回错误: %v", err)
	}
}

// TestRedisLockTimeoutOption 测试 WithRedisLockTimeout 选项：Lock 等待超时
func TestRedisLockTimeoutOption(t *testing.T) {
	ctx := newTestRedisContext(t)
	key := newTestRedisKey("test_redis_lock_timeout_option")

	lock, err := hlock.NewRedisLock(ctx, key, hlock.WithRedisLockTimeout(100*time.Millisecond))
	if err != nil {
		t.Fatalf("预期创建锁成功，但实际返回错误: %v", err)
	}
	if err := lock.Lock(ctx); err != nil {
		t.Fatalf("预期加锁成功，但实际返回错误: %v", err)
	}
	defer lock.Unlock(ctx)

	errCh := make(chan error, 1)
	go func() {
		other, err := hlock.NewRedisLock(ctx, key, hlock.WithRedisLockTimeout(100*time.Millisecond))
		if err != nil {
			errCh <- err
			return
		}
		errCh <- other.Lock(ctx)
	}()

	select {
	case err := <-errCh:
		if err == nil {
			t.Errorf("预期超时返回错误，实际返回 nil")
		}
	case <-time.After(5 * time.Second):
		t.Errorf("预期及时超时返回，实际未返回")
	}
}

// TestWithRedisLockTimeoutOption 测试组合锁本地锁超时与 redis 锁超时一致
func TestWithRedisLockTimeoutOption(t *testing.T) {
	ctx := newTestRedisContext(t)
	key := newTestRedisKey("test_with_redis_lock_timeout_option")

	// goroutine 1 持有锁（本地+redis），带超时选项
	holding := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_, _ = hlock.WithRedis[int](ctx, key, func(ctx context.Context) (int, error) {
			close(holding)
			<-release
			return 0, nil
		}, hlock.WithRedisLockTimeout(100*time.Millisecond))
	}()
	<-holding

	// goroutine 2 带相同超时选项，本地锁等待应在超时后失败
	start := time.Now()
	_, err := hlock.WithRedis[int](ctx, key, func(ctx context.Context) (int, error) {
		return 1, nil
	}, hlock.WithRedisLockTimeout(100*time.Millisecond))
	if err == nil {
		t.Errorf("预期本地锁超时返回错误，实际返回 nil")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("预期按时超时，但耗时 %v", elapsed)
	}

	close(release)
}

// TestRedisLockTTLExpire 测试 TTL 过期后锁自动释放
func TestRedisLockTTLExpire(t *testing.T) {
	ctx := newTestRedisContext(t)
	key := newTestRedisKey("test_redis_ttl_expire")

	lock, err := hlock.NewRedisLock(ctx, key, hlock.WithRedisLockTTL(300*time.Millisecond))
	if err != nil {
		t.Fatalf("预期创建锁成功，但实际返回错误: %v", err)
	}

	// TryLock 不启动 watchdog，TTL 到期后锁应自动释放
	locked, err := lock.TryLock(ctx)
	if err != nil {
		t.Fatalf("预期尝试加锁无错误，但实际返回错误: %v", err)
	}
	if !locked {
		t.Fatalf("预期尝试加锁成功，但实际失败")
	}

	time.Sleep(500 * time.Millisecond)

	other, err := hlock.NewRedisLock(ctx, key)
	if err != nil {
		t.Fatalf("预期创建锁成功，但实际返回错误: %v", err)
	}
	locked, err = other.TryLock(ctx)
	if err != nil {
		t.Fatalf("预期尝试加锁无错误，但实际返回错误: %v", err)
	}
	if !locked {
		t.Errorf("预期 TTL 过期后锁已释放，但尝试加锁失败")
	}
	_ = other.Unlock(ctx)
}

// TestWithRedis 测试 WithRedis 加锁执行
func TestWithRedis(t *testing.T) {
	ctx := newTestRedisContext(t)
	key := newTestRedisKey("test_with_redis")

	// 测试happy path
	val, err := hlock.WithRedis[int](ctx, key, func(ctx context.Context) (int, error) {
		return 42, nil
	})
	if err != nil {
		t.Fatalf("预期操作成功，但实际返回错误: %v", err)
	}
	if val != 42 {
		t.Errorf("预期返回值为42，但实际返回%v", val)
	}

	// 测试fun失败
	val, err = hlock.WithRedis[int](ctx, key, func(ctx context.Context) (int, error) {
		return 0, errors.New("function failed")
	})
	if err == nil {
		t.Errorf("预期fun失败，但实际未返回错误")
	}
}

// TestWithRedisGetOrPopulate 测试 WithRedisGetOrPopulate
func TestWithRedisGetOrPopulate(t *testing.T) {
	ctx := newTestRedisContext(t)
	key := newTestRedisKey("test_with_redis_get_or_populate")

	// 测试happy path（get 未命中走 populate）
	val, err := hlock.WithRedisGetOrPopulate[int](ctx, key,
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
	val, err = hlock.WithRedisGetOrPopulate[int](ctx, key,
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
	val, err = hlock.WithRedisGetOrPopulate[int](ctx, key,
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

// TestWithRedisTry 测试 WithRedisTry
func TestWithRedisTry(t *testing.T) {
	ctx := newTestRedisContext(t)
	key := newTestRedisKey("test_with_redis_try")

	// 测试happy path
	val, err := hlock.WithRedisTry[int](ctx, key, func(ctx context.Context) (int, error) {
		return 42, nil
	})
	if err != nil {
		t.Fatalf("预期操作成功，但实际返回错误: %v", err)
	}
	if val != 42 {
		t.Errorf("预期返回值为42，但实际返回%v", val)
	}

	// 持锁期间尝试加锁应失败
	lock, err := hlock.NewRedisLock(ctx, key)
	if err != nil {
		t.Fatalf("预期创建锁成功，但实际返回错误: %v", err)
	}
	if err := lock.Lock(ctx); err != nil {
		t.Fatalf("预期加锁成功，但实际返回错误: %v", err)
	}
	defer lock.Unlock(ctx)

	val, err = hlock.WithRedisTry[int](ctx, key, func(ctx context.Context) (int, error) {
		return 0, errors.New("should not be called")
	})
	if err == nil {
		t.Errorf("预期持锁期间尝试加锁失败，但实际未返回错误")
	}

	// 持锁释放后 WithRedisTry 可再次成功
	_ = lock.Unlock(ctx)
	val, err = hlock.WithRedisTry[int](ctx, key, func(ctx context.Context) (int, error) {
		return 43, nil
	})
	if err != nil {
		t.Fatalf("预期锁释放后尝试加锁成功，但实际返回错误: %v", err)
	}
	if val != 43 {
		t.Errorf("预期返回值为43，但实际返回%v", val)
	}
}

// TestWithRedisConcurrent 测试多 goroutine 并发 WithRedis 加锁互斥
func TestWithRedisConcurrent(t *testing.T) {
	ctx := newTestRedisContext(t)
	key := newTestRedisKey("test_with_redis_concurrent")

	const goroutines = 10
	counter := 0
	errs := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			_, err := hlock.WithRedis[int](ctx, key, func(ctx context.Context) (int, error) {
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

	if counter != goroutines {
		t.Errorf("预期 counter 为 %d，但实际为 %d", goroutines, counter)
	}
}
