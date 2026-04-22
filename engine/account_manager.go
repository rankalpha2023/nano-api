package engine

import (
	"log"
	"sync"
	"time"

	"nano-api/core"
	"nano-api/types"
)

type ProviderState struct {
	Name            string
	accounts        []*types.Account
	available       []*types.Account
	failed          []*types.Account
	models          map[string]string // 该provider自己的model映射: 别名->真实模型名
	RateLimiter     *core.RateLimiter
	retryIntervalMs int
	currentIndex    int
	mutex           sync.Mutex
}

type AccountManager struct {
	providers          map[string]*ProviderState
	modelProviders     map[string][]string // model名称到provider名称列表的映射
	modelProviderIndex map[string]int      // 每个model对应的provider索引
	requestHandler     *core.RequestHandler
}

func NewAccountManager(config *types.Config) *AccountManager {
	providers := make(map[string]*ProviderState)
	modelProviders := make(map[string][]string)
	modelProviderIndex := make(map[string]int)

	defaultRateLimit := config.RateLimit

	for _, provider := range config.Providers {
		rateLimit := provider.RateLimit
		if rateLimit.MaxRequestsPerMinute == 0 && rateLimit.MinIntervalMs == 0 {
			rateLimit = defaultRateLimit
		}

		var accounts []*types.Account
		for _, apiKey := range provider.APIKeyEntries {
			accounts = append(accounts, &types.Account{
				ProviderName: provider.Name,
				BaseURL:      provider.BaseURL,
				APIKey:       apiKey,
				Timeout:      provider.Timeout,
				Proxy:        provider.Proxy,
				Headers:      provider.Headers,
				Status:       types.AccountStatusAvailable,
				LastUsed:     time.Now(),
			})
		}

		providerModels := make(map[string]string)
		for alias, model := range provider.Models {
			providerModels[alias] = model
			modelProviders[alias] = append(modelProviders[alias], provider.Name)
			modelProviders[model] = append(modelProviders[model], provider.Name)
		}

		providers[provider.Name] = &ProviderState{
			Name:            provider.Name,
			accounts:        accounts,
			available:       accounts,
			failed:          []*types.Account{},
			models:          providerModels,
			RateLimiter:     core.NewRateLimiter(rateLimit.MaxRequestsPerMinute, rateLimit.MinIntervalMs),
			retryIntervalMs: rateLimit.RetryIntervalMs,
			currentIndex:    0,
		}
	}

	// 初始化每个model的provider索引
	for model := range modelProviders {
		modelProviderIndex[model] = 0
	}

	return &AccountManager{
		providers:          providers,
		modelProviders:     modelProviders,
		modelProviderIndex: modelProviderIndex,
		requestHandler:     core.NewRequestHandler(),
	}
}

func (am *AccountManager) Start() {
	for name, provider := range am.providers {
		go am.checkFailedAccounts(provider)
		log.Printf("Started account manager for provider: %s", name)
	}
}

func (am *AccountManager) checkFailedAccounts(ps *ProviderState) {
	ticker := time.NewTicker(time.Duration(ps.retryIntervalMs) * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		ps.mutex.Lock()

		for i := len(ps.failed) - 1; i >= 0; i-- {
			account := ps.failed[i]
			if time.Since(account.LastFailed) >= time.Duration(ps.retryIntervalMs)*time.Millisecond {
				account.Status = types.AccountStatusAvailable
				ps.available = append(ps.available, account)
				ps.failed = append(ps.failed[:i], ps.failed[i+1:]...)
			}
		}

		ps.mutex.Unlock()
	}
}

func (am *AccountManager) GetNextAccount(providerName string) *types.Account {
	ps, ok := am.providers[providerName]
	if !ok {
		return nil
	}

	ps.mutex.Lock()
	defer ps.mutex.Unlock()

	if len(ps.available) == 0 {
		return nil
	}

	if ps.currentIndex >= len(ps.available) {
		ps.currentIndex = 0
	}

	account := ps.available[ps.currentIndex]
	ps.currentIndex = (ps.currentIndex + 1) % len(ps.available)

	return account
}

func (am *AccountManager) MarkAccountFailed(account *types.Account) {
	ps, ok := am.providers[account.ProviderName]
	if !ok {
		return
	}

	ps.mutex.Lock()
	defer ps.mutex.Unlock()

	account.Status = types.AccountStatusFailed
	account.LastFailed = time.Now()

	for i, acc := range ps.available {
		if acc == account {
			ps.available = append(ps.available[:i], ps.available[i+1:]...)
			break
		}
	}

	ps.failed = append(ps.failed, account)
}

func (am *AccountManager) MarkAccountAvailable(account *types.Account) {
	ps, ok := am.providers[account.ProviderName]
	if !ok {
		return
	}

	ps.mutex.Lock()
	defer ps.mutex.Unlock()

	account.Status = types.AccountStatusAvailable
	account.LastUsed = time.Now()
	account.RequestCount++
}

func (am *AccountManager) GetNextProviderForModel(modelName string) string {
	providers, ok := am.modelProviders[modelName]
	if !ok || len(providers) == 0 {
		return ""
	}

	startIdx := am.modelProviderIndex[modelName]
	for i := 0; i < len(providers); i++ {
		idx := (startIdx + i) % len(providers)
		providerName := providers[idx]
		ps, ok := am.providers[providerName]
		if !ok {
			continue
		}
		ps.mutex.Lock()
		hasAvailable := len(ps.available) > 0
		ps.mutex.Unlock()
		if hasAvailable {
			am.modelProviderIndex[modelName] = (idx + 1) % len(providers)
			return providerName
		}
	}

	return ""
}

func (am *AccountManager) GetRealModelName(providerName, modelName string) string {
	ps, ok := am.providers[providerName]
	if !ok {
		return modelName
	}

	if realModel, ok := ps.models[modelName]; ok {
		return realModel
	}
	return modelName
}

func (am *AccountManager) SendRequest(req *types.ChatRequest) (*core.RequestHandler, *types.Account, string, error) {
	modelName := req.Model

	providerName := am.GetNextProviderForModel(modelName)
	if providerName == "" {
		log.Printf("No available provider for model: %s", modelName)
		return nil, nil, "", nil
	}

	realModel := am.GetRealModelName(providerName, modelName)
	if realModel != modelName {
		log.Printf("Model mapped in provider %s: %s -> %s", providerName, modelName, realModel)
	}

	ps, ok := am.providers[providerName]
	if !ok {
		return nil, nil, "", nil
	}

	ps.RateLimiter.Wait()

	account := am.GetNextAccount(providerName)
	if account == nil {
		return nil, nil, "", nil
	}

	return am.requestHandler, account, realModel, nil
}
