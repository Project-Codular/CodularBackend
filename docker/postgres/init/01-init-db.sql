-- Log start of initialization
\echo 'Starting database initialization...'

-- Create the database if it doesn't exist
DO
$do$
BEGIN
    IF NOT EXISTS (
        SELECT FROM pg_database WHERE datname = 'project_codular_db'
    ) THEN
        CREATE DATABASE project_codular_db;
END IF;
END
$do$;

-- Connect to project_codular_db
\c project_codular_db postgres

-- Create the users table if it doesn't exist
CREATE TABLE IF NOT EXISTS users (
                                     id SERIAL PRIMARY KEY,
                                     email TEXT NOT NULL UNIQUE,
                                     password_hash TEXT NOT NULL,
                                     created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Create the tokens table if it doesn't exist
CREATE TABLE IF NOT EXISTS tokens (
                                      id SERIAL PRIMARY KEY,
                                      user_id INTEGER NOT NULL,
                                      token TEXT NOT NULL UNIQUE,
                                      type TEXT NOT NULL CHECK (type IN ('access', 'refresh')),
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
    );
CREATE INDEX idx_tokens_token ON tokens(token);

-- Create the programming_languages table if it doesn't exist
CREATE TABLE IF NOT EXISTS programming_languages (
                                                     id SERIAL PRIMARY KEY,
                                                     name TEXT NOT NULL UNIQUE
);

INSERT INTO programming_languages (name) VALUES ('Java'), ('Python'), ('C++')
    ON CONFLICT (name) DO NOTHING;

-- Create the tasks table if it doesn't exist
CREATE TABLE IF NOT EXISTS tasks (
    id SERIAL PRIMARY KEY,
    user_id INTEGER NOT NULL,
    type TEXT NOT NULL CHECK (type IN ('skips', 'noises')),
    taskCode TEXT NOT NULL,
    userOriginalCode TEXT,
    answers TEXT[] NOT NULL,
    programming_language_id INTEGER NOT NULL,
    created_at TIMESTAMP NOT NULL,
    public BOOLEAN NOT NULL DEFAULT FALSE,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (programming_language_id) REFERENCES programming_languages(id) ON DELETE RESTRICT,
    CHECK (
    (type = 'noises' AND array_length(answers, 1) = 1) OR
(type = 'skips' AND array_length(answers, 1) >= 1)
    )
    );

-- Create the aliases table if it doesn't exist
CREATE TABLE IF NOT EXISTS aliases (
                                       id SERIAL PRIMARY KEY,
                                       alias TEXT NOT NULL UNIQUE,
                                       task_id INTEGER NOT NULL,
                                       FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
    );

-- Create the submissions table if it doesn't exist
CREATE TABLE IF NOT EXISTS submissions (
    id SERIAL PRIMARY KEY,
    task_alias TEXT NOT NULL,
    submission_code TEXT[],
    status TEXT NOT NULL CHECK (status IN ('Pending', 'Success', 'Failed')),
    score INTEGER,
    hints TEXT[],
    submitted_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (task_alias) REFERENCES aliases(alias) ON DELETE CASCADE
    );

\echo 'Created tables'

-- Log completion
\echo 'Database initialization completed.'