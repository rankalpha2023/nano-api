package engine_test

import (
	"testing"
	"time"

	"nano-api/engine"
	"nano-api/types"
)

func TestAccountManager(t *testing.T) {
	// 创建测试配置
	config := &types.Config{
		Servers: []types.ServerConfig{
			{Name: "account1", APIKey: "key1", BaseURL: "http://example.com"},
			{Name: "account2", APIKey: "key2", BaseURL: "http://example.com"},
			{Name: "account3", APIKey: "key3", BaseURL: "http://example.com"},
		},
		RateLimit: types.RateLimitConfig{
			MaxRequestsPerMinute: 60,
			MinIntervalMs:        10,
			RetryIntervalMs:      100,
		},
	}
	
	// 创建帐号管理器
	am := engine.NewAccountManager(config)
	am.Start()
	
	// 测试轮询功能
	accounts := make(map[string]int)
	for i := 0; i < 6; i++ {
		account := am.GetNextAccount()
		if account == nil {
			t.Fatal("Expected account, got nil")
		}
		accounts[account.Name]++
	}
	
	// 验证每个帐号都被使用了2次
	for name, count := range accounts {
		if count != 2 {
			t.Errorf("Expected account %s to be used 2 times, got %d", name, count)
		}
	}
	
	// 测试失败处理
	testAccount := am.GetNextAccount()
	if testAccount == nil {
		t.Fatal("Expected account, got nil")
	}
	
	// 标记帐号失败
	am.MarkAccountFailed(testAccount)
	
	// 验证该帐号不在可用列表中
	for i := 0; i < 5; i++ {
		account := am.GetNextAccount()
		if account == nil {
			t.Fatal("Expected account, got nil")
		}
		if account.Name == testAccount.Name {
			t.Errorf("Expected account %s to be in failed state, but it's available", testAccount.Name)
		}
	}
	
	// 等待重试间隔
	time.Sleep(150 * time.Millisecond)
	
	// 验证帐号是否恢复
	found := false
	for i := 0; i < 5; i++ {
		account := am.GetNextAccount()
		if account == nil {
			t.Fatal("Expected account, got nil")
		}
		if account.Name == testAccount.Name {
			found = true
			break
		}
	}
	
	if !found {
		t.Errorf("Expected account %s to be available after retry interval", testAccount.Name)
	}
}
