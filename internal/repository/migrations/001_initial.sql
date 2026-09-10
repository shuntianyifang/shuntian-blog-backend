CREATE TABLE admins (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 singleton boolean NOT NULL DEFAULT true UNIQUE CHECK(singleton),
 username text NOT NULL UNIQUE,
 password_hash text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE sessions (
 token_hash text PRIMARY KEY,
 admin_id bigint NOT NULL REFERENCES admins(id),
 csrf_token text NOT NULL,
 expires_at timestamptz NOT NULL
);
CREATE INDEX sessions_expiry ON sessions(expires_at);
CREATE TABLE posts (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 slug text NOT NULL UNIQUE,
 current_draft_revision_id bigint,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE post_revisions (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 post_id bigint NOT NULL REFERENCES posts(id),
 title text NOT NULL,
 summary text NOT NULL,
 markdown text NOT NULL,
 safe_html text NOT NULL,
 published_at timestamptz NOT NULL,
 category jsonb NOT NULL,
 tags jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(post_id,id)
);
ALTER TABLE posts ADD CONSTRAINT current_revision_fk FOREIGN KEY(id,current_draft_revision_id) REFERENCES post_revisions(post_id,id);
CREATE TABLE publication_snapshots (
 id text PRIMARY KEY,
 idempotency_key text NOT NULL UNIQUE,
 request_hash text NOT NULL,
 manifest jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE FUNCTION reject_immutable_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'immutable record'; END;
$$;
CREATE TRIGGER immutable_revision BEFORE UPDATE OR DELETE ON post_revisions FOR EACH ROW EXECUTE FUNCTION reject_immutable_change();
CREATE TRIGGER immutable_snapshot BEFORE UPDATE OR DELETE ON publication_snapshots FOR EACH ROW EXECUTE FUNCTION reject_immutable_change();
