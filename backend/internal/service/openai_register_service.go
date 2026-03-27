package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

const (
	OpenAIRegisterScopeAllOpenAIOAuth = "all_openai_oauth"
	OpenAIRegisterScopeManagedOnly    = "managed_only"

	openAIRegisterManagedByTag      = "openai_register"
	openAIRegisterUsageURL          = "https://chatgpt.com/backend-api/wham/usage"
	openAIRegisterDefaultUserAgent  = "codex_cli_rs/0.104.0"
	openAIRegisterLoopInterval      = 30 * time.Second
	openAIRegisterCheckPageSize     = 100
	openAIRegisterStatusOK          = "ok"
	openAIRegisterStatusInvalid     = "invalid"
	openAIRegisterStatusHighUsage   = "high_usage"
	openAIRegisterStatusUncertain   = "uncertain"
	openAIRegisterStatusSkipped     = "skipped"
	openAIRegisterActionNone        = "none"
	openAIRegisterActionSetInactive = "set_inactive"
	openAIAccountStatusInactive     = "inactive"
)

var ErrOpenAIRegisterCheckRunning = infraerrors.Conflict(
	"OPENAI_REGISTER_CHECK_RUNNING",
	"openai register check is already running",
)

type openAIRegisterClientFactory func(opts httpclient.Options) (*http.Client, error)

// OpenAIRegisterSettings stores DB-backed configuration for the OpenAI register module.
// 当前先落地“检测线程”，注册线程后续接入同一模块。
type OpenAIRegisterSettings struct {
	AutoCheckEnabled     bool   `json:"auto_check_enabled"`
	CheckIntervalSeconds int    `json:"check_interval_seconds"`
	RequestTimeoutSecs   int    `json:"request_timeout_seconds"`
	UsageThresholdPct    int    `json:"usage_threshold_percent"`
	InactiveOnInvalid    bool   `json:"inactive_on_invalid"`
	Scope                string `json:"scope"`
	CheckProxyID         *int64 `json:"check_proxy_id,omitempty"`
}

func DefaultOpenAIRegisterSettings() *OpenAIRegisterSettings {
	return &OpenAIRegisterSettings{
		AutoCheckEnabled:     false,
		CheckIntervalSeconds: 900,
		RequestTimeoutSecs:   20,
		UsageThresholdPct:    90,
		InactiveOnInvalid:    true,
		Scope:                OpenAIRegisterScopeAllOpenAIOAuth,
	}
}

// OpenAIRegisterRuntime is in-memory runtime state for the current process.
// 这是运行态快照，不做跨实例持久化。
type OpenAIRegisterRuntime struct {
	Running               bool                        `json:"running"`
	LastStartedAt         *time.Time                  `json:"last_started_at,omitempty"`
	LastFinishedAt        *time.Time                  `json:"last_finished_at,omitempty"`
	LastDurationMS        int64                       `json:"last_duration_ms"`
	LastTrigger           string                      `json:"last_trigger,omitempty"`
	LastError             string                      `json:"last_error,omitempty"`
	LastSummary           *OpenAIRegisterSummary      `json:"last_summary,omitempty"`
	CurrentTotal          int                         `json:"current_total"`
	CurrentCompleted      int                         `json:"current_completed"`
	CurrentAccountID      int64                       `json:"current_account_id,omitempty"`
	CurrentAccountName    string                      `json:"current_account_name,omitempty"`
	CurrentAccountStarted *time.Time                  `json:"current_account_started_at,omitempty"`
	RecentResults         []OpenAIRegisterCheckResult `json:"recent_results,omitempty"`
}

type OpenAIRegisterSummary struct {
	Trigger     string    `json:"trigger"`
	Scope       string    `json:"scope"`
	Total       int       `json:"total"`
	Checked     int       `json:"checked"`
	OK          int       `json:"ok"`
	Invalid     int       `json:"invalid"`
	HighUsage   int       `json:"high_usage"`
	Uncertain   int       `json:"uncertain"`
	Skipped     int       `json:"skipped"`
	Inactivated int       `json:"inactivated"`
	StartedAt   time.Time `json:"started_at"`
	FinishedAt  time.Time `json:"finished_at"`
	DurationMS  int64     `json:"duration_ms"`
}

type OpenAIRegisterCheckResult struct {
	AccountID   int64  `json:"account_id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	UsedPercent *int   `json:"used_percent,omitempty"`
	Detail      string `json:"detail"`
	Action      string `json:"action"`
}

type OpenAIRegisterCheckRunResult struct {
	Summary OpenAIRegisterSummary       `json:"summary"`
	Results []OpenAIRegisterCheckResult `json:"results"`
}

type OpenAIRegisterCheckStartResult struct {
	Accepted bool `json:"accepted"`
	Running  bool `json:"running"`
}

type OpenAIRegisterRunCheckInput struct {
	AccountIDs []int64 `json:"account_ids"`
	Trigger    string  `json:"-"`
}

// OpenAIRegisterService currently owns the account status checking workflow.
// 注册线程后续会复用同一配置和运行时框架接入。
type OpenAIRegisterService struct {
	accountRepo AccountRepository
	settingRepo SettingRepository
	proxyRepo   ProxyRepository

	clientFactory openAIRegisterClientFactory
	usageURL      string
	now           func() time.Time

	stopCh chan struct{}
	wg     sync.WaitGroup

	mu            sync.Mutex
	runtime       OpenAIRegisterRuntime
	lastAutoRunAt time.Time
}

func NewOpenAIRegisterService(accountRepo AccountRepository, settingRepo SettingRepository) *OpenAIRegisterService {
	return &OpenAIRegisterService{
		accountRepo:   accountRepo,
		settingRepo:   settingRepo,
		clientFactory: httpclient.GetClient,
		usageURL:      openAIRegisterUsageURL,
		now:           time.Now,
		stopCh:        make(chan struct{}),
		runtime: OpenAIRegisterRuntime{
			LastSummary: nil,
		},
	}
}

func ProvideOpenAIRegisterService(accountRepo AccountRepository, settingRepo SettingRepository, proxyRepo ProxyRepository) *OpenAIRegisterService {
	svc := NewOpenAIRegisterService(accountRepo, settingRepo)
	svc.proxyRepo = proxyRepo
	svc.Start()
	return svc
}

func (s *OpenAIRegisterService) Start() {
	if s == nil {
		return
	}
	s.wg.Add(1)
	go s.loop()
}

func (s *OpenAIRegisterService) Stop() {
	if s == nil {
		return
	}
	select {
	case <-s.stopCh:
		return
	default:
		close(s.stopCh)
	}
	s.wg.Wait()
}

func (s *OpenAIRegisterService) loop() {
	defer s.wg.Done()

	ticker := time.NewTicker(openAIRegisterLoopInterval)
	defer ticker.Stop()

	s.runAutoCheckIfDue()

	for {
		select {
		case <-ticker.C:
			s.runAutoCheckIfDue()
		case <-s.stopCh:
			return
		}
	}
}

func (s *OpenAIRegisterService) runAutoCheckIfDue() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	settings, err := s.GetSettings(ctx)
	cancel()
	if err != nil {
		slog.Warn("openai_register.auto_check_settings_failed", "error", err)
		return
	}
	if !settings.AutoCheckEnabled {
		return
	}

	interval := time.Duration(settings.CheckIntervalSeconds) * time.Second
	now := s.now()

	s.mu.Lock()
	lastRunAt := s.lastAutoRunAt
	running := s.runtime.Running
	s.mu.Unlock()

	if running {
		return
	}
	if !lastRunAt.IsZero() && now.Sub(lastRunAt) < interval {
		return
	}

	if _, err := s.triggerCheckWithSettings(context.Background(), &OpenAIRegisterRunCheckInput{
		Trigger: "auto",
	}, settings); err != nil {
		if errors.Is(err, ErrOpenAIRegisterCheckRunning) {
			return
		}
		slog.Warn("openai_register.auto_check_failed", "error", err)
		return
	}

	s.mu.Lock()
	s.lastAutoRunAt = now
	s.mu.Unlock()
}

func (s *OpenAIRegisterService) GetSettings(ctx context.Context) (*OpenAIRegisterSettings, error) {
	value, err := s.settingRepo.GetValue(ctx, SettingKeyOpenAIRegisterSettings)
	if err != nil {
		if errors.Is(err, ErrSettingNotFound) {
			return DefaultOpenAIRegisterSettings(), nil
		}
		return nil, fmt.Errorf("get openai register settings: %w", err)
	}
	if strings.TrimSpace(value) == "" {
		return DefaultOpenAIRegisterSettings(), nil
	}

	var settings OpenAIRegisterSettings
	if err := json.Unmarshal([]byte(value), &settings); err != nil {
		return DefaultOpenAIRegisterSettings(), nil
	}

	normalizeOpenAIRegisterSettings(&settings)
	return &settings, nil
}

func (s *OpenAIRegisterService) UpdateSettings(ctx context.Context, settings *OpenAIRegisterSettings) (*OpenAIRegisterSettings, error) {
	if settings == nil {
		return nil, infraerrors.BadRequest("OPENAI_REGISTER_SETTINGS_REQUIRED", "settings is required")
	}

	normalized := *settings
	normalizeOpenAIRegisterSettings(&normalized)

	raw, err := json.Marshal(&normalized)
	if err != nil {
		return nil, fmt.Errorf("marshal openai register settings: %w", err)
	}
	if err := s.settingRepo.Set(ctx, SettingKeyOpenAIRegisterSettings, string(raw)); err != nil {
		return nil, fmt.Errorf("save openai register settings: %w", err)
	}

	return &normalized, nil
}

func (s *OpenAIRegisterService) GetRuntime() OpenAIRegisterRuntime {
	s.mu.Lock()
	defer s.mu.Unlock()

	runtime := s.runtime
	if s.runtime.LastSummary != nil {
		summary := *s.runtime.LastSummary
		runtime.LastSummary = &summary
	}
	if s.runtime.CurrentAccountStarted != nil {
		startedAt := *s.runtime.CurrentAccountStarted
		runtime.CurrentAccountStarted = &startedAt
	}
	if len(s.runtime.RecentResults) > 0 {
		runtime.RecentResults = append([]OpenAIRegisterCheckResult(nil), s.runtime.RecentResults...)
	}
	return runtime
}

func (s *OpenAIRegisterService) TriggerCheck(ctx context.Context, input *OpenAIRegisterRunCheckInput) (*OpenAIRegisterCheckStartResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	settings, err := s.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	return s.triggerCheckWithSettings(ctx, input, settings)
}

func (s *OpenAIRegisterService) RunCheck(ctx context.Context, input *OpenAIRegisterRunCheckInput) (*OpenAIRegisterCheckRunResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	input = cloneOpenAIRegisterRunCheckInput(input)

	settings, err := s.GetSettings(ctx)
	if err != nil {
		return nil, err
	}

	startedAt := s.now().UTC()
	trigger := normalizeOpenAIRegisterTrigger(input.Trigger)
	if err := s.beginCheck(startedAt, trigger); err != nil {
		return nil, err
	}

	runCtx := detachOpenAIRegisterRunContext(ctx)
	result, runErr := s.runCheck(runCtx, input, settings, startedAt, trigger)
	s.finishCheck(startedAt, result, runErr)
	return result, runErr
}

func (s *OpenAIRegisterService) triggerCheckWithSettings(
	ctx context.Context,
	input *OpenAIRegisterRunCheckInput,
	settings *OpenAIRegisterSettings,
) (*OpenAIRegisterCheckStartResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if settings == nil {
		return nil, infraerrors.InternalServer("OPENAI_REGISTER_SETTINGS_NOT_INITIALIZED", "openai register settings not initialized")
	}

	input = cloneOpenAIRegisterRunCheckInput(input)
	startedAt := s.now().UTC()
	trigger := normalizeOpenAIRegisterTrigger(input.Trigger)
	if err := s.beginCheck(startedAt, trigger); err != nil {
		return nil, err
	}

	runCtx := detachOpenAIRegisterRunContext(ctx)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		result, runErr := s.runCheck(runCtx, input, settings, startedAt, trigger)
		s.finishCheck(startedAt, result, runErr)
	}()

	return &OpenAIRegisterCheckStartResult{
		Accepted: true,
		Running:  true,
	}, nil
}

func detachOpenAIRegisterRunContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return context.WithoutCancel(ctx)
}

func cloneOpenAIRegisterRunCheckInput(input *OpenAIRegisterRunCheckInput) *OpenAIRegisterRunCheckInput {
	if input == nil {
		return &OpenAIRegisterRunCheckInput{}
	}

	cloned := *input
	if len(input.AccountIDs) > 0 {
		cloned.AccountIDs = append([]int64(nil), input.AccountIDs...)
	}
	return &cloned
}

func normalizeOpenAIRegisterTrigger(trigger string) string {
	normalized := strings.TrimSpace(trigger)
	if normalized == "" {
		return "manual"
	}
	return normalized
}

func (s *OpenAIRegisterService) beginCheck(startedAt time.Time, trigger string) error {
	if s == nil {
		return infraerrors.InternalServer("OPENAI_REGISTER_SERVICE_REQUIRED", "openai register service is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.runtime.Running {
		return ErrOpenAIRegisterCheckRunning
	}
	s.runtime.Running = true
	s.runtime.LastStartedAt = &startedAt
	s.runtime.LastTrigger = trigger
	s.runtime.LastError = ""
	s.runtime.CurrentTotal = 0
	s.runtime.CurrentCompleted = 0
	s.runtime.CurrentAccountID = 0
	s.runtime.CurrentAccountName = ""
	s.runtime.CurrentAccountStarted = nil
	s.runtime.RecentResults = nil
	return nil
}

func (s *OpenAIRegisterService) finishCheck(
	startedAt time.Time,
	result *OpenAIRegisterCheckRunResult,
	runErr error,
) {
	if s == nil {
		return
	}

	finishedAt := s.now().UTC()
	durationMS := finishedAt.Sub(startedAt).Milliseconds()

	if result != nil {
		result.Summary.FinishedAt = finishedAt
		result.Summary.DurationMS = durationMS
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.runtime.Running = false
	s.runtime.LastFinishedAt = &finishedAt
	s.runtime.LastDurationMS = durationMS
	if runErr != nil {
		s.runtime.LastError = runErr.Error()
		return
	}
	s.runtime.LastError = ""
	if result != nil {
		summary := result.Summary
		s.runtime.LastSummary = &summary
	}
}

func (s *OpenAIRegisterService) runCheck(
	ctx context.Context,
	input *OpenAIRegisterRunCheckInput,
	settings *OpenAIRegisterSettings,
	startedAt time.Time,
	trigger string,
) (*OpenAIRegisterCheckRunResult, error) {
	scope := settings.Scope
	if len(input.AccountIDs) > 0 {
		scope = "selected_accounts"
	}

	accounts, err := s.loadCheckAccounts(ctx, input.AccountIDs, settings.Scope)
	if err != nil {
		return nil, err
	}
	checkProxyURL, err := s.resolveOpenAIRegisterCheckProxyURL(ctx, settings.CheckProxyID)
	if err != nil {
		return nil, err
	}

	runResult := &OpenAIRegisterCheckRunResult{
		Summary: OpenAIRegisterSummary{
			Trigger:   trigger,
			Scope:     scope,
			Total:     len(accounts),
			StartedAt: startedAt,
		},
		Results: make([]OpenAIRegisterCheckResult, 0, len(accounts)),
	}
	s.resetRuntimeProgress(accounts)

	for i := range accounts {
		account := &accounts[i]
		s.markRuntimeAccountChecking(account)
		checkResult := s.inspectAccount(ctx, settings, account, checkProxyURL)
		runResult.Results = append(runResult.Results, checkResult)
		s.markRuntimeAccountChecked(checkResult)

		switch checkResult.Status {
		case openAIRegisterStatusOK:
			runResult.Summary.Checked++
			runResult.Summary.OK++
		case openAIRegisterStatusInvalid:
			runResult.Summary.Checked++
			runResult.Summary.Invalid++
			if checkResult.Action == openAIRegisterActionSetInactive {
				runResult.Summary.Inactivated++
			}
		case openAIRegisterStatusHighUsage:
			runResult.Summary.Checked++
			runResult.Summary.HighUsage++
		case openAIRegisterStatusUncertain:
			runResult.Summary.Checked++
			runResult.Summary.Uncertain++
		default:
			runResult.Summary.Skipped++
		}
	}

	return runResult, nil
}

func (s *OpenAIRegisterService) loadCheckAccounts(ctx context.Context, accountIDs []int64, scope string) ([]Account, error) {
	if len(accountIDs) > 0 {
		accounts, err := s.accountRepo.GetByIDs(ctx, accountIDs)
		if err != nil {
			return nil, fmt.Errorf("get accounts by ids: %w", err)
		}

		result := make([]Account, 0, len(accounts))
		for _, account := range accounts {
			if account == nil {
				continue
			}
			result = append(result, *account)
		}
		return result, nil
	}

	page := 1
	result := make([]Account, 0, openAIRegisterCheckPageSize)
	for {
		accounts, pageInfo, err := s.accountRepo.ListWithFilters(
			ctx,
			pagination.PaginationParams{Page: page, PageSize: openAIRegisterCheckPageSize},
			PlatformOpenAI,
			AccountTypeOAuth,
			"",
			"",
			0,
			"",
		)
		if err != nil {
			return nil, fmt.Errorf("list openai oauth accounts: %w", err)
		}

		for i := range accounts {
			if scope == OpenAIRegisterScopeManagedOnly && !isManagedByOpenAIRegister(accounts[i].Extra) {
				continue
			}
			result = append(result, accounts[i])
		}

		if pageInfo == nil || page >= pageInfo.Pages || len(accounts) == 0 {
			break
		}
		page++
	}
	return result, nil
}

func (s *OpenAIRegisterService) inspectAccount(
	ctx context.Context,
	settings *OpenAIRegisterSettings,
	account *Account,
	checkProxyURL string,
) OpenAIRegisterCheckResult {
	result := OpenAIRegisterCheckResult{
		AccountID: account.ID,
		Name:      account.Name,
		Status:    openAIRegisterStatusSkipped,
		Action:    openAIRegisterActionNone,
	}

	if account == nil {
		result.Detail = "账号不存在"
		return result
	}
	if account.Platform != PlatformOpenAI || account.Type != AccountTypeOAuth {
		result.Detail = "仅支持 OpenAI OAuth 账号"
		return result
	}

	accessToken := strings.TrimSpace(account.GetCredential("access_token"))
	if accessToken == "" {
		result.Status = openAIRegisterStatusInvalid
		result.Detail = "缺少 access_token"
		return s.applyCheckResult(ctx, account, result, settings)
	}

	chatgptAccountID := strings.TrimSpace(account.GetChatGPTAccountID())
	if chatgptAccountID == "" {
		result.Status = openAIRegisterStatusInvalid
		result.Detail = "缺少 chatgpt_account_id"
		return s.applyCheckResult(ctx, account, result, settings)
	}

	client, err := s.clientFactory(httpclient.Options{
		ProxyURL:              openAIRegisterCheckProxyURL(account, checkProxyURL),
		Timeout:               time.Duration(settings.RequestTimeoutSecs) * time.Second,
		ResponseHeaderTimeout: time.Duration(settings.RequestTimeoutSecs) * time.Second,
	})
	if err != nil {
		result.Status = openAIRegisterStatusUncertain
		result.Detail = "创建检测客户端失败: " + err.Error()
		return s.applyCheckResult(ctx, account, result, settings)
	}

	reqCtx, cancel := context.WithTimeout(ctx, time.Duration(settings.RequestTimeoutSecs)*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, s.usageURL, nil)
	if err != nil {
		result.Status = openAIRegisterStatusUncertain
		result.Detail = "创建检测请求失败: " + err.Error()
		return s.applyCheckResult(ctx, account, result, settings)
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Chatgpt-Account-Id", chatgptAccountID)
	req.Header.Set("User-Agent", openAIRegisterUserAgent(account))

	resp, err := client.Do(req)
	if err != nil {
		result.Status = openAIRegisterStatusUncertain
		result.Detail = "额度查询异常: " + truncateOpenAIRegisterDetail(err.Error(), 240)
		return s.applyCheckResult(ctx, account, result, settings)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		result.Status = openAIRegisterStatusInvalid
		result.Detail = fmt.Sprintf("额度接口返回 %d", resp.StatusCode)
		return s.applyCheckResult(ctx, account, result, settings)
	case http.StatusOK:
		// keep going
	default:
		result.Status = openAIRegisterStatusUncertain
		result.Detail = fmt.Sprintf("额度接口返回 %d", resp.StatusCode)
		return s.applyCheckResult(ctx, account, result, settings)
	}

	usedPercent, ok := extractOpenAIRegisterUsedPercent(body)
	if !ok {
		result.Status = openAIRegisterStatusUncertain
		result.Detail = "额度结果缺少 used_percent"
		return s.applyCheckResult(ctx, account, result, settings)
	}
	result.UsedPercent = &usedPercent

	if usedPercent >= settings.UsageThresholdPct {
		result.Status = openAIRegisterStatusHighUsage
		result.Detail = fmt.Sprintf("已用比例 %d%% >= %d%%", usedPercent, settings.UsageThresholdPct)
		return s.applyCheckResult(ctx, account, result, settings)
	}

	result.Status = openAIRegisterStatusOK
	result.Detail = fmt.Sprintf("已用比例 %d%%", usedPercent)
	return s.applyCheckResult(ctx, account, result, settings)
}

func (s *OpenAIRegisterService) applyCheckResult(
	ctx context.Context,
	account *Account,
	result OpenAIRegisterCheckResult,
	settings *OpenAIRegisterSettings,
) OpenAIRegisterCheckResult {
	if s == nil || account == nil {
		return result
	}

	checkedAt := s.now().UTC().Format(time.RFC3339)
	extraUpdates := map[string]any{
		"openai_register_check_status":       result.Status,
		"openai_register_check_detail":       result.Detail,
		"openai_register_check_checked_at":   checkedAt,
		"openai_register_check_action":       result.Action,
		"openai_register_check_used_percent": nil,
	}
	if result.UsedPercent != nil {
		extraUpdates["openai_register_check_used_percent"] = float64(*result.UsedPercent)
	}

	if result.Status == openAIRegisterStatusInvalid && settings.InactiveOnInvalid {
		updatedAccount := *account
		updatedAccount.ErrorMessage = result.Detail
		if updatedAccount.Status != openAIAccountStatusInactive {
			updatedAccount.Status = openAIAccountStatusInactive
			result.Action = openAIRegisterActionSetInactive
			extraUpdates["openai_register_check_action"] = result.Action
		}
		if updatedAccount.Extra == nil {
			updatedAccount.Extra = make(map[string]any, len(extraUpdates))
		}
		for key, value := range extraUpdates {
			updatedAccount.Extra[key] = value
		}
		if err := s.accountRepo.Update(ctx, &updatedAccount); err != nil {
			slog.Warn("openai_register.persist_invalid_failed", "account_id", account.ID, "error", err)
		}
		return result
	}

	if err := s.accountRepo.UpdateExtra(ctx, account.ID, extraUpdates); err != nil {
		slog.Warn("openai_register.persist_check_extra_failed", "account_id", account.ID, "error", err)
	}
	return result
}

func normalizeOpenAIRegisterSettings(settings *OpenAIRegisterSettings) {
	if settings == nil {
		return
	}
	if settings.CheckProxyID != nil && *settings.CheckProxyID <= 0 {
		settings.CheckProxyID = nil
	}
	if settings.CheckIntervalSeconds < 60 {
		settings.CheckIntervalSeconds = 60
	}
	if settings.CheckIntervalSeconds > 86400 {
		settings.CheckIntervalSeconds = 86400
	}
	if settings.RequestTimeoutSecs < 5 {
		settings.RequestTimeoutSecs = 5
	}
	if settings.RequestTimeoutSecs > 120 {
		settings.RequestTimeoutSecs = 120
	}
	if settings.UsageThresholdPct < 1 {
		settings.UsageThresholdPct = 1
	}
	if settings.UsageThresholdPct > 100 {
		settings.UsageThresholdPct = 100
	}
	switch strings.TrimSpace(settings.Scope) {
	case OpenAIRegisterScopeManagedOnly:
		settings.Scope = OpenAIRegisterScopeManagedOnly
	default:
		settings.Scope = OpenAIRegisterScopeAllOpenAIOAuth
	}
}

func (s *OpenAIRegisterService) resetRuntimeProgress(accounts []Account) {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.runtime.CurrentTotal = len(accounts)
	s.runtime.CurrentCompleted = 0
	s.runtime.CurrentAccountID = 0
	s.runtime.CurrentAccountName = ""
	s.runtime.CurrentAccountStarted = nil
	s.runtime.RecentResults = make([]OpenAIRegisterCheckResult, 0, len(accounts))
}

func (s *OpenAIRegisterService) markRuntimeAccountChecking(account *Account) {
	if s == nil || account == nil {
		return
	}

	startedAt := s.now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()

	s.runtime.CurrentAccountID = account.ID
	s.runtime.CurrentAccountName = account.Name
	s.runtime.CurrentAccountStarted = &startedAt
}

func (s *OpenAIRegisterService) markRuntimeAccountChecked(result OpenAIRegisterCheckResult) {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.runtime.RecentResults = append(s.runtime.RecentResults, result)
	s.runtime.CurrentCompleted = len(s.runtime.RecentResults)
	s.runtime.CurrentAccountID = 0
	s.runtime.CurrentAccountName = ""
	s.runtime.CurrentAccountStarted = nil
}

func isManagedByOpenAIRegister(extra map[string]any) bool {
	if len(extra) == 0 {
		return false
	}
	value, _ := extra["managed_by"].(string)
	return strings.EqualFold(strings.TrimSpace(value), openAIRegisterManagedByTag)
}

func (s *OpenAIRegisterService) resolveOpenAIRegisterCheckProxyURL(ctx context.Context, proxyID *int64) (string, error) {
	if proxyID == nil {
		return "", nil
	}
	if s == nil || s.proxyRepo == nil {
		return "", infraerrors.InternalServer("OPENAI_REGISTER_PROXY_REPO_REQUIRED", "proxy repository is required")
	}

	proxy, err := s.proxyRepo.GetByID(ctx, *proxyID)
	if err != nil {
		if errors.Is(err, ErrProxyNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("get check proxy: %w", err)
	}
	if proxy == nil {
		return "", nil
	}
	return proxy.URL(), nil
}

func openAIRegisterCheckProxyURL(account *Account, configuredProxyURL string) string {
	if trimmed := strings.TrimSpace(configuredProxyURL); trimmed != "" {
		return trimmed
	}
	return openAIRegisterProxyURL(account)
}

func openAIRegisterProxyURL(account *Account) string {
	if account == nil || account.Proxy == nil {
		return ""
	}
	return account.Proxy.URL()
}

func openAIRegisterUserAgent(account *Account) string {
	if account == nil {
		return openAIRegisterDefaultUserAgent
	}
	userAgent := strings.TrimSpace(account.GetOpenAIUserAgent())
	if userAgent == "" {
		return openAIRegisterDefaultUserAgent
	}
	return userAgent
}

func extractOpenAIRegisterUsedPercent(body []byte) (int, bool) {
	if len(body) == 0 {
		return 0, false
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return 0, false
	}

	rateLimit, _ := payload["rate_limit"].(map[string]any)
	if len(rateLimit) == 0 {
		return 0, false
	}
	primaryWindow, _ := rateLimit["primary_window"].(map[string]any)
	if len(primaryWindow) == 0 {
		return 0, false
	}
	usedPercent, ok := normalizeOpenAIRegisterPercent(primaryWindow["used_percent"])
	if !ok {
		return 0, false
	}
	return usedPercent, true
}

func normalizeOpenAIRegisterPercent(value any) (int, bool) {
	switch v := value.(type) {
	case float64:
		return int(v), true
	case float32:
		return int(v), true
	case int:
		return v, true
	case int64:
		return int(v), true
	case json.Number:
		if f, err := v.Float64(); err == nil {
			return int(f), true
		}
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return int(f), true
		}
	}
	return 0, false
}

func truncateOpenAIRegisterDetail(value string, maxLen int) string {
	value = strings.TrimSpace(value)
	if maxLen <= 0 || len(value) <= maxLen {
		return value
	}
	return value[:maxLen]
}
