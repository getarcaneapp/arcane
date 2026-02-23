import { yaml } from '@codemirror/lang-yaml';
import { StreamLanguage, getIndentation, indentString } from '@codemirror/language';
import { properties } from '@codemirror/legacy-modes/mode/properties';
import { EditorState, type Extension } from '@codemirror/state';
import { MergeView } from '@codemirror/merge';
import { keymap, EditorView } from '@codemirror/view';
import type { Action } from 'svelte/action';
import type { CodeLanguage } from './analysis/types';

export type MergeActionParams = {
	diffActive: boolean;
	language: CodeLanguage;
	value: string;
	baseline: string;
};

type CreateMergeHostActionOptions = {
	getTheme: () => Extension;
	mergeEnterIndentKeymap: Extension;
	getLanguageExtension: (lang: CodeLanguage, options?: { lightweight?: boolean }) => Extension[];
	onValueChange: (nextValue: string) => void;
	onPrimaryViewReady: (view: EditorView) => void;
};

export function createMergeEnterIndentKeymap(): Extension {
	return keymap.of([
		{
			key: 'Enter',
			run(view) {
				const selection = view.state.selection.main;
				if (!selection) return false;

				const from = selection.from;
				const to = selection.to;
				const currentLine = view.state.doc.lineAt(from);
				const isCursorAtLineEnd = from === to && from === currentLine.to;
				const nextLine = currentLine.number < view.state.doc.lines ? view.state.doc.line(currentLine.number + 1) : null;
				const hasNextNonEmptyLine = Boolean(nextLine && nextLine.text.trim().length > 0);
				const lineBreakPos = from + 1;

				const lineBreakTransaction = view.state.update({
					changes: { from, to, insert: '\n' }
				});
				const lineBreakState = lineBreakTransaction.state;

				let indentation = '';

				if (isCursorAtLineEnd && hasNextNonEmptyLine) {
					indentation = nextLine?.text.match(/^\s*/)?.[0] ?? '';
				} else {
					const computedIndent = getIndentation(lineBreakState, lineBreakPos);
					if (computedIndent !== null) {
						indentation = indentString(lineBreakState, computedIndent);
					} else {
						const textBeforeCursor = currentLine.text.slice(0, Math.max(0, from - currentLine.from));
						indentation = textBeforeCursor.match(/^\s*/)?.[0] ?? '';
					}
				}

				view.dispatch({
					changes: { from, to, insert: `\n${indentation}` },
					selection: { anchor: lineBreakPos + indentation.length },
					userEvent: 'input'
				});

				return true;
			}
		}
	]);
}

export function createMergeHostAction(options: CreateMergeHostActionOptions): Action<HTMLDivElement, MergeActionParams> {
	const { getTheme, mergeEnterIndentKeymap, getLanguageExtension, onValueChange, onPrimaryViewReady } = options;

	return (node, params) => {
		let currentParams = params;
		let currentMergeView: MergeView | null = null;

		const destroyCurrentMergeView = () => {
			if (!currentMergeView) return;
			currentMergeView.destroy();
			currentMergeView = null;
		};

		const createCurrentMergeView = () => {
			if (!currentParams.diffActive || currentMergeView) return;

			const theme = getTheme();
			const readonlyExtension = [EditorState.readOnly.of(true), EditorView.editable.of(false), theme];

			currentMergeView = new MergeView({
				parent: node,
				a: {
					doc: currentParams.value,
					extensions: [
						...getLanguageExtension(currentParams.language, { lightweight: true }),
						mergeEnterIndentKeymap,
						theme,
						EditorView.updateListener.of((update) => {
							if (update.docChanged) {
								onValueChange(update.state.doc.toString());
							}
						})
					]
				},
				b: {
					doc: currentParams.baseline,
					extensions: [currentParams.language === 'yaml' ? yaml() : StreamLanguage.define(properties), ...readonlyExtension]
				}
			});

			onPrimaryViewReady(currentMergeView.a);
		};

		const syncCurrentMergeView = () => {
			if (!currentMergeView || !currentParams.diffActive) return;

			const currentLeft = currentMergeView.a.state.doc.toString();
			if (currentLeft !== currentParams.value) {
				currentMergeView.a.dispatch({
					changes: {
						from: 0,
						to: currentMergeView.a.state.doc.length,
						insert: currentParams.value
					}
				});
			}

			const currentRight = currentMergeView.b.state.doc.toString();
			if (currentRight !== currentParams.baseline) {
				currentMergeView.b.dispatch({
					changes: {
						from: 0,
						to: currentMergeView.b.state.doc.length,
						insert: currentParams.baseline
					}
				});
			}
		};

		const applyParams = (nextParams: MergeActionParams) => {
			const mustRecreate = Boolean(currentMergeView && nextParams.language !== currentParams.language);
			currentParams = nextParams;

			if (!currentParams.diffActive) {
				destroyCurrentMergeView();
				return;
			}

			if (mustRecreate) {
				destroyCurrentMergeView();
			}

			createCurrentMergeView();
			syncCurrentMergeView();
		};

		applyParams(params);

		return {
			update(nextParams: MergeActionParams) {
				applyParams(nextParams);
			},
			destroy() {
				destroyCurrentMergeView();
			}
		};
	};
}
