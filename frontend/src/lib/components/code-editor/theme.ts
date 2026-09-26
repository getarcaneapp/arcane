import { tags as t } from '@lezer/highlight';
import { EditorView } from '@codemirror/view';
import type { Extension } from '@codemirror/state';
import { createTheme } from '@uiw/codemirror-themes';

function accentWithAlpha(alpha: number): string {
	return `color-mix(in oklab, var(--primary) ${Math.round(alpha * 100)}%, transparent)`;
}

// Panels, tooltips, completions and search UI are not covered by createTheme.
const chromeTheme = EditorView.theme({
	'.cm-panels': {
		backgroundColor: 'var(--popover)',
		color: 'var(--popover-foreground)'
	},
	'.cm-panels-top': { borderBottom: '1px solid var(--border)' },
	'.cm-panels-bottom': { borderTop: '1px solid var(--border)' },
	'.cm-textfield': {
		backgroundColor: 'var(--background)',
		color: 'var(--foreground)',
		border: '1px solid var(--input)',
		borderRadius: '0.25rem'
	},
	'.cm-button': {
		backgroundImage: 'none',
		backgroundColor: 'var(--secondary)',
		color: 'var(--secondary-foreground)',
		border: '1px solid var(--border)',
		borderRadius: '0.25rem',
		'&:active': { backgroundImage: 'none', backgroundColor: 'var(--accent)' }
	},
	'.cm-tooltip': {
		backgroundColor: 'var(--popover)',
		color: 'var(--popover-foreground)',
		border: '1px solid var(--border)',
		borderRadius: '0.375rem'
	},
	'.cm-tooltip-section:not(:first-child)': { borderTop: '1px solid var(--border)' },
	'.cm-tooltip-autocomplete ul li[aria-selected]': {
		backgroundColor: accentWithAlpha(0.2),
		color: 'var(--popover-foreground)'
	},
	'.cm-completionMatchedText': { color: 'var(--primary)', textDecoration: 'none', fontWeight: '600' },
	'.cm-searchMatch': {
		backgroundColor: accentWithAlpha(0.25),
		'& span': { outline: `1px solid ${accentWithAlpha(0.6)}` }
	},
	'.cm-searchMatch.cm-searchMatch-selected': { backgroundColor: accentWithAlpha(0.45) },
	'.cm-collapsedLines': {
		color: 'var(--muted-foreground)',
		background: 'linear-gradient(to bottom, transparent 0, var(--muted) 30%, var(--muted) 70%, transparent 100%)'
	},
	'.cm-foldPlaceholder': {
		backgroundColor: 'var(--muted)',
		color: 'var(--muted-foreground)',
		border: '1px solid var(--border)'
	}
});

export function createArcaneTheme(dark: boolean): Extension {
	return [
		createTheme({
			theme: dark ? 'dark' : 'light',
			settings: {
				background: 'var(--background)',
				foreground: 'var(--foreground)',
				caret: 'var(--primary)',
				selection: accentWithAlpha(0.35),
				selectionMatch: accentWithAlpha(0.15),
				lineHighlight: accentWithAlpha(0.05),
				gutterBackground: 'var(--background)',
				gutterForeground: 'var(--muted-foreground)',
				gutterActiveForeground: 'var(--foreground)',
				gutterBorder: 'transparent',
				fontFamily:
					'"Mona Sans Mono", ui-monospace, SFMono-Regular, Menlo, Consolas, "Liberation Mono", "Courier New", monospace',
				fontSize: '13px'
			},
			styles: [
				{ tag: [t.comment, t.meta], color: 'var(--syntax-comment)' },
				{ tag: [t.keyword, t.modifier, t.operatorKeyword], color: 'var(--syntax-keyword)' },
				{ tag: [t.typeName, t.namespace, t.number, t.atom, t.bool], color: 'var(--syntax-number)' },
				{
					tag: [
						t.className,
						t.definition(t.variableName),
						t.propertyName,
						t.attributeName,
						t.function(t.variableName),
						t.labelName
					],
					color: 'var(--syntax-property)'
				},
				{ tag: [t.variableName, t.name], color: 'var(--foreground)' },
				{ tag: [t.string, t.inserted, t.regexp, t.special(t.string)], color: 'var(--syntax-string)' },
				{ tag: [t.operator, t.url, t.link, t.escape], color: 'var(--syntax-operator)' },
				{ tag: [t.separator, t.punctuation], color: 'var(--muted-foreground)' },
				{ tag: t.heading, color: 'var(--foreground)', fontWeight: 'bold' },
				{ tag: t.strong, fontWeight: 'bold' },
				{ tag: t.emphasis, fontStyle: 'italic' },
				{ tag: t.strikethrough, textDecoration: 'line-through' },
				{ tag: t.invalid, color: 'var(--destructive)' },
				{ tag: t.link, textDecoration: 'underline' }
			]
		}),
		chromeTheme
	];
}
