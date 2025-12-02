/** @type {import('i18n-unused').RunOptions} */
module.exports = {
	// Locale files config
	localesPath: 'messages',
	localesExtensions: ['json'],
	// localeNameResolver: (name) => name === 'en.json', // Only check source locale

	// Source files config
	srcPath: 'src',
	srcExtensions: ['js', 'ts', 'svelte'],
	ignorePaths: ['src/lib/paraglide'], // Ignore generated Paraglide files

	// Paraglide pattern: m.key_name() or m.key_name({ param })
	translationKeyMatcher: /\bm\.([a-zA-Z_][a-zA-Z0-9_]*)\s*\(/g,

	// Exclude metadata keys (Crowdin comments, JSON schema)
	excludeKey: ['_comment', '$schema'],

	// Your JSON uses flat keys (key_name not nested.key.name)
	flatTranslations: true,

	// Ignore comments in source files
	ignoreComments: true,

	// Formatting for any JSON modifications
	localeJsonStringifyIndent: '\t'
};
