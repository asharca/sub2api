CREATE OR REPLACE FUNCTION public.try_parse_conversation_log_jsonb(input_text TEXT)
RETURNS JSONB
LANGUAGE plpgsql
IMMUTABLE
STRICT
AS $$
BEGIN
    RETURN input_text::jsonb;
EXCEPTION
    WHEN OTHERS THEN
        RETURN NULL;
END;
$$;
