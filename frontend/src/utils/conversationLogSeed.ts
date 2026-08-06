import type { ConversationLog } from '@/api/admin/conversationLogs'

// Local development preview records sampled directly from the rhzy production
// database on 2026-08-06. The OpenAI route, stream protocol, model, timestamp,
// latency, and token fields map one-to-one to their source rows. Account IDs,
// opaque IDs, and body text containing credentials or copied source material
// are redacted or shortened before being stored in the repository.
// This file is only loaded by `?seed=conversation-logs` in development.

const taggingInstructions = [
  'You suggest 1-3 short topic tags for news articles.',
  'Tags must be 1-3 words, lowercase, and contain no punctuation.',
  'Return JSON matching: { "tags": string[] }.'
].join(' ')

function openaiStreamingDbRequest() {
  const redactedContext = Array.from({ length: 16 }, (_, index) => (
    `[Production context fragment ${index + 1}/16 redacted — this source request contains third-party credentials.]`
  )).join('\n')

  return JSON.stringify({
    include: ['reasoning.encrypted_content'],
    model: 'gpt-5.6-luna',
    input: [{
      role: 'user',
      content: `Production conversation context (redacted for local preview):\n${redactedContext}`
    }],
    store: false,
    stream: true,
    text: { verbosity: 'low' }
  })
}

function openaiStreamingDbResponse() {
  return [
    'event: response.created',
    'data: {"type":"response.created","response":{"id":"resp_[redacted]","object":"response","status":"in_progress","model":"gpt-5.6-luna","output":[]}}',
    '',
    'event: response.completed',
    'data: {"type":"response.completed","response":{"id":"resp_[redacted]","object":"response","status":"completed","model":"gpt-5.6-luna"}}',
    '',
    'data: [DONE]'
  ].join('\n')
}

function openaiLongStreamingDbResponse() {
  // Source log 328886 contains 122 response.function_call_arguments.delta events.
  // The event sequence is retained while the argument values are removed.
  const argumentDeltas = Array.from({ length: 122 }, (_, index) => [
    'event: response.function_call_arguments.delta',
    `data: {"type":"response.function_call_arguments.delta","item_id":"fc_[redacted]","call_id":"call_[redacted]","name":"production_operation","delta":"[redacted production argument fragment ${index + 1}/122]"}`,
    ''
  ].join('\n')).join('\n')

  return [
    'event: response.created',
    'data: {"type":"response.created","response":{"id":"resp_[redacted]","object":"response","status":"in_progress","model":"gpt-5.6-luna","output":[]}}',
    '',
    'event: response.output_item.added',
    'data: {"type":"response.output_item.added","item":{"id":"fc_[redacted]","type":"function_call","name":"production_operation","arguments":""}}',
    '',
    argumentDeltas,
    'event: response.function_call_arguments.done',
    'data: {"type":"response.function_call_arguments.done","item_id":"fc_[redacted]","name":"production_operation","arguments":"[redacted production function arguments]"}',
    '',
    'event: response.completed',
    'data: {"type":"response.completed","response":{"id":"resp_[redacted]","object":"response","status":"completed","model":"gpt-5.6-luna"}}',
    '',
    'data: [DONE]'
  ].join('\n')
}

const shortenedArticle = [
  'Article title: 《完蛋！我被男同学包围了》为什么会好评如潮，',
  '纯粹因为它是高中生玩票制作的搞笑玩梗游戏吗？',
  '',
  'Article excerpt: 一款由高中生团队制作的校园日常游戏在 Steam 免费发布后，',
  '因完成度和轻松题材受到玩家关注。',
  '',
  '[The remaining production article text is omitted from the local seed.]'
].join('\n')

function createProductionSeed(id: number, overrides: Partial<ConversationLog>): ConversationLog {
  return {
    id,
    request_id: `redacted-production-request-${id}`,
    response_id: '',
    user_id: 0,
    user_email: 'redacted-production-user',
    api_key_id: 0,
    api_key_name: 'Production API key (redacted)',
    account_id: 0,
    account_name: 'Production account (redacted)',
    group_id: null,
    group_name: 'Production group (redacted)',
    platform: 'openai',
    inbound_endpoint: '/v1/responses',
    upstream_endpoint: 'wss://redacted-upstream.invalid/responses',
    model: 'gpt-5.6-terra',
    requested_model: 'gpt-5.6-terra',
    upstream_model: 'gpt-5.6-terra',
    request_type: 'ws_v2',
    stream: true,
    openai_ws_mode: true,
    status_code: 101,
    duration_ms: 0,
    first_token_ms: null,
    input_tokens: 0,
    output_tokens: 0,
    cache_read_tokens: 0,
    cache_create_tokens: 0,
    request_hash: 'redacted-production-hash',
    request_body: '',
    response_body: '',
    request_truncated: false,
    response_truncated: false,
    queue_delay_ms: null,
    created_at: '2026-08-06T00:00:00+08:00',
    total_tokens: 0,
    ...overrides
  }
}

export const conversationLogSeed: ConversationLog[] = [
  // Production log 328903: a real HTTP SSE stream, not a WebSocket connection.
  createProductionSeed(328903, {
    model: 'gpt-5.6-luna',
    requested_model: 'gpt-5.6-luna',
    upstream_model: 'gpt-5.6-luna',
    request_type: 'stream',
    stream: true,
    openai_ws_mode: false,
    status_code: 200,
    duration_ms: 86835,
    first_token_ms: 77298,
    input_tokens: 16471,
    output_tokens: 9,
    cache_read_tokens: 15872,
    total_tokens: 16480,
    created_at: '2026-08-06T14:10:01.761354+08:00',
    request_body: openaiStreamingDbRequest(),
    response_body: openaiStreamingDbResponse(),
    request_truncated: true,
    response_truncated: true
  }),
  // Production log 328833: a compact Anthropic request and its actual short JSON reply.
  createProductionSeed(328833, {
    platform: 'anthropic',
    inbound_endpoint: '/v1/messages',
    upstream_endpoint: 'https://redacted-upstream.invalid/v1/messages',
    model: 'MiniMax-M3',
    requested_model: 'MiniMax-M3',
    upstream_model: 'MiniMax-M3',
    request_type: 'sync',
    stream: false,
    openai_ws_mode: false,
    status_code: 200,
    duration_ms: 2188,
    input_tokens: 644,
    output_tokens: 10,
    cache_read_tokens: 128,
    total_tokens: 654,
    created_at: '2026-08-06T13:56:23.298552+08:00',
    request_body: JSON.stringify({
      model: 'MiniMax-M3',
      max_tokens: 4096,
      system: taggingInstructions,
      messages: [{ role: 'user', content: shortenedArticle }],
      temperature: 0.2
    }),
    response_body: JSON.stringify({
      type: 'message',
      role: 'assistant',
      model: 'MiniMax-M3',
      content: [{ type: 'text', text: '{"tags":["steam","game-development"]}' }],
      usage: { input_tokens: 644, output_tokens: 10, cache_read_tokens: 128 },
      stop_reason: 'end_turn'
    })
  }),
  // Production log 328886: a 77KB request / 251KB HTTP SSE response with 122 argument deltas.
  createProductionSeed(328886, {
    model: 'gpt-5.6-luna',
    requested_model: 'gpt-5.6-luna',
    upstream_model: 'gpt-5.6-luna',
    request_type: 'stream',
    stream: true,
    openai_ws_mode: false,
    status_code: 200,
    duration_ms: 53520,
    first_token_ms: 24140,
    input_tokens: 16187,
    output_tokens: 235,
    cache_read_tokens: 15872,
    total_tokens: 16422,
    created_at: '2026-08-06T14:06:42.796749+08:00',
    request_body: openaiStreamingDbRequest(),
    response_body: openaiLongStreamingDbResponse(),
    request_truncated: true,
    response_truncated: true
  })
]
