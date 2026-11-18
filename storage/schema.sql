CREATE TABLE IF NOT EXISTS moodle_sessions
(
    telegram_user_id INTEGER PRIMARY KEY,
    moodle_user_id   TEXT NOT NULL,
    moodle_fullname TEXT NOT NULL,
    public_token     TEXT NOT NULL,
    private_token    TEXT NOT NULL
);