import { flushPromises, mount } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import MonitorSettingsPanel from '../MonitorSettingsPanel.vue'
import { CONCRETE_PLATFORM_OPTIONS } from '@/constants/platforms'
import type { MonitorConfig } from '@/api/channelMonitorV2'

const mocks = vi.hoisted(() => ({
  getConfig: vi.fn(),
  updateConfig: vi.fn(),
  groups: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))
vi.mock('@/api/channelMonitorV2', () => ({
  getConfig: mocks.getConfig,
  updateConfig: mocks.updateConfig,
  MONITOR_ERROR_CATEGORIES: ['authentication', 'network'],
}))
vi.mock('@/api/admin', () => ({ adminAPI: { groups: { getAllIncludingInactive: mocks.groups } } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({
  cachedPublicSettings: { channel_monitor_enabled: true },
  showError: mocks.showError,
  showSuccess: mocks.showSuccess,
}) }))
vi.mock('@/utils/featureFlags', () => ({
  getChannelMonitorMode: () => 'v2',
  isChannelMonitorV2Mode: () => true,
}))
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key, te: () => false }),
  }
})

function config(): MonitorConfig {
  return {
    version: 2,
    enabled: true,
    refresh_interval_seconds: 60,
    platforms: [
      { platform: 'openai', enabled: false, models: ['gpt-4'] },
      { platform: 'custom-provider', enabled: true, models: ['custom-model'] },
    ],
    group_ids: [42],
    ignored_error_categories: [],
    health_thresholds: {
      minimum_sample: 70, warning_error_rate: 0.1, critical_error_rate: 0.3,
      target_ttft_ms: 1500, warning_ttft_ms: 2000, critical_ttft_ms: 5000,
      warning_cache_rate: 0.7, critical_cache_rate: 0.4,
      error_weight: 0.5, ttft_weight: 0.3, cache_weight: 0.2,
    },
  }
}

async function panel(value = config()) {
  mocks.getConfig.mockResolvedValue(value)
  const wrapper = mount(MonitorSettingsPanel, { global: { stubs: { RouterLink: true, teleport: true } } })
  await flushPromises()
  return wrapper
}

type Panel = VueWrapper

function button(wrapper: Panel, key: string) {
  return wrapper.findAll('button').find((item) => item.text() === 'channelMonitorV2.settings.' + key)!
}

function platformOptions(wrapper: Panel) {
  return wrapper
    .get('[data-testid="monitor-v2-platform-select"]')
    .findAll('option')
    .map((option) => option.attributes('value') || '')
    .filter((value) => value !== '')
}

function modelsInputs(wrapper: Panel) {
  return wrapper.findAll('input[placeholder="channelMonitorV2.settings.modelsPlaceholder"]')
}

async function add(wrapper: Panel, platform: string) {
  await wrapper.get('[data-testid="monitor-v2-platform-select"]').setValue(platform)
  await button(wrapper, 'addPlatform').trigger('click')
}

beforeEach(() => {
  vi.clearAllMocks()
  mocks.groups.mockResolvedValue([])
  mocks.updateConfig.mockImplementation(async (value) => JSON.parse(JSON.stringify(value)))
})

describe('MonitorSettingsPanel platform configuration', () => {
  it('adds a missing provider and saves its models without changing existing settings', async () => {
    const original = config()
    const wrapper = await panel(original)
    expect(button(wrapper, 'save').attributes('disabled')).toBeDefined()
    expect(platformOptions(wrapper)).not.toContain('openai')
    expect(platformOptions(wrapper)).not.toContain('custom-provider')

    await add(wrapper, 'deepseek')
    expect(platformOptions(wrapper)).not.toContain('deepseek')
    const models = modelsInputs(wrapper).at(-1)!
    await models.setValue(' deepseek-chat, deepseek-reasoner, deepseek-chat ')
    await button(wrapper, 'save').trigger('click')
    await flushPromises()

    expect(mocks.updateConfig).toHaveBeenCalledWith({
      ...original,
      candy_probes: [],
      candy_history_limit: 100,
      platforms: [...original.platforms, {
        platform: 'deepseek', enabled: true, models: ['deepseek-chat', 'deepseek-reasoner'],
      }],
    })
    expect(button(wrapper, 'save').attributes('disabled')).toBeDefined()
    expect(original.platforms).toHaveLength(2)

    const persisted = mocks.updateConfig.mock.results[0].value
    wrapper.unmount()
    const reloaded = await panel(await persisted)
    expect((modelsInputs(reloaded).at(-1)!.element as HTMLInputElement).value).toBe('deepseek-chat, deepseek-reasoner')
    expect(platformOptions(reloaded)).not.toContain('deepseek')
    reloaded.unmount()
  })

  it('can start with no configured platforms and rejects duplicate additions', async () => {
    const wrapper = await panel({ ...config(), platforms: [] })
    await add(wrapper, 'kimi')
    await wrapper.get('[data-testid="monitor-v2-platform-custom"]').setValue('Kimi')
    await button(wrapper, 'addPlatform').trigger('click')
    expect(mocks.showError).toHaveBeenCalledWith('channelMonitorV2.settings.platformDuplicate')
    await button(wrapper, 'save').trigger('click')
    await flushPromises()
    expect(mocks.updateConfig.mock.calls[0][0].platforms).toEqual([
      { platform: 'kimi', enabled: true, models: [] },
    ])
    wrapper.unmount()
  })

  it('restores the server config and available options when saving fails', async () => {
    const wrapper = await panel()
    mocks.updateConfig.mockRejectedValueOnce(new Error('save failed'))
    await add(wrapper, 'minimax')
    await button(wrapper, 'save').trigger('click')
    await flushPromises()
    expect(mocks.showError).toHaveBeenCalledOnce()
    expect(platformOptions(wrapper)).toContain('minimax')
    expect(modelsInputs(wrapper)).toHaveLength(2)
    expect(button(wrapper, 'save').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('stops offering platforms that are already configured', async () => {
    const wrapper = await panel({
      ...config(),
      platforms: CONCRETE_PLATFORM_OPTIONS.map(({ value }) => ({ platform: value, enabled: true, models: [] })),
    })
    const offered = platformOptions(wrapper)
    for (const { value } of CONCRETE_PLATFORM_OPTIONS) expect(offered).not.toContain(value)
    wrapper.unmount()
  })

  it('adds a hand-typed platform and rejects ids outside the platform pattern', async () => {
    const wrapper = await panel()
    const custom = wrapper.get('[data-testid="monitor-v2-platform-custom"]')
    await custom.setValue('My Provider')
    await button(wrapper, 'addPlatform').trigger('click')
    expect(mocks.showError).toHaveBeenCalledWith('channelMonitorV2.settings.platformInvalid')

    await custom.setValue('my-provider')
    await button(wrapper, 'addPlatform').trigger('click')
    await button(wrapper, 'save').trigger('click')
    await flushPromises()
    expect(mocks.updateConfig.mock.calls[0][0].platforms.at(-1)).toEqual({
      platform: 'my-provider', enabled: true, models: [],
    })
    wrapper.unmount()
  })
})
