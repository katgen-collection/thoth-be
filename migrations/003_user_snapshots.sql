-- Local cache of user identity, kept in sync via the `events.users` Redis
-- channel (UserCreated / UserUpdated) published by user-auth-service. Same
-- pattern as chat-service: the api-gateway is the source of truth for auth;
-- this is a denormalized read copy so thothai can resolve a user's profile
-- (name, avatar, role) without calling the auth service on every request.
--
-- Intentionally NOT foreign-keyed from cvs/conversations/etc: a user may act
-- before their snapshot arrives (cold start), so ownership stays a bare
-- user_id string — exactly as chat-service keeps it decoupled.
CREATE TABLE IF NOT EXISTS user_snapshots (
    id         VARCHAR(255) PRIMARY KEY,
    email      VARCHAR(255) NOT NULL DEFAULT '',
    fullname   VARCHAR(255) NOT NULL DEFAULT '',
    username   VARCHAR(255) NOT NULL DEFAULT '',
    avatar     TEXT,
    role       VARCHAR(50)  NOT NULL DEFAULT 'user',
    updated_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);
