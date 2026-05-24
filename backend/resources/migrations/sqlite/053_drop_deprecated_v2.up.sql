-- v2.0.0: drop the Apprise notification service table and deprecated settings rows.
DROP TABLE IF EXISTS apprise_settings;

DELETE FROM settings WHERE key IN (
    'dockerPruneMode',
    'scheduledPruneContainers',
    'scheduledPruneImages',
    'scheduledPruneVolumes',
    'scheduledPruneNetworks',
    'scheduledPruneBuildCache',
    'authOidcConfig'
);
