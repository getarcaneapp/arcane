import {
	createCssVariablesTheme,
	createHighlighterCore,
	type GrammarState,
	type HighlighterCore,
	type ThemedToken
} from 'shiki/core';
import { createJavaScriptRegexEngine } from 'shiki/engine/javascript';

// Add the languages you need. Each one loads the first time it is used.
const languages = {
	bash: () => import('shiki/langs/shellscript.mjs'),
	css: () => import('shiki/langs/css.mjs'),
	diff: () => import('shiki/langs/diff.mjs'),
	dockerfile: () => import('shiki/langs/dockerfile.mjs'),
	html: () => import('shiki/langs/html.mjs'),
	ini: () => import('shiki/langs/ini.mjs'),
	javascript: () => import('shiki/langs/javascript.mjs'),
	json: () => import('shiki/langs/json.mjs'),
	jsx: () => import('shiki/langs/jsx.mjs'),
	markdown: () => import('shiki/langs/markdown.mjs'),
	python: () => import('shiki/langs/python.mjs'),
	sql: () => import('shiki/langs/sql.mjs'),
	svelte: () => import('shiki/langs/svelte.mjs'),
	toml: () => import('shiki/langs/toml.mjs'),
	tsx: () => import('shiki/langs/tsx.mjs'),
	typescript: () => import('shiki/langs/typescript.mjs'),
	yaml: () => import('shiki/langs/yaml.mjs')
};

const aliases: Record<string, keyof typeof languages> = {
	env: 'ini',
	js: 'javascript',
	md: 'markdown',
	properties: 'ini',
	py: 'python',
	sh: 'bash',
	shell: 'bash',
	shellscript: 'bash',
	systemd: 'ini',
	ts: 'typescript',
	yml: 'yaml',
	zsh: 'bash'
};

const cssVariablesTheme = createCssVariablesTheme({
	name: 'arcane',
	variablePrefix: '--shiki-'
});

const arcaneTheme = {
	...cssVariablesTheme,
	tokenColors: [
		...(cssVariablesTheme.tokenColors ?? []),
		{
			scope: [
				'entity.name.tag.yaml',
				'support.type.property-name.json',
				'meta.object-literal.key',
				'variable.other.property',
				'variable.other.key.toml',
				'keyword.other.definition.ini'
			],
			settings: { foreground: 'var(--shiki-token-property)' }
		},
		{
			scope: ['entity.name.type', 'support.type', 'constant.numeric', 'constant.language'],
			settings: { foreground: 'var(--shiki-token-constant)' }
		},
		{
			scope: ['keyword.operator', 'constant.character.escape'],
			settings: { foreground: 'var(--shiki-token-operator)' }
		},
		{
			scope: [
				'punctuation.separator.key-value',
				'punctuation.separator.key-value.mapping.yaml',
				'punctuation.separator.dictionary.key-value.json',
				'punctuation.separator.dictionary.pair.json',
				'punctuation.separator.array.json',
				'punctuation.definition.dictionary.begin.json',
				'punctuation.definition.dictionary.end.json',
				'punctuation.definition.array.begin.json',
				'punctuation.definition.array.end.json',
				'punctuation.definition.block.sequence.item.yaml'
			],
			settings: { foreground: 'var(--shiki-token-punctuation)' }
		},
		{
			scope: ['invalid'],
			settings: { foreground: 'var(--shiki-token-invalid)' }
		}
	]
};

let highlighter: Promise<HighlighterCore> | undefined;

function getHighlighter() {
	highlighter ??= createHighlighterCore({
		themes: [arcaneTheme],
		langs: [],
		engine: createJavaScriptRegexEngine({ forgiving: true })
	});

	return highlighter;
}

function getLanguage(language: string) {
	const name = language.toLowerCase();

	if (name === 'ansi') {
		return name;
	}

	if (name in languages) {
		return name as keyof typeof languages;
	}

	return aliases[name] ?? null;
}

export type HighlightedToken = {
	content: string;
	class?: string;
};

// A line of plain text, or the highlighted tokens of a line.
export type HighlightedLine = string | HighlightedToken[];

const tokenColorClasses: Record<string, string> = {
	'var(--shiki-foreground)': 'text-foreground',
	'var(--shiki-token-comment)': 'text-(--syntax-comment)',
	'var(--shiki-token-keyword)': 'text-(--syntax-keyword)',
	'var(--shiki-token-string)': 'text-(--syntax-string)',
	'var(--shiki-token-string-expression)': 'text-(--syntax-string)',
	'var(--shiki-token-constant)': 'text-(--syntax-number)',
	'var(--shiki-token-function)': 'text-(--syntax-property)',
	'var(--shiki-token-parameter)': 'text-(--syntax-property)',
	'var(--shiki-token-punctuation)': 'text-muted-foreground',
	'var(--shiki-token-link)': 'text-(--syntax-operator)',
	'var(--shiki-token-property)': 'text-(--syntax-property)',
	'var(--shiki-token-operator)': 'text-(--syntax-operator)',
	'var(--shiki-token-inserted)': 'text-(--syntax-string)',
	'var(--shiki-token-deleted)': 'text-destructive',
	'var(--shiki-token-invalid)': 'text-destructive'
};

const fontStyleClasses: [number, string][] = [
	[1, 'italic'],
	[2, 'font-bold'],
	[4, 'underline'],
	[8, 'line-through']
];

function tokenClass(token: ThemedToken) {
	const fontStyle = Math.max(token.fontStyle ?? 0, 0);

	return [
		tokenColorClasses[token.color ?? ''],
		...fontStyleClasses.filter(([flag]) => (fontStyle & flag) !== 0).map(([, className]) => className)
	]
		.filter(Boolean)
		.join(' ');
}

function toHighlightedLine(tokens: ThemedToken[]): HighlightedToken[] {
	return tokens.map((token) => ({
		content: token.content,
		class: tokenClass(token)
	}));
}

export type Highlighter = NonNullable<Awaited<ReturnType<typeof loadHighlighter>>>;

// Loads a language and returns a highlighter for it, or null for a language
// that is not listed.
export async function loadHighlighter(language: string) {
	const lang = getLanguage(language);

	if (!lang) {
		return null;
	}

	const shiki = await getHighlighter();

	if (lang !== 'ansi' && !shiki.getLoadedLanguages().includes(lang)) {
		await shiki.loadLanguage(languages[lang]);
	}

	return {
		// Returns the tokens of each line, and the grammar state after the last
		// line. Pass that state back to continue where the code left off.
		highlight(code: string, state?: GrammarState) {
			const tokenize = (text: string) =>
				shiki.codeToTokens(text, {
					lang,
					theme: 'arcane',
					grammarState: state
				}).tokens;

			// ANSI has no grammar state, so each line is tokenized on its own and
			// gets the same colors whether it arrives in one piece or streams.
			const tokens = lang === 'ansi' ? code.split('\n').flatMap((line) => tokenize(line)) : tokenize(code);

			return {
				lines: tokens.map((line) => toHighlightedLine(line)),
				state: shiki.getLastGrammarState(tokens)
			};
		}
	};
}

// Returns the tokens of each line, or null for a language that is not listed.
export async function highlightCode(code: string, language: string) {
	const highlighter = await loadHighlighter(language);

	return highlighter ? highlighter.highlight(code).lines : null;
}
