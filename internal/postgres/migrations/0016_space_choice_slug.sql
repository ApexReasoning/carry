ALTER TABLE spaces
    ADD COLUMN slug text COLLATE "C";

UPDATE spaces
SET slug = replace(space_id::text, '-', '');

ALTER TABLE spaces
    ALTER COLUMN slug SET NOT NULL,
    ADD CONSTRAINT spaces_slug_length_check CHECK (
        char_length(slug) BETWEEN 1 AND 32
    ),
    ADD CONSTRAINT spaces_slug_unique UNIQUE (slug);

UPDATE carry_users
SET display_name = 'Member ' || left(replace(user_id::text, '-', ''), 8)
WHERE display_name IS NULL;

ALTER TABLE carry_users
    DROP CONSTRAINT carry_users_display_name_check,
    ALTER COLUMN display_name SET NOT NULL,
    ADD CONSTRAINT carry_users_display_name_check CHECK (
        btrim(display_name) <> ''
    );
