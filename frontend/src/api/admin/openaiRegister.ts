import { apiClient } from '../client'

export interface OpenAIRegisterSettings {
  auto_check_enabled: boolean
  check_interval_seconds: number
  request_timeout_seconds: number
  usage_threshold_percent: number
  inactive_on_invalid: boolean
  scope: 'all_openai_oauth' | 'managed_only'
}

export interface OpenAIRegisterSummary {
  trigger: string
  scope: string
  total: number
  checked: number
  ok: number
  invalid: number
  high_usage: number
  uncertain: number
  skipped: number
  inactivated: number
  started_at: string
  finished_at: string
  duration_ms: number
}

export interface OpenAIRegisterCheckResult {
  account_id: number
  name: string
  status: 'ok' | 'invalid' | 'high_usage' | 'uncertain' | 'skipped'
  used_percent?: number
  detail: string
  action: 'none' | 'set_inactive'
}

export interface OpenAIRegisterCheckRunResult {
  summary: OpenAIRegisterSummary
  results: OpenAIRegisterCheckResult[]
}

export interface OpenAIRegisterRuntime {
  running: boolean
  last_started_at?: string
  last_finished_at?: string
  last_duration_ms: number
  last_trigger?: string
  last_error?: string
  last_summary?: OpenAIRegisterSummary
  current_total: number
  current_completed: number
  current_account_id?: number
  current_account_name?: string
  current_account_started_at?: string
  recent_results?: OpenAIRegisterCheckResult[]
}

export async function getSettings(): Promise<OpenAIRegisterSettings> {
  const { data } = await apiClient.get<OpenAIRegisterSettings>('/admin/openai-register/settings')
  return data
}

export async function updateSettings(payload: OpenAIRegisterSettings): Promise<OpenAIRegisterSettings> {
  const { data } = await apiClient.put<OpenAIRegisterSettings>('/admin/openai-register/settings', payload)
  return data
}

export async function getRuntime(): Promise<OpenAIRegisterRuntime> {
  const { data } = await apiClient.get<OpenAIRegisterRuntime>('/admin/openai-register/runtime')
  return data
}

export async function runCheck(payload?: { account_ids?: number[] }): Promise<OpenAIRegisterCheckRunResult> {
  const { data } = await apiClient.post<OpenAIRegisterCheckRunResult>('/admin/openai-register/checks/run', payload ?? {})
  return data
}

export default {
  getSettings,
  updateSettings,
  getRuntime,
  runCheck
}
