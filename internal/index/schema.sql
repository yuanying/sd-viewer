CREATE TABLE IF NOT EXISTS images (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    root          TEXT    NOT NULL,
    path          TEXT    NOT NULL,
    dir           TEXT    NOT NULL DEFAULT '',
    name          TEXT    NOT NULL DEFAULT '',
    size          INTEGER NOT NULL DEFAULT 0,
    mtime         INTEGER NOT NULL DEFAULT 0,
    width         INTEGER NOT NULL DEFAULT 0,
    height        INTEGER NOT NULL DEFAULT 0,
    created_at    INTEGER NOT NULL DEFAULT 0,
    has_params    INTEGER NOT NULL DEFAULT 0,
    prompt        TEXT    NOT NULL DEFAULT '',
    negative      TEXT    NOT NULL DEFAULT '',
    model         TEXT    NOT NULL DEFAULT '',
    model_hash    TEXT    NOT NULL DEFAULT '',
    sampler       TEXT    NOT NULL DEFAULT '',
    schedule_type TEXT    NOT NULL DEFAULT '',
    steps         INTEGER NOT NULL DEFAULT 0,
    cfg_scale     REAL    NOT NULL DEFAULT 0,
    seed          INTEGER NOT NULL DEFAULT 0,
    denoising     REAL    NOT NULL DEFAULT 0,
    version       TEXT    NOT NULL DEFAULT '',
    gen_width     INTEGER NOT NULL DEFAULT 0,
    gen_height    INTEGER NOT NULL DEFAULT 0,
    extras        TEXT    NOT NULL DEFAULT '',
    raw           TEXT    NOT NULL DEFAULT '',
    -- ゴミ箱へ入れた日時。0 ならゴミ箱の外にある。
    trashed_at    INTEGER NOT NULL DEFAULT 0,
    -- ゴミ箱へ入れる前のパス。戻し先として使う。
    orig_path     TEXT    NOT NULL DEFAULT '',
    -- Fav にした日時。0 なら Fav ではない。
    fav_at        INTEGER NOT NULL DEFAULT 0,
    UNIQUE (root, path)
);

CREATE INDEX IF NOT EXISTS images_created_at  ON images (created_at DESC);
CREATE INDEX IF NOT EXISTS images_model       ON images (model);
CREATE INDEX IF NOT EXISTS images_sampler     ON images (sampler);
CREATE INDEX IF NOT EXISTS images_dir         ON images (dir);
CREATE INDEX IF NOT EXISTS images_root        ON images (root);

CREATE TABLE IF NOT EXISTS image_loras (
    image_id INTEGER NOT NULL REFERENCES images (id) ON DELETE CASCADE,
    seq      INTEGER NOT NULL,
    name     TEXT    NOT NULL,
    weight   REAL    NOT NULL DEFAULT 0,
    hash     TEXT    NOT NULL DEFAULT '',
    PRIMARY KEY (image_id, seq)
);

CREATE INDEX IF NOT EXISTS image_loras_name ON image_loras (name);

-- kind: 0 = positive prompt, 1 = negative prompt
CREATE TABLE IF NOT EXISTS image_tags (
    image_id INTEGER NOT NULL REFERENCES images (id) ON DELETE CASCADE,
    kind     INTEGER NOT NULL,
    seq      INTEGER NOT NULL,
    tag      TEXT    NOT NULL,
    PRIMARY KEY (image_id, kind, seq)
);

CREATE INDEX IF NOT EXISTS image_tags_tag ON image_tags (tag, kind);

CREATE VIRTUAL TABLE IF NOT EXISTS images_fts USING fts5 (
    prompt,
    negative,
    content = 'images',
    content_rowid = 'id'
);

CREATE TRIGGER IF NOT EXISTS images_fts_insert AFTER INSERT ON images BEGIN
    INSERT INTO images_fts (rowid, prompt, negative)
    VALUES (new.id, new.prompt, new.negative);
END;

CREATE TRIGGER IF NOT EXISTS images_fts_delete AFTER DELETE ON images BEGIN
    INSERT INTO images_fts (images_fts, rowid, prompt, negative)
    VALUES ('delete', old.id, old.prompt, old.negative);
END;

CREATE TRIGGER IF NOT EXISTS images_fts_update AFTER UPDATE ON images BEGIN
    INSERT INTO images_fts (images_fts, rowid, prompt, negative)
    VALUES ('delete', old.id, old.prompt, old.negative);
    INSERT INTO images_fts (rowid, prompt, negative)
    VALUES (new.id, new.prompt, new.negative);
END;
