-- 异步对话日志：记录用户输入与模型输出的有界快照。

CREATE TABLE IF NOT EXISTS conversation_logs (
    id                  BIGSERIAL PRIMARY KEY,
    request_id          VARCHAR(128) NOT NULL DEFAULT '',
    response_id         VARCHAR(128) NOT NULL DEFAULT '',
    user_id             BIGINT REFERENCES users(id) ON DELETE SET NULL,
    api_key_id          BIGINT REFERENCES api_keys(id) ON DELETE SET NULL,
    account_id          BIGINT REFERENCES accounts(id) ON DELETE SET NULL,
    group_id            BIGINT REFERENCES groups(id) ON DELETE SET NULL,
    platform            VARCHAR(32) NOT NULL DEFAULT '',
    inbound_endpoint    VARCHAR(128) NOT NULL DEFAULT '',
    upstream_endpoint   VARCHAR(128) NOT NULL DEFAULT '',
    model               VARCHAR(255) NOT NULL DEFAULT '',
    requested_model     VARCHAR(255) NOT NULL DEFAULT '',
    upstream_model      VARCHAR(255) NOT NULL DEFAULT '',
    request_type        SMALLINT NOT NULL DEFAULT 0,
    stream              BOOLEAN NOT NULL DEFAULT FALSE,
    openai_ws_mode      BOOLEAN NOT NULL DEFAULT FALSE,
    status_code         INT NOT NULL DEFAULT 0,
    duration_ms         INT,
    first_token_ms      INT,
    input_tokens        INT NOT NULL DEFAULT 0,
    output_tokens       INT NOT NULL DEFAULT 0,
    cache_read_tokens   INT NOT NULL DEFAULT 0,
    cache_create_tokens INT NOT NULL DEFAULT 0,
    request_hash        VARCHAR(64) NOT NULL DEFAULT '',
    request_body        TEXT NOT NULL DEFAULT '',
    response_body       TEXT NOT NULL DEFAULT '',
    request_truncated   BOOLEAN NOT NULL DEFAULT FALSE,
    response_truncated  BOOLEAN NOT NULL DEFAULT FALSE,
    queue_delay_ms      INT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_conversation_logs_created_at ON conversation_logs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_conversation_logs_user_created_at ON conversation_logs(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_conversation_logs_api_key_created_at ON conversation_logs(api_key_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_conversation_logs_account_created_at ON conversation_logs(account_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_conversation_logs_group_created_at ON conversation_logs(group_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_conversation_logs_request_id ON conversation_logs(request_id);
