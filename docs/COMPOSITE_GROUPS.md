# Composite Groups

Composite groups are an admin routing layer for API keys that should choose a
concrete provider from the requested model instead of binding the key to a
single provider group. They support both built-in model detection and an
admin-configured model route registry for public model aliases.

## Supported Providers

Composite groups can route to these concrete account platforms:

- Anthropic
- Gemini
- OpenAI
- Antigravity
- Grok
- Kimi
- Zhipu GLM
- DeepSeek
- MiniMax

The selected concrete platform is used for account selection, user platform
quota checks, post-usage billing, ops error platform attribution, channel
mapping/pricing lookup, and platform usage reporting.

## Route Registry

Admins can configure routes on a composite group from the group list's
`Routes` action or through the admin API:

- `GET /api/v1/admin/groups/:id/composite-routes`
- `POST /api/v1/admin/groups/:id/composite-routes`
- `PUT /api/v1/admin/groups/:id/composite-routes/:route_id`
- `DELETE /api/v1/admin/groups/:id/composite-routes/:route_id`
- `POST /api/v1/admin/groups/:id/composite-routes/preview`

Each route belongs to one composite group and contains:

- `public_model`: model identifier the client sends.
- `match_type`: `exact` or `prefix`.
- `target_platform`: concrete provider platform.
- `upstream_model`: model identifier sent upstream. If omitted, the public
  model is reused.
- `endpoint`: `any`, `messages`, `count_tokens`, `responses`,
  `chat_completions`, `embeddings`, `images`, or `gemini`.
- `priority`: lower values win after match specificity.
- `enabled`: disabled routes are ignored by runtime resolution but remain
  visible to admins.
- `rules`: ordered smart rules (see [Smart Rules](#smart-rules)); always
  returned as an array, `[]` when none.
- `session_affinity`: keep a client session on its first smart-rule choice
  (default `true`).

Resolution order is explicit route first, then built-in detection. When more
than one explicit route matches, exact matches beat prefix matches,
endpoint-specific routes beat `any`, longer prefixes beat shorter prefixes,
then lower `priority`, then lower route id.

For JSON-body endpoints, the gateway rewrites the request `model` field to the
route's `upstream_model` before dispatch. For Gemini native paths such as
`/v1beta/models/{model}:generateContent`, the gateway resolves `{model}` and
the handler forwards the resolved upstream model.

Codex Alpha Search and Live requests use the `responses` route domain. Live
requests resolve the model from `session.model`, including multipart `session`
payloads, and apply the configured `upstream_model` before dispatch.
Codex model manifest requests reuse the existing OpenAI account selection and
failover path within the Composite group.

Responses WebSocket resolves the first `response.create` model in the
`responses` route domain (including `any` routes). Only OpenAI and Grok targets
are supported. The public model remains the admission and response identity;
the route's upstream model is applied before channel/account mapping. Subsequent
turns can repeat or omit the public model. Changing it requires reconnecting so
the gateway can resolve the route and select an account again.

## Smart Rules

An explicit route (for example `public_model = auto`) can carry up to 20
ordered smart rules that pick a different concrete platform and upstream model
from the request content. Routes with rules must use the `any`, `messages`,
`responses`, or `chat_completions` endpoint scope.

Each rule contains:

- `name`: optional label, at most 64 characters.
- `enabled`: disabled rules stay visible but are never candidates.
- `target_platform`: concrete provider platform (same check as the route).
- `upstream_model`: required, at most 200 characters.
- `conditions`: every condition that is set must hold (AND); omitted or `null`
  conditions do not restrict:
  - `has_images`: the request contains image content parts.
  - `has_tools`: the request declares tools (`tools` / `functions`).
  - `reasoning`: thinking/reasoning is enabled (`thinking.type` other than
    `disabled`, `reasoning_effort` / `reasoning.effort` other than `none`).
  - `min_input_tokens` / `max_input_tokens`: bounds on the estimated input
    tokens (text of system/instructions, messages/input and tool definitions;
    image and file payloads are not counted). `0` means unbounded; when both
    are set `min <= max`.
  - `clients`: any of `claude_code`, `codex`, `gemini_cli`, `other`, detected
    from the User-Agent.
  - `keywords`: up to 20 keywords (1-100 characters, trimmed and de-duplicated);
    any keyword matches as a case-insensitive substring of the most recent user
    message that contains text (tool-result-only turns are skipped).

### Matching Order and Availability

Candidates are the enabled rules whose conditions all match, in rule order,
followed by the route's own `target_platform` / `upstream_model` (the default
target, which has no conditions). The gateway uses the first candidate whose
target platform has a currently schedulable account in the group that supports
its upstream model. A failed availability check counts as available. When no
candidate is available the first candidate is used, so the request fails with
the normal no-available-account error instead of silently changing provider.
Availability answers are cached in-process for a few seconds.

Because unavailable targets fall through to later rules, a rule that targets a
text-only model should set `has_images: false`; otherwise an image request can
fall through to it when the vision target has no available account. Use the
preview to check what a request falls back to.

### Session Affinity

With `session_affinity` enabled, the gateway remembers the chosen target per
client session for one hour (refreshed on every request) in Redis under
`composite_route_sticky:{group}:{route}:{sha256(session) prefix}`. Session IDs
come from the Anthropic `metadata.user_id` session, or the OpenAI
`session_id` / `conversation_id` headers and `prompt_cache_key`. Requests
without a session ID are routed independently.

A session keeps its earlier target while that target is still the route
target or an enabled rule's target, is still available, and its rule's
capability conditions (`has_images`, `min_input_tokens`, `max_input_tokens`)
still hold. Capability conditions can also force a switch: when the freshly
selected rule requires images or a minimum context size and the session's rule
does not, the session moves to the new target. Other conditions (tools,
reasoning, clients, keywords) never switch an established session. When the
earlier target no longer qualifies, the gateway selects again and overwrites
the session record. `count_tokens` reads the session choice but never writes
it.

### Entry Points Without a Body

Rules are evaluated only on the text entry points: `messages`, `count_tokens`
(read as messages), `chat_completions`, and `responses`. Gemini native paths,
Responses WebSocket, Live, Alpha Search, images, embeddings, and other
entry points resolve a route with rules to its default target.

### Billing and Usage

Billing is unchanged: usage is priced by the concrete platform and upstream
model that actually served the request, for both subscription and balance
groups. Clients keep seeing the public model in responses. The usage record's
model mapping chain starts with the public model when it differs from the
routed model, for example `auto→claude-sonnet-4-6`. Each smart-rule decision is
also logged as `composite_smart_route_selected` with the group, route, rule
index, target, upstream model, stickiness and a feature summary (never the
user's text).

### Preview

`POST /api/v1/admin/groups/:id/composite-routes/preview` accepts an optional
sample `body` (JSON object; read as `messages` when `endpoint` is `any`) and
`user_agent`. With a body, the decision adds `rule_index` (`-1` for the route
target), `rule_name`, `sticky` (always `false` in previews), `features`, and a
`trace` listing every rule and finally the route target with `matched`,
`available`, `selected`, and `reason`. Without a body, routes with rules return
their default target with no `features` or `trace`.

### Example: `auto`

| Order | Name | Conditions | Target platform | Upstream model |
| --- | --- | --- | --- | --- |
| 0 | `vision` | `has_images: true` | `gemini` | `gemini-2.5-pro` |
| 1 | `long-context` | `min_input_tokens: 150000` | `gemini` | `gemini-2.5-pro` |
| 2 | `translate` | `keywords: ["翻译", "translate"]`, `has_images: false` | `deepseek` | `deepseek-chat` |
| route | default | none | `anthropic` | `claude-sonnet-4-6` |

```json
{
  "public_model": "auto",
  "match_type": "exact",
  "endpoint": "any",
  "target_platform": "anthropic",
  "upstream_model": "claude-sonnet-4-6",
  "session_affinity": true,
  "rules": [
    {"name": "vision", "enabled": true, "target_platform": "gemini", "upstream_model": "gemini-2.5-pro", "conditions": {"has_images": true}},
    {"name": "long-context", "enabled": true, "target_platform": "gemini", "upstream_model": "gemini-2.5-pro", "conditions": {"min_input_tokens": 150000}},
    {"name": "translate", "enabled": true, "target_platform": "deepseek", "upstream_model": "deepseek-chat", "conditions": {"keywords": ["翻译", "translate"], "has_images": false}}
  ]
}
```

A Claude Code session that starts with plain text stays on Claude Sonnet even
if a later turn asks for a translation; a later turn that attaches an image
switches the session to Gemini.

## Built-In Detection

Composite routing detects common public model IDs and provider-prefixed IDs:

- `claude-*` and `anthropic/claude-*` route to Anthropic.
- `gemini-*` and `google/gemini-*` route to Gemini.
- `gpt-*`, `o*`, `codex-*`, `text-embedding-*`, `dall-e-*`, and
  `openai/*` route to OpenAI.
- `grok-*` and `xai/grok-*` route to Grok.
- `kimi-*`, `moonshot-*`, and `k3` route to Kimi.
- `glm-*` routes to Zhipu GLM.
- `deepseek-*` routes to DeepSeek.
- `minimax-*`, `abab*`, and `minimax/*` route to MiniMax.

Unknown or ambiguous model names fail closed with a client error instead of
guessing a provider.

## Admin Workflows

- Admins can create a group with platform `composite`.
- Admins can add, edit, delete, and preview composite model routes.
- Composite groups can copy accounts from concrete provider groups.
- Concrete provider accounts can be assigned directly to composite groups from
  account create/edit and bulk account workflows.
- Subscription payment plans can bind to a composite group when that group's
  `subscription_type` is `subscription`. The plan grants access to the
  composite group; each request is still billed and quota-checked against the
  resolved concrete provider platform.
- Channel configuration exposes composite groups in concrete provider sections.
  The channel `group_ids` payload is still flat; provider-specific model
  mapping and pricing remain keyed by concrete platform.

## Bucket 2 Setup: OpenAI + Claude + Gemini + Grok

Use one composite subscription group when one customer-facing plan should expose
model aliases across OpenAI, Claude, Gemini, and Grok without issuing separate
keys per provider.

1. Create concrete provider groups for the upstream account pools, for example
   `OpenAI Paid`, `Claude Paid`, `Gemini Paid`, and `Grok Paid`.
2. Create a `composite` group with `subscription_type` set to `subscription`.
3. Assign provider accounts directly to the composite group, or copy accounts
   from the concrete provider groups during group creation.
4. Add explicit routes for public aliases that should not rely on built-in
   model detection:

   | Public model | Endpoint | Target platform | Upstream model |
   | --- | --- | --- | --- |
   | `all/gpt-5` | `responses` | `openai` | `gpt-5` |
   | `all/claude-sonnet` | `messages` | `anthropic` | `claude-sonnet-4-6` |
   | `all/gemini-pro` | `gemini` | `gemini` | `gemini-2.5-pro` |
   | `all/grok` | `responses` | `grok` | `grok-4.3` |

5. Configure channel pricing and model mapping under the concrete platforms
   named in each route. Composite routing does not create pricing records.
6. Create a subscription payment plan for the composite group.

The same composite group can also rely on built-in detection for standard model
names such as `gpt-*`, `claude-*`, `gemini-*`, and `grok-*`. Explicit routes are
recommended for bundled plan aliases because they make endpoint, provider, and
upstream model attribution reviewable in the admin UI.

## Limits

Composite routes choose a concrete provider and upstream model; they do not
create synthetic model metadata, pricing, or upstream capability records by
themselves. Keep channel pricing/model mapping configured for the concrete
provider platforms that the routes target.

This PR intentionally does not implement:

- Direct API-key binding to several existing groups without a composite group.
- Protocol-agnostic provider decoupling or a LiteLLM-style adapter rewrite.
