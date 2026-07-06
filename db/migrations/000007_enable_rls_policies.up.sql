-- Pattern 1: plain school isolation, applied to every table that only
-- needs school_id scoping. See Database Schema, Section 09, Pattern 1.

CREATE POLICY school_isolation ON schools
    FOR ALL USING (id = (current_setting('request.jwt.claims', true)::json ->> 'school_id')::uuid);

CREATE POLICY school_isolation ON academic_years
    FOR ALL USING (school_id = (current_setting('request.jwt.claims', true)::json ->> 'school_id')::uuid);

CREATE POLICY school_isolation ON user_profiles
    FOR ALL USING (school_id = (current_setting('request.jwt.claims', true)::json ->> 'school_id')::uuid);

CREATE POLICY school_isolation ON classes
    FOR ALL USING (school_id = (current_setting('request.jwt.claims', true)::json ->> 'school_id')::uuid);

CREATE POLICY school_isolation ON sections
    FOR ALL USING (school_id = (current_setting('request.jwt.claims', true)::json ->> 'school_id')::uuid);

CREATE POLICY school_isolation ON subjects
    FOR ALL USING (school_id = (current_setting('request.jwt.claims', true)::json ->> 'school_id')::uuid);

CREATE POLICY school_isolation ON class_subjects
    FOR ALL USING (school_id = (current_setting('request.jwt.claims', true)::json ->> 'school_id')::uuid);

CREATE POLICY school_isolation ON students
    FOR ALL USING (school_id = (current_setting('request.jwt.claims', true)::json ->> 'school_id')::uuid);

CREATE POLICY school_isolation ON guardians
    FOR ALL USING (school_id = (current_setting('request.jwt.claims', true)::json ->> 'school_id')::uuid);

CREATE POLICY school_isolation ON teacher_assignments
    FOR ALL USING (school_id = (current_setting('request.jwt.claims', true)::json ->> 'school_id')::uuid);

-- Pattern 2: school isolation + role-scoped write access.
-- See Database Schema, Section 09, Pattern 2.

CREATE POLICY attendance_read ON attendance_records
    FOR SELECT USING (
        school_id = (current_setting('request.jwt.claims', true)::json ->> 'school_id')::uuid
    );

CREATE POLICY attendance_write ON attendance_records
    FOR INSERT WITH CHECK (
        school_id = (current_setting('request.jwt.claims', true)::json ->> 'school_id')::uuid
        AND (
            (current_setting('request.jwt.claims', true)::json ->> 'role') IN ('principal', 'admin')
            OR EXISTS (
                SELECT 1 FROM teacher_assignments ta
                WHERE ta.section_id = attendance_records.section_id
                AND ta.teacher_id = (current_setting('request.jwt.claims', true)::json ->> 'sub')::uuid
            )
        )
    );

CREATE POLICY attendance_update ON attendance_records
    FOR UPDATE USING (
        school_id = (current_setting('request.jwt.claims', true)::json ->> 'school_id')::uuid
        AND (
            (current_setting('request.jwt.claims', true)::json ->> 'role') IN ('principal', 'admin')
            OR EXISTS (
                SELECT 1 FROM teacher_assignments ta
                WHERE ta.section_id = attendance_records.section_id
                AND ta.teacher_id = (current_setting('request.jwt.claims', true)::json ->> 'sub')::uuid
            )
        )
    );
