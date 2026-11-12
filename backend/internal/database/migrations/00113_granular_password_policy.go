package migrations

import (
	"github.com/pressly/goose/v3"
)

const (
	v113Up = `
INSERT INTO settings (key, value)
SELECT 'authPasswordMinLength', CASE value WHEN 'basic' THEN '8' WHEN 'standard' THEN '10' ELSE '12' END
FROM settings WHERE key = 'authPasswordPolicy'
ON CONFLICT (key) DO NOTHING;
INSERT INTO settings (key, value)
SELECT 'authPasswordRequireUppercase', CASE value WHEN 'basic' THEN 'false' ELSE 'true' END
FROM settings WHERE key = 'authPasswordPolicy'
ON CONFLICT (key) DO NOTHING;
INSERT INTO settings (key, value)
SELECT 'authPasswordRequireLowercase', CASE value WHEN 'basic' THEN 'false' ELSE 'true' END
FROM settings WHERE key = 'authPasswordPolicy'
ON CONFLICT (key) DO NOTHING;
INSERT INTO settings (key, value)
SELECT 'authPasswordRequireNumber', CASE value WHEN 'basic' THEN 'false' ELSE 'true' END
FROM settings WHERE key = 'authPasswordPolicy'
ON CONFLICT (key) DO NOTHING;
INSERT INTO settings (key, value)
SELECT 'authPasswordRequireSymbol', CASE WHEN value IN ('basic', 'standard') THEN 'false' ELSE 'true' END
FROM settings WHERE key = 'authPasswordPolicy'
ON CONFLICT (key) DO NOTHING;
DELETE FROM settings WHERE key = 'authPasswordPolicy';
`
	v113Down = `
INSERT INTO settings (key, value)
SELECT 'authPasswordPolicy', CASE
	WHEN p.req_symbol = 'true' OR p.min_length >= 12 THEN 'strong'
	WHEN p.req_upper = 'true' OR p.req_lower = 'true' OR p.req_number = 'true' OR p.min_length >= 10 THEN 'standard'
	ELSE 'basic'
END
FROM (
	SELECT
		COUNT(*) AS stored,
		CAST(COALESCE(MAX(CASE WHEN key = 'authPasswordMinLength' THEN value END), '12') AS INTEGER) AS min_length,
		COALESCE(MAX(CASE WHEN key = 'authPasswordRequireUppercase' THEN value END), 'true') AS req_upper,
		COALESCE(MAX(CASE WHEN key = 'authPasswordRequireLowercase' THEN value END), 'true') AS req_lower,
		COALESCE(MAX(CASE WHEN key = 'authPasswordRequireNumber' THEN value END), 'true') AS req_number,
		COALESCE(MAX(CASE WHEN key = 'authPasswordRequireSymbol' THEN value END), 'true') AS req_symbol
	FROM settings
	WHERE key IN ('authPasswordMinLength', 'authPasswordRequireUppercase', 'authPasswordRequireLowercase', 'authPasswordRequireNumber', 'authPasswordRequireSymbol')
) p
WHERE p.stored > 0
ON CONFLICT (key) DO NOTHING;
DELETE FROM settings WHERE key IN ('authPasswordMinLength', 'authPasswordRequireUppercase', 'authPasswordRequireLowercase', 'authPasswordRequireNumber', 'authPasswordRequireSymbol');
`
)

func init() {
	goose.AddMigrationContext(run(v113Up, v113Up), run(v113Down, v113Down))
}
