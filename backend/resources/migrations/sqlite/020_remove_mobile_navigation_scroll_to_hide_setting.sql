-- +goose Up
DELETE FROM settings WHERE key = 'mobileNavigationScrollToHide';

-- +goose Down
INSERT INTO settings (key, value) VALUES ('mobileNavigationScrollToHide', 'true');
