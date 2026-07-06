-- Applies a WATI delivery-status webhook update to whatsapp_messages.
-- SECURITY DEFINER for the same reason as resolve_school_id (see
-- 000013): the WATI webhook is authenticated via HMAC signature, not a
-- Supabase JWT, so there is no request.jwt.claims session variable to
-- satisfy whatsapp_messages' school_isolation RLS policy (000012). This
-- mirrors the audit_log pattern in CLAUDE.md — a service-level write with
-- no end-user identity, scoped as narrowly as the schoolSlug resolver:
-- keyed only by wati_message_id (opaque, WATI-issued), touching only the
-- status/delivered_at/failure_reason columns.
CREATE FUNCTION update_whatsapp_message_status(
    p_wati_message_id text,
    p_status text,
    p_failure_reason text DEFAULT NULL
) RETURNS boolean
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
DECLARE
    v_count int;
BEGIN
    UPDATE whatsapp_messages
    SET status = p_status,
        delivered_at = CASE WHEN p_status = 'delivered' THEN now() ELSE delivered_at END,
        failure_reason = COALESCE(p_failure_reason, failure_reason)
    WHERE wati_message_id = p_wati_message_id;
    GET DIAGNOSTICS v_count = ROW_COUNT;
    RETURN v_count > 0;
END;
$$;

GRANT EXECUTE ON FUNCTION update_whatsapp_message_status(text, text, text) TO PUBLIC;
