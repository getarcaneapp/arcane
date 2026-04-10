import { getIndentUnit, indentString } from '@codemirror/language';
import { EditorState, Prec, type Extension } from '@codemirror/state';
import { EditorView } from '@codemirror/view';
import type { CodeLanguage } from './analysis/types';

function getYamlIndent(state: EditorState, textBeforeCursor: string, currentIndent: string, isCursorAtLineEnd: boolean): string {
	if (!isCursorAtLineEnd) return currentIndent;

	const trimmedBeforeCursor = textBeforeCursor.trimEnd();
	const startsYamlBlock =
		/:\s*(?:#.*)?$/.test(trimmedBeforeCursor) ||
		/:\s*[|>][-+0-9]*\s*(?:#.*)?$/.test(trimmedBeforeCursor) ||
		/^-\s*(?:#.*)?$/.test(trimmedBeforeCursor.trimStart());

	if (startsYamlBlock) {
		return indentString(state, currentIndent.length + getIndentUnit(state));
	}

	return currentIndent;
}

export function createEnterIndentKeymap(language: CodeLanguage): Extension {
	return Prec.highest(
		EditorView.domEventHandlers({
			keydown(event, view) {
				if (
					event.key !== 'Enter' ||
					event.defaultPrevented ||
					event.isComposing ||
					event.altKey ||
					event.ctrlKey ||
					event.metaKey ||
					view.state.facet(EditorState.readOnly)
				) {
					return false;
				}

				const selection = view.state.selection.main;
				if (!selection) return false;

				const from = selection.from;
				const to = selection.to;
				const currentLine = view.state.doc.lineAt(from);
				const isCursorAtLineEnd = from === to && from === currentLine.to;
				const currentIndent = currentLine.text.match(/^\s*/)?.[0] ?? '';
				const textBeforeCursor = currentLine.text.slice(0, Math.max(0, from - currentLine.from));
				const lineBreakPos = from + 1;

				let indentation: string;

				if (language === 'yaml') {
					indentation = getYamlIndent(view.state, textBeforeCursor, currentIndent, isCursorAtLineEnd);
				} else {
					indentation = textBeforeCursor.match(/^\s*/)?.[0] ?? '';
				}

				event.preventDefault();
				view.dispatch({
					changes: { from, to, insert: `\n${indentation}` },
					selection: { anchor: lineBreakPos + indentation.length },
					userEvent: 'input'
				});

				return true;
			}
		})
	);
}
