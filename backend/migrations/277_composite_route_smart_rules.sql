-- Composite smart routing: an explicit route can carry an ordered list of
-- request-content rules (images, tools, reasoning, input size, client,
-- keywords) that pick a different concrete platform/upstream model, and can
-- keep a client session on its first choice. Existing routes get no rules and
-- keep resolving to their own target exactly as before.

ALTER TABLE composite_model_routes
    ADD COLUMN IF NOT EXISTS rules JSONB NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE composite_model_routes
    ADD COLUMN IF NOT EXISTS session_affinity BOOLEAN NOT NULL DEFAULT TRUE;
