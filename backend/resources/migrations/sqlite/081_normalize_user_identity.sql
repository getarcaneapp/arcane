-- +goose Up
-- Trim the same Unicode whitespace as Go, then normalize existing identity text.
WITH whitespace(chars) AS (
    VALUES (char(9, 10, 11, 12, 13, 32, 133, 160, 5760,
                 8192, 8193, 8194, 8195, 8196, 8197, 8198, 8199, 8200, 8201, 8202,
                 8232, 8233, 8239, 8287, 12288))
)
UPDATE users
SET username = normalize(trim(username, whitespace.chars), 'nfc'),
    email = normalize(trim(email, whitespace.chars), 'nfc'),
    display_name = normalize(trim(display_name, whitespace.chars), 'nfc')
FROM whitespace;

-- +goose Down
-- Original text cannot be reconstructed; restore it from a pre-upgrade backup.
