<template>
  <AppLayout>
    <div class="mx-auto max-w-6xl space-y-6">
      <div class="card p-6">
        <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">账号检测</h1>
        <p class="mt-2 text-sm text-gray-500 dark:text-gray-400">
          支持定时检测 OpenAI OAuth 账号状态，并可直接选择 IP 管理中的代理发起底层检测请求，避免依赖全局代理。
        </p>
      </div>

      <div v-if="loading" class="flex items-center justify-center py-12">
        <div class="h-8 w-8 animate-spin rounded-full border-b-2 border-primary-600"></div>
      </div>

      <div v-else class="grid gap-6 xl:grid-cols-[360px_minmax(0,1fr)]">
        <section class="card p-6">
          <div class="border-b border-gray-100 pb-4 dark:border-dark-700">
            <h2 class="text-lg font-semibold text-gray-900 dark:text-white">检测配置</h2>
            <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
              自动检测会在当前实例后台循环执行。手动检测不受自动开关影响。
            </p>
          </div>

          <div class="mt-5 space-y-5">
            <label class="flex items-start gap-3">
              <input v-model="form.auto_check_enabled" type="checkbox" class="mt-1 h-4 w-4 rounded border-gray-300" />
              <div>
                <div class="text-sm font-medium text-gray-900 dark:text-white">启用自动检测</div>
                <div class="text-xs text-gray-500 dark:text-gray-400">按固定间隔自动检查 OpenAI OAuth 账号状态。</div>
              </div>
            </label>

            <div>
              <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">检测范围</label>
              <select v-model="form.scope" class="input w-full">
                <option value="all_openai_oauth">全部 OpenAI OAuth 账号</option>
                <option value="managed_only">仅受托管账号</option>
              </select>
            </div>

            <div>
              <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">检测间隔（秒）</label>
              <input v-model.number="form.check_interval_seconds" type="number" min="60" max="86400" class="input w-full" />
            </div>

            <div>
              <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">请求超时（秒）</label>
              <input v-model.number="form.request_timeout_seconds" type="number" min="5" max="120" class="input w-full" />
            </div>

            <div>
              <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">检测代理</label>
              <ProxySelector v-model="form.check_proxy_id" :proxies="proxies" />
              <p class="mt-2 text-xs text-gray-500 dark:text-gray-400">
                可从 IP 管理中选择任一代理；未选择时优先沿用账号绑定代理，没有则直连。
              </p>
            </div>

            <div>
              <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">高用量阈值（%）</label>
              <input v-model.number="form.usage_threshold_percent" type="number" min="1" max="100" class="input w-full" />
            </div>

            <label class="flex items-start gap-3">
              <input v-model="form.inactive_on_invalid" type="checkbox" class="mt-1 h-4 w-4 rounded border-gray-300" />
              <div>
                <div class="text-sm font-medium text-gray-900 dark:text-white">无效账号自动设为 inactive</div>
                <div class="text-xs text-gray-500 dark:text-gray-400">当检测到 401/403 或关键凭证缺失时，直接停用账号并写入错误信息。</div>
              </div>
            </label>

            <button type="button" class="btn btn-primary w-full" :disabled="saving" @click="saveSettings">
              {{ saving ? '保存中...' : '保存配置' }}
            </button>
          </div>
        </section>

        <div class="space-y-6">
          <section class="card p-6">
            <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-100 pb-4 dark:border-dark-700">
              <div>
                <h2 class="text-lg font-semibold text-gray-900 dark:text-white">运行状态</h2>
                <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">这里显示当前实例里的检测线程运行态，运行中会自动刷新进度。</p>
              </div>
              <button type="button" class="btn btn-secondary btn-sm" @click="loadRuntime">刷新状态</button>
            </div>

            <div class="mt-5 grid gap-4 md:grid-cols-2">
              <div class="rounded-lg border border-gray-100 p-4 dark:border-dark-700">
                <div class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">当前状态</div>
                <div class="mt-2 text-sm font-medium text-gray-900 dark:text-white">
                  {{ runtime?.running ? '检测中' : '空闲' }}
                </div>
              </div>
              <div class="rounded-lg border border-gray-100 p-4 dark:border-dark-700">
                <div class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">最近触发方式</div>
                <div class="mt-2 text-sm font-medium text-gray-900 dark:text-white">
                  {{ runtime?.last_trigger || '-' }}
                </div>
              </div>
              <div class="rounded-lg border border-gray-100 p-4 dark:border-dark-700">
                <div class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">最近开始时间</div>
                <div class="mt-2 text-sm font-medium text-gray-900 dark:text-white">
                  {{ formatDate(runtime?.last_started_at) }}
                </div>
              </div>
              <div class="rounded-lg border border-gray-100 p-4 dark:border-dark-700">
                <div class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">最近完成时间</div>
                <div class="mt-2 text-sm font-medium text-gray-900 dark:text-white">
                  {{ formatDate(runtime?.last_finished_at) }}
                </div>
              </div>
              <div class="rounded-lg border border-gray-100 p-4 dark:border-dark-700 md:col-span-2">
                <div class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">最近错误</div>
                <div class="mt-2 break-all text-sm font-medium text-red-600 dark:text-red-400">
                  {{ runtime?.last_error || '-' }}
                </div>
              </div>
              <div v-if="runtime?.current_total" class="rounded-lg border border-gray-100 p-4 dark:border-dark-700 md:col-span-2">
                <div class="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
                  <div class="min-w-0 flex-1">
                    <div class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">当前进度</div>
                    <div class="mt-2 text-sm font-medium text-gray-900 dark:text-white">
                      {{ runtime?.current_completed || 0 }} / {{ runtime?.current_total || 0 }}
                    </div>
                    <div class="mt-3 h-2 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-800">
                      <div
                        class="h-full rounded-full bg-primary-600 transition-all duration-300"
                        :style="{ width: `${progressPercent}%` }"
                      />
                    </div>
                  </div>
                  <div class="min-w-0 md:max-w-xs md:text-right">
                    <div class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">当前账号</div>
                    <div class="mt-2 break-all text-sm font-medium text-gray-900 dark:text-white">
                      {{ currentAccountLabel }}
                    </div>
                    <div class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                      {{ currentAccountStatusText }}
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </section>

          <section class="card p-6">
            <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-100 pb-4 dark:border-dark-700">
              <div>
                <h2 class="text-lg font-semibold text-gray-900 dark:text-white">手动检测</h2>
                <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">立即按当前配置扫描账号状态，并把结果回写到数据库。</p>
              </div>
              <button type="button" class="btn btn-primary" :disabled="running || runtime?.running" @click="runCheck">
                {{ running || runtime?.running ? '检测中...' : '立即检测' }}
              </button>
            </div>

            <div v-if="latestSummary" class="mt-5 grid gap-4 md:grid-cols-4">
              <div class="rounded-lg border border-gray-100 p-4 dark:border-dark-700">
                <div class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">总账号数</div>
                <div class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">{{ latestSummary.total }}</div>
              </div>
              <div class="rounded-lg border border-gray-100 p-4 dark:border-dark-700">
                <div class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">有效</div>
                <div class="mt-2 text-2xl font-semibold text-green-600 dark:text-green-400">{{ latestSummary.ok }}</div>
              </div>
              <div class="rounded-lg border border-gray-100 p-4 dark:border-dark-700">
                <div class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">无效</div>
                <div class="mt-2 text-2xl font-semibold text-red-600 dark:text-red-400">{{ latestSummary.invalid }}</div>
              </div>
              <div class="rounded-lg border border-gray-100 p-4 dark:border-dark-700">
                <div class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">高用量</div>
                <div class="mt-2 text-2xl font-semibold text-amber-600 dark:text-amber-400">{{ latestSummary.high_usage }}</div>
              </div>
              <div class="rounded-lg border border-gray-100 p-4 dark:border-dark-700">
                <div class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">不确定</div>
                <div class="mt-2 text-2xl font-semibold text-gray-900 dark:text-white">{{ latestSummary.uncertain }}</div>
              </div>
              <div class="rounded-lg border border-gray-100 p-4 dark:border-dark-700">
                <div class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">已停用</div>
                <div class="mt-2 text-2xl font-semibold text-red-600 dark:text-red-400">{{ latestSummary.inactivated }}</div>
              </div>
              <div class="rounded-lg border border-gray-100 p-4 dark:border-dark-700">
                <div class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">触发方式</div>
                <div class="mt-2 text-sm font-medium text-gray-900 dark:text-white">{{ latestSummary.trigger }}</div>
              </div>
              <div class="rounded-lg border border-gray-100 p-4 dark:border-dark-700">
                <div class="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">耗时</div>
                <div class="mt-2 text-sm font-medium text-gray-900 dark:text-white">{{ formatDuration(latestSummary.duration_ms) }}</div>
              </div>
            </div>

            <div v-if="resultRows.length" class="mt-6 overflow-x-auto">
              <table class="w-full min-w-[860px] text-sm">
                <thead>
                  <tr class="border-b border-gray-200 text-left text-xs uppercase tracking-wide text-gray-500 dark:border-dark-700 dark:text-gray-400">
                    <th class="py-2 pr-4">账号</th>
                    <th class="py-2 pr-4">结果</th>
                    <th class="py-2 pr-4">已用比例</th>
                    <th class="py-2 pr-4">动作</th>
                    <th class="py-2">详情</th>
                  </tr>
                </thead>
                <tbody>
                  <tr
                    v-for="item in resultRows"
                    :key="`${item.account_id}-${item.status}-${item.detail}`"
                    class="border-b border-gray-100 align-top dark:border-dark-800"
                  >
                    <td class="py-3 pr-4">
                      <div class="font-medium text-gray-900 dark:text-white">{{ item.name }}</div>
                      <div class="mt-1 text-xs text-gray-500 dark:text-gray-400">#{{ item.account_id }}</div>
                    </td>
                    <td class="py-3 pr-4">
                      <span
                        class="rounded px-2 py-0.5 text-xs"
                        :class="statusClass(item.status)"
                      >
                        {{ statusLabel(item.status) }}
                      </span>
                    </td>
                    <td class="py-3 pr-4 text-sm text-gray-700 dark:text-gray-300">
                      {{ formatUsedPercent(item.used_percent) }}
                    </td>
                    <td class="py-3 pr-4 text-sm text-gray-700 dark:text-gray-300">
                      {{ actionLabel(item.action) }}
                    </td>
                    <td class="py-3 text-sm text-gray-700 dark:text-gray-300">
                      {{ item.detail || '-' }}
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>
          </section>
        </div>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'

import AppLayout from '@/components/layout/AppLayout.vue'
import ProxySelector from '@/components/common/ProxySelector.vue'
import { adminAPI } from '@/api/admin'
import type {
  OpenAIRegisterCheckResult,
  OpenAIRegisterCheckRunResult,
  OpenAIRegisterRuntime,
  OpenAIRegisterSettings,
  OpenAIRegisterSummary
} from '@/api/admin/openaiRegister'
import type { Proxy } from '@/types'
import { useAppStore } from '@/stores/app'

const appStore = useAppStore()

const loading = ref(true)
const saving = ref(false)
const running = ref(false)
const proxies = ref<Proxy[]>([])
const runtime = ref<OpenAIRegisterRuntime | null>(null)
const checkResult = ref<OpenAIRegisterCheckRunResult | null>(null)
const runtimePollingTimer = ref<ReturnType<typeof setInterval> | null>(null)

type OpenAIRegisterResultRow = {
  account_id: number
  name: string
  status: OpenAIRegisterCheckResult['status'] | 'checking'
  used_percent?: number
  detail: string
  action: OpenAIRegisterCheckResult['action']
}

type OpenAIRegisterForm = OpenAIRegisterSettings & {
  check_proxy_id: number | null
}

const form = reactive<OpenAIRegisterForm>({
  auto_check_enabled: false,
  check_interval_seconds: 900,
  request_timeout_seconds: 20,
  usage_threshold_percent: 90,
  inactive_on_invalid: true,
  scope: 'all_openai_oauth',
  check_proxy_id: null
})

const latestSummary = computed<OpenAIRegisterSummary | null>(() => {
  return checkResult.value?.summary ?? runtime.value?.last_summary ?? null
})

const progressPercent = computed(() => {
  const total = runtime.value?.current_total ?? 0
  if (total <= 0) {
    return 0
  }
  const completed = runtime.value?.current_completed ?? 0
  return Math.max(0, Math.min(100, Math.round((completed / total) * 100)))
})

const currentAccountLabel = computed(() => {
  const currentRuntime = runtime.value
  if (!currentRuntime?.current_total) {
    return '-'
  }
  if (!currentRuntime.current_account_id) {
    return '当前没有正在检查的账号'
  }
  return `${currentRuntime.current_account_name || '未命名账号'} (#${currentRuntime.current_account_id})`
})

const currentAccountStatusText = computed(() => {
  const currentRuntime = runtime.value
  if (!currentRuntime?.current_total) {
    return '暂无运行中的检测任务'
  }
  if (currentRuntime.running && currentRuntime.current_account_id) {
    return '正在请求额度接口并回写状态'
  }
  if (currentRuntime.running) {
    return '检测线程已启动，等待领取下一个账号'
  }
  return '本轮检测已完成'
})

const runtimeResults = computed<OpenAIRegisterCheckResult[]>(() => {
  return runtime.value?.recent_results ?? []
})

const currentCheckingRow = computed<OpenAIRegisterResultRow | null>(() => {
  const currentRuntime = runtime.value
  if (!currentRuntime?.running || !currentRuntime.current_account_id) {
    return null
  }
  return {
    account_id: currentRuntime.current_account_id,
    name: currentRuntime.current_account_name || '未命名账号',
    status: 'checking',
    detail: '正在检查账号额度',
    action: 'none'
  }
})

const resultRows = computed<OpenAIRegisterResultRow[]>(() => {
  const rows: OpenAIRegisterResultRow[] = []
  if (currentCheckingRow.value) {
    rows.push(currentCheckingRow.value)
  }

  const finishedResults = runtimeResults.value.length > 0
    ? runtimeResults.value
    : (checkResult.value?.results ?? [])

  rows.push(...finishedResults.map(item => ({ ...item })))
  return rows
})

function startRuntimePolling() {
  if (runtimePollingTimer.value) {
    return
  }
  runtimePollingTimer.value = setInterval(async () => {
    try {
      await loadRuntime()
    } catch {
      // 轮询失败时不中断，等待下一轮刷新
    }
  }, 1500)
}

function stopRuntimePolling() {
  if (!runtimePollingTimer.value) {
    return
  }
  clearInterval(runtimePollingTimer.value)
  runtimePollingTimer.value = null
}

async function loadRuntime() {
  const runtimeData = await adminAPI.openAIRegister.getRuntime()
  runtime.value = runtimeData
  if (runtimeData.running) {
    startRuntimePolling()
    return
  }
  stopRuntimePolling()
}

async function loadPageData() {
  loading.value = true
  try {
    const [settingsResult, runtimeResult, proxiesResult] = await Promise.allSettled([
      adminAPI.openAIRegister.getSettings(),
      adminAPI.openAIRegister.getRuntime(),
      adminAPI.proxies.getAll()
    ])

    if (settingsResult.status !== 'fulfilled') {
      throw settingsResult.reason
    }
    if (runtimeResult.status !== 'fulfilled') {
      throw runtimeResult.reason
    }

    Object.assign(form, settingsResult.value, {
      check_proxy_id: settingsResult.value.check_proxy_id ?? null
    })
    runtime.value = runtimeResult.value

    if (proxiesResult.status === 'fulfilled') {
      proxies.value = proxiesResult.value
    } else {
      proxies.value = []
      appStore.showError(proxiesResult.reason?.message || '加载代理列表失败，当前仅可使用账号绑定代理或直连')
    }

    if (runtimeResult.value.running) {
      startRuntimePolling()
    }
  } catch (error: any) {
    appStore.showError(error?.message || '加载账号检测配置失败')
  } finally {
    loading.value = false
  }
}

async function saveSettings() {
  saving.value = true
  try {
    const saved = await adminAPI.openAIRegister.updateSettings({
      ...form,
      check_proxy_id: form.check_proxy_id ?? null
    })
    Object.assign(form, saved, {
      check_proxy_id: saved.check_proxy_id ?? null
    })
    appStore.showSuccess('账号检测配置已保存')
  } catch (error: any) {
    appStore.showError(error?.message || '保存账号检测配置失败')
  } finally {
    saving.value = false
  }
}

async function runCheck() {
  running.value = true
  checkResult.value = null
  if (runtime.value) {
    runtime.value = {
      ...runtime.value,
      running: true,
      current_total: 0,
      current_completed: 0,
      current_account_id: undefined,
      current_account_name: undefined,
      current_account_started_at: undefined,
      recent_results: []
    }
  }
  startRuntimePolling()
  try {
    checkResult.value = await adminAPI.openAIRegister.runCheck()
    await loadRuntime()
    appStore.showSuccess('账号检测已完成')
  } catch (error: any) {
    try {
      await loadRuntime()
    } catch {
      if (runtime.value) {
        runtime.value = {
          ...runtime.value,
          running: false
        }
      }
    }
    appStore.showError(error?.message || '执行账号检测失败')
  } finally {
    running.value = false
    if (!runtime.value?.running) {
      stopRuntimePolling()
    }
  }
}

function formatDate(value?: string) {
  if (!value) {
    return '-'
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value
  }
  return date.toLocaleString()
}

function formatDuration(value?: number) {
  if (!value || value <= 0) {
    return '-'
  }
  if (value < 1000) {
    return `${value} ms`
  }
  return `${(value / 1000).toFixed(2)} s`
}

function formatUsedPercent(value?: number) {
  if (typeof value !== 'number') {
    return '-'
  }
  return `${value}%`
}

function statusLabel(status: string) {
  switch (status) {
    case 'checking':
      return '检查中'
    case 'ok':
      return '正常'
    case 'invalid':
      return '无效'
    case 'high_usage':
      return '高用量'
    case 'uncertain':
      return '不确定'
    default:
      return '跳过'
  }
}

function actionLabel(action: string) {
  switch (action) {
    case 'set_inactive':
      return '已设为 inactive'
    default:
      return '-'
  }
}

function statusClass(status: string) {
  switch (status) {
    case 'checking':
      return 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300'
    case 'ok':
      return 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-300'
    case 'invalid':
      return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300'
    case 'high_usage':
      return 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
    case 'uncertain':
      return 'bg-gray-100 text-gray-700 dark:bg-dark-800 dark:text-gray-300'
    default:
      return 'bg-gray-100 text-gray-700 dark:bg-dark-800 dark:text-gray-300'
  }
}

onMounted(() => {
  loadPageData()
})

onBeforeUnmount(() => {
  stopRuntimePolling()
})
</script>
