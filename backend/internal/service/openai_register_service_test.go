//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type openAIRegisterAccountRepoStub struct {
	mockAccountRepoForGemini
	listAccounts    []Account
	updatedAccounts []Account
	updatedExtraID  int64
	updatedExtra    map[string]any
}

func (r *openAIRegisterAccountRepoStub) ListWithFilters(
	_ context.Context,
	_ pagination.PaginationParams,
	_, _, _, _ string,
	_ int64,
) ([]Account, *pagination.PaginationResult, error) {
	return r.listAccounts, &pagination.PaginationResult{
		Total:    int64(len(r.listAccounts)),
		Page:     1,
		PageSize: len(r.listAccounts),
		Pages:    1,
	}, nil
}

func (r *openAIRegisterAccountRepoStub) Update(_ context.Context, account *Account) error {
	if account == nil {
		return nil
	}
	r.updatedAccounts = append(r.updatedAccounts, *account)
	return nil
}

func (r *openAIRegisterAccountRepoStub) UpdateExtra(_ context.Context, id int64, updates map[string]any) error {
	r.updatedExtraID = id
	r.updatedExtra = make(map[string]any, len(updates))
	for key, value := range updates {
		r.updatedExtra[key] = value
	}
	return nil
}

func TestOpenAIRegisterService_GetSettings_DefaultsOnMissingSetting(t *testing.T) {
	svc := NewOpenAIRegisterService(&openAIRegisterAccountRepoStub{}, &settingRepoStub{values: map[string]string{}})

	settings, err := svc.GetSettings(context.Background())
	require.NoError(t, err)
	require.NotNil(t, settings)
	require.False(t, settings.AutoCheckEnabled)
	require.Equal(t, 900, settings.CheckIntervalSeconds)
	require.Equal(t, 20, settings.RequestTimeoutSecs)
	require.Equal(t, 90, settings.UsageThresholdPct)
	require.True(t, settings.InactiveOnInvalid)
	require.Equal(t, OpenAIRegisterScopeAllOpenAIOAuth, settings.Scope)
	require.Nil(t, settings.CheckProxyID)
}

func TestOpenAIRegisterService_RunCheck_InvalidAccountSetsInactive(t *testing.T) {
	account := Account{
		ID:       101,
		Name:     "oa-1",
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Status:   StatusActive,
		Credentials: map[string]any{
			"access_token":       "token-1",
			"chatgpt_account_id": "acct-1",
		},
	}
	repo := &openAIRegisterAccountRepoStub{listAccounts: []Account{account}}

	settingsRaw, _ := json.Marshal(&OpenAIRegisterSettings{
		AutoCheckEnabled:     false,
		CheckIntervalSeconds: 900,
		RequestTimeoutSecs:   15,
		UsageThresholdPct:    90,
		InactiveOnInvalid:    true,
		Scope:                OpenAIRegisterScopeAllOpenAIOAuth,
	})
	svc := NewOpenAIRegisterService(repo, &settingRepoStub{values: map[string]string{
		SettingKeyOpenAIRegisterSettings: string(settingsRaw),
	}})
	svc.clientFactory = func(_ httpclient.Options) (*http.Client, error) {
		return &http.Client{
			Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusUnauthorized,
					Body:       io.NopCloser(strings.NewReader("")),
					Header:     make(http.Header),
				}, nil
			}),
		}, nil
	}
	svc.usageURL = "https://chatgpt.com/backend-api/wham/usage"
	now := time.Date(2026, 3, 21, 10, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }

	result, err := svc.RunCheck(context.Background(), &OpenAIRegisterRunCheckInput{Trigger: "manual"})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, result.Summary.Invalid)
	require.Len(t, repo.updatedAccounts, 1)
	require.Equal(t, openAIAccountStatusInactive, repo.updatedAccounts[0].Status)
	require.Equal(t, "额度接口返回 401", repo.updatedAccounts[0].ErrorMessage)
	require.Equal(t, openAIRegisterActionSetInactive, result.Results[0].Action)
}

func TestOpenAIRegisterService_RunCheck_HighUsageOnlyUpdatesExtra(t *testing.T) {
	account := Account{
		ID:       102,
		Name:     "oa-2",
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Status:   StatusActive,
		Credentials: map[string]any{
			"access_token":       "token-2",
			"chatgpt_account_id": "acct-2",
		},
	}
	repo := &openAIRegisterAccountRepoStub{listAccounts: []Account{account}}

	svc := NewOpenAIRegisterService(repo, &settingRepoStub{values: map[string]string{}})
	svc.clientFactory = func(_ httpclient.Options) (*http.Client, error) {
		return &http.Client{
			Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(strings.NewReader(
						`{"rate_limit":{"primary_window":{"used_percent":95}}}`,
					)),
					Header: http.Header{
						"Content-Type": []string{"application/json"},
					},
				}, nil
			}),
		}, nil
	}
	svc.usageURL = "https://chatgpt.com/backend-api/wham/usage"

	result, err := svc.RunCheck(context.Background(), &OpenAIRegisterRunCheckInput{Trigger: "manual"})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, result.Summary.HighUsage)
	require.Empty(t, repo.updatedAccounts)
	require.Equal(t, int64(102), repo.updatedExtraID)
	require.Equal(t, openAIRegisterStatusHighUsage, repo.updatedExtra["openai_register_check_status"])
}

func TestOpenAIRegisterService_RunCheck_UsesSelectedCheckProxy(t *testing.T) {
	proxyID := int64(7)
	account := Account{
		ID:       104,
		Name:     "oa-4",
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Status:   StatusActive,
		Credentials: map[string]any{
			"access_token":       "token-4",
			"chatgpt_account_id": "acct-4",
		},
		Proxy: &Proxy{
			Protocol: "http",
			Host:     "account-proxy.local",
			Port:     7890,
		},
	}
	repo := &openAIRegisterAccountRepoStub{listAccounts: []Account{account}}

	settingsRaw, _ := json.Marshal(&OpenAIRegisterSettings{
		CheckIntervalSeconds: 900,
		RequestTimeoutSecs:   15,
		UsageThresholdPct:    90,
		InactiveOnInvalid:    true,
		Scope:                OpenAIRegisterScopeAllOpenAIOAuth,
		CheckProxyID:         &proxyID,
	})
	svc := NewOpenAIRegisterService(repo, &settingRepoStub{values: map[string]string{
		SettingKeyOpenAIRegisterSettings: string(settingsRaw),
	}})
	svc.proxyRepo = &mockProxyRepoForOAuth{
		getByIDFunc: func(_ context.Context, id int64) (*Proxy, error) {
			require.Equal(t, proxyID, id)
			return &Proxy{
				Protocol: "http",
				Host:     "managed-proxy.local",
				Port:     8080,
			}, nil
		},
	}

	var capturedProxyURL string
	svc.clientFactory = func(opts httpclient.Options) (*http.Client, error) {
		capturedProxyURL = opts.ProxyURL
		return &http.Client{
			Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(strings.NewReader(
						`{"rate_limit":{"primary_window":{"used_percent":10}}}`,
					)),
					Header: http.Header{
						"Content-Type": []string{"application/json"},
					},
				}, nil
			}),
		}, nil
	}
	svc.usageURL = "https://chatgpt.com/backend-api/wham/usage"

	result, err := svc.RunCheck(context.Background(), &OpenAIRegisterRunCheckInput{Trigger: "manual"})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "http://managed-proxy.local:8080", capturedProxyURL)
	require.Equal(t, 1, result.Summary.OK)
}

func TestOpenAIRegisterService_RunCheck_ExposesRuntimeProgress(t *testing.T) {
	account := Account{
		ID:       103,
		Name:     "oa-3",
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Status:   StatusActive,
		Credentials: map[string]any{
			"access_token":       "token-3",
			"chatgpt_account_id": "acct-3",
		},
	}
	repo := &openAIRegisterAccountRepoStub{listAccounts: []Account{account}}

	svc := NewOpenAIRegisterService(repo, &settingRepoStub{values: map[string]string{}})

	requestStarted := make(chan struct{}, 1)
	releaseResponse := make(chan struct{})

	svc.clientFactory = func(_ httpclient.Options) (*http.Client, error) {
		return &http.Client{
			Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
				select {
				case requestStarted <- struct{}{}:
				default:
				}
				<-releaseResponse
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(strings.NewReader(
						`{"rate_limit":{"primary_window":{"used_percent":10}}}`,
					)),
					Header: http.Header{
						"Content-Type": []string{"application/json"},
					},
				}, nil
			}),
		}, nil
	}

	done := make(chan struct{})
	var result *OpenAIRegisterCheckRunResult
	var err error

	go func() {
		result, err = svc.RunCheck(context.Background(), &OpenAIRegisterRunCheckInput{Trigger: "manual"})
		close(done)
	}()

	<-requestStarted

	runtime := svc.GetRuntime()
	require.True(t, runtime.Running)
	require.Equal(t, 1, runtime.CurrentTotal)
	require.Equal(t, 0, runtime.CurrentCompleted)
	require.Equal(t, int64(103), runtime.CurrentAccountID)
	require.Equal(t, "oa-3", runtime.CurrentAccountName)
	require.NotNil(t, runtime.CurrentAccountStarted)
	require.Empty(t, runtime.RecentResults)

	close(releaseResponse)
	<-done

	require.NoError(t, err)
	require.NotNil(t, result)

	runtime = svc.GetRuntime()
	require.False(t, runtime.Running)
	require.Equal(t, 1, runtime.CurrentTotal)
	require.Equal(t, 1, runtime.CurrentCompleted)
	require.Zero(t, runtime.CurrentAccountID)
	require.Empty(t, runtime.CurrentAccountName)
	require.Nil(t, runtime.CurrentAccountStarted)
	require.Len(t, runtime.RecentResults, 1)
	require.Equal(t, openAIRegisterStatusOK, runtime.RecentResults[0].Status)
}
