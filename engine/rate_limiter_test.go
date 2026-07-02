package engine_test

import (
	"testing"
	"time"

	"nano-api/engine"
)

func TestRateLimiter(t *testing.T) {
	// 创建限流管理器，每分钟最多10个请求，最小间隔100ms
	limiter := engine.NewRateLimiter(10, 100)

	// 测试10个请求的时间
	start := time.Now()
	for i := 0; i < 10; i++ {
		limiter.Wait()
	}
	elapsed := time.Since(start)

	// 验证时间是否在合理范围内（至少100ms*10=1秒）
	if elapsed < 1*time.Second {
		t.Errorf("Expected at least 1 second, got %v", elapsed)
	}
}

func TestRateLimiter_Accessors(t *testing.T) {
	limiter := engine.NewRateLimiter(30, 250)

	if mi := limiter.MinInterval(); mi != 250*time.Millisecond {
		t.Errorf("MinInterval: expected 250ms, got %v", mi)
	}
	if rpm := limiter.MaxRequestsPerMinute(); rpm != 30 {
		t.Errorf("MaxRequestsPerMinute: expected 30, got %d", rpm)
	}
}

func TestRateLimiter_ZeroValues(t *testing.T) {
	limiter := engine.NewRateLimiter(0, 0)

	if mi := limiter.MinInterval(); mi != 0 {
		t.Errorf("MinInterval with 0 input: expected 0, got %v", mi)
	}
	if rpm := limiter.MaxRequestsPerMinute(); rpm != 0 {
		t.Errorf("MaxRequestsPerMinute with 0 input: expected 0, got %d", rpm)
	}
}
