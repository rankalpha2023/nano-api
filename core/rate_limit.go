package core

import (
	"context"
	"time"

	"golang.org/x/time/rate"
)

// RateLimiter 限流管理器
type RateLimiter struct {
	limiter *rate.Limiter
	minInterval time.Duration
	lastRequest time.Time
}

// NewRateLimiter 创建新的限流管理器
func NewRateLimiter(maxRequestsPerMinute, minIntervalMs int) *RateLimiter {
	// 计算每秒允许的请求数
	r := rate.Limit(float64(maxRequestsPerMinute) / 60.0)
	// 创建令牌桶，桶容量为maxRequestsPerMinute
	limiter := rate.NewLimiter(r, maxRequestsPerMinute)
	
	return &RateLimiter{
		limiter:     limiter,
		minInterval: time.Duration(minIntervalMs) * time.Millisecond,
		lastRequest: time.Now(),
	}
}

// Wait 等待直到可以发送请求
func (r *RateLimiter) Wait() {
	// 等待令牌桶有可用令牌
	r.limiter.Wait(context.Background())
	
	// 确保最小发送时间间隔
	elapsed := time.Since(r.lastRequest)
	if elapsed < r.minInterval {
		time.Sleep(r.minInterval - elapsed)
	}
	
	// 更新最后请求时间
	r.lastRequest = time.Now()
}
