package core_test

import (
	"testing"
	"time"

	"nano-api/core"
)

func TestRateLimiter(t *testing.T) {
	// 创建限流管理器，每分钟最多10个请求，最小间隔100ms
	limiter := core.NewRateLimiter(10, 100)

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
