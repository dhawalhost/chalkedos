-- Returns the profile fields the login response needs for a just-verified
-- user. SECURITY DEFINER for the same reason as resolve_school_id (000013):
-- at OTP-verify time the backend has no request.jwt.claims session variable
-- yet, so user_profiles' school_isolation RLS policy (000007) would hide the
-- row. Narrow by construction: keyed by the auth user id (which the caller
-- can only obtain from a Supabase-verified OTP session), returns one row,
-- only login-relevant columns, and only for active profiles.
CREATE FUNCTION get_login_profile(p_user_id uuid)
RETURNS TABLE (
    school_id uuid,
    school_slug text,
    school_name text,
    role text,
    full_name text,
    language_pref text
)
LANGUAGE sql
SECURITY DEFINER
SET search_path = public
STABLE
AS $$
    SELECT up.school_id, s.slug, s.name, up.role, up.full_name, up.language_pref
    FROM user_profiles up
    JOIN schools s ON s.id = up.school_id
    WHERE up.id = p_user_id AND up.is_active;
$$;

GRANT EXECUTE ON FUNCTION get_login_profile(uuid) TO PUBLIC;
