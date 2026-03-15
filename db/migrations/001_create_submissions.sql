CREATE TABLE IF NOT EXISTS submissions (
    username         VARCHAR(255) NOT NULL,
    timestamp        TIMESTAMP    NOT NULL,
    submission_count INT          NOT NULL DEFAULT 1,
    UNIQUE(username, timestamp)
);
