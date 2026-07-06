-- Plain school-isolation policy, applied to every table added since
-- migration 000007. See Database Schema doc, Section 09, Pattern 1.

CREATE POLICY school_isolation ON fee_structures
    FOR ALL USING (school_id = (current_setting('request.jwt.claims', true)::json ->> 'school_id')::uuid);

CREATE POLICY school_isolation ON fee_payments
    FOR ALL USING (school_id = (current_setting('request.jwt.claims', true)::json ->> 'school_id')::uuid);

CREATE POLICY school_isolation ON fee_reminders_log
    FOR ALL USING (school_id = (current_setting('request.jwt.claims', true)::json ->> 'school_id')::uuid);

CREATE POLICY school_isolation ON ai_generations
    FOR ALL USING (school_id = (current_setting('request.jwt.claims', true)::json ->> 'school_id')::uuid);

CREATE POLICY school_isolation ON ai_usage_quota
    FOR ALL USING (school_id = (current_setting('request.jwt.claims', true)::json ->> 'school_id')::uuid);

CREATE POLICY school_isolation ON whatsapp_messages
    FOR ALL USING (school_id = (current_setting('request.jwt.claims', true)::json ->> 'school_id')::uuid);

CREATE POLICY school_isolation ON ptm_slots
    FOR ALL USING (school_id = (current_setting('request.jwt.claims', true)::json ->> 'school_id')::uuid);

CREATE POLICY school_isolation ON ptm_bookings
    FOR ALL USING (school_id = (current_setting('request.jwt.claims', true)::json ->> 'school_id')::uuid);

CREATE POLICY school_isolation ON timetable_slots
    FOR ALL USING (school_id = (current_setting('request.jwt.claims', true)::json ->> 'school_id')::uuid);

CREATE POLICY school_isolation ON substitutions
    FOR ALL USING (school_id = (current_setting('request.jwt.claims', true)::json ->> 'school_id')::uuid);

-- audit_log gets NO general school_isolation policy — see
-- audit_no_client_write in migration 000011. The Go backend writes to
-- it using a service role that bypasses RLS (BYPASSRLS), per the
-- Database Schema doc's "Pattern 3: no direct access" note.
