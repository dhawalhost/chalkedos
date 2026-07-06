CREATE TABLE whatsapp_messages (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id         uuid NOT NULL REFERENCES schools(id),
    student_id        uuid REFERENCES students(id),
    guardian_id       uuid REFERENCES guardians(id),
    template          text NOT NULL CHECK (template IN ('absence_alert','fee_reminder','broadcast','ptm_confirmation')),
    language          text NOT NULL,
    body_preview      text,
    status            text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','sent','delivered','read','failed')),
    wati_message_id   text,
    sent_at           timestamptz,
    delivered_at      timestamptz,
    failure_reason    text,
    created_at        timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE whatsapp_messages ENABLE ROW LEVEL SECURITY;
CREATE INDEX idx_whatsapp_messages_school_student ON whatsapp_messages(school_id, student_id, created_at);
CREATE INDEX idx_whatsapp_messages_wati_id ON whatsapp_messages(wati_message_id);

CREATE TABLE ptm_slots (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id    uuid NOT NULL REFERENCES schools(id),
    teacher_id   uuid NOT NULL REFERENCES user_profiles(id),
    slot_time    timestamptz NOT NULL,
    is_booked    boolean NOT NULL DEFAULT false,
    created_at   timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE ptm_slots ENABLE ROW LEVEL SECURITY;

CREATE TABLE ptm_bookings (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    school_id       uuid NOT NULL REFERENCES schools(id),
    ptm_slot_id     uuid NOT NULL REFERENCES ptm_slots(id) UNIQUE,
    student_id      uuid NOT NULL REFERENCES students(id),
    guardian_id     uuid NOT NULL REFERENCES guardians(id),
    booked_at       timestamptz NOT NULL DEFAULT now(),
    reminder_sent   boolean NOT NULL DEFAULT false
);
ALTER TABLE ptm_bookings ENABLE ROW LEVEL SECURITY;
