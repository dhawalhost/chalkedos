-- Public PTM booking endpoints are authenticated by a signed token, not a
-- JWT (parents never log in — F-07), so no request.jwt.claims exists and
-- the school_isolation policies on ptm_slots/ptm_bookings would hide
-- everything. Same SECURITY DEFINER pattern as resolve_school_id (000013):
-- narrow inputs, narrow outputs, school scoping enforced by parameter.

-- Open slots for a school, with the teacher's name for display.
CREATE FUNCTION list_open_ptm_slots(p_school_id uuid)
RETURNS TABLE (id uuid, teacher_name text, slot_time timestamptz)
LANGUAGE sql
SECURITY DEFINER
SET search_path = public
STABLE
AS $$
    SELECT ps.id, up.full_name, ps.slot_time
    FROM ptm_slots ps
    JOIN user_profiles up ON up.id = ps.teacher_id
    WHERE ps.school_id = p_school_id
      AND NOT ps.is_booked
      AND ps.slot_time > now()
    ORDER BY ps.slot_time;
$$;

-- Books a slot atomically. The UNIQUE constraint on
-- ptm_bookings.ptm_slot_id is the real double-booking guard (F-07) — the
-- unique_violation handler turns the race loser into NULL, which the Go
-- handler maps to 409. Returns NULL too when the slot doesn't exist in
-- this school or already flagged booked.
CREATE FUNCTION book_ptm_slot(
    p_slot_id uuid,
    p_school_id uuid,
    p_student_id uuid,
    p_guardian_id uuid
) RETURNS uuid
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public
AS $$
DECLARE
    v_booking_id uuid;
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM ptm_slots
        WHERE id = p_slot_id AND school_id = p_school_id AND NOT is_booked
    ) THEN
        RETURN NULL;
    END IF;

    BEGIN
        INSERT INTO ptm_bookings (school_id, ptm_slot_id, student_id, guardian_id)
        VALUES (p_school_id, p_slot_id, p_student_id, p_guardian_id)
        RETURNING id INTO v_booking_id;
    EXCEPTION WHEN unique_violation THEN
        RETURN NULL;
    END;

    UPDATE ptm_slots SET is_booked = true WHERE id = p_slot_id;
    RETURN v_booking_id;
END;
$$;

GRANT EXECUTE ON FUNCTION list_open_ptm_slots(uuid) TO PUBLIC;
GRANT EXECUTE ON FUNCTION book_ptm_slot(uuid, uuid, uuid, uuid) TO PUBLIC;
