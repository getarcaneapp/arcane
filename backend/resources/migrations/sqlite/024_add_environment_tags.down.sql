-- Drop environment_tags table and indexes
DROP INDEX IF EXISTS idx_environment_tags_tag;
DROP TABLE IF EXISTS environment_tags;

-- Drop environment filters table and indexes
DROP INDEX IF EXISTS idx_environment_filters_user_default;
DROP INDEX IF EXISTS idx_environment_filters_user_id;
DROP TABLE IF EXISTS environment_filters;
