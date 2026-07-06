-- Resolves a school's slug to its id without requiring the
-- request.jwt.claims session variable to already be set — needed by the
-- cross-tenant guard in internal/middleware/auth.go, which must check the
-- JWT's school_id against the URL's :schoolSlug *before* any RLS claim
-- exists for the request.
--
-- SECURITY DEFINER (not a broadened RLS policy on `schools`) so the
-- function returns only a single id for a given slug, not the school's
-- full row (name, subscription_tier, subscription_status, phone, etc).
-- Existing policies (see 000007_enable_rls_policies.up.sql) apply to
-- whatever role the app connects as — no `TO <role>` restriction — so this
-- grants to PUBLIC to match that pattern rather than assuming Supabase's
-- `authenticated`/`anon` roles, which this app's direct pgx connection
-- does not actually authenticate as.
CREATE FUNCTION resolve_school_id(p_slug text)
RETURNS uuid
LANGUAGE sql
SECURITY DEFINER
SET search_path = public
STABLE
AS $$
    SELECT id FROM schools WHERE slug = p_slug;
$$;

-- SQL functions are EXECUTE-able by PUBLIC by default; stated explicitly
-- here so the grant isn't just an implicit default.
GRANT EXECUTE ON FUNCTION resolve_school_id(text) TO PUBLIC;
