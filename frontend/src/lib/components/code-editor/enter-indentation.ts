import { getIndentUnit, getIndentation, indentString } from '@codemirror/language';
import { EditorState, Prec, type Extension } from '@codemirror/state';
import { EditorView } from '@codemirror/view';
import type { CodeLanguage } from './analysis/types';

function getYamlHeuristicIndent(
	lineBreakState: EditorState,
	textBeforeCursor: string,
	currentIndent: string,
	isCursorAtLineEnd: boolean
): string | null {
	if (!isCursorAtLineEnd) return null;

	const trimmedBeforeCursor = textBeforeCursor.trimEnd();
	const startsYamlBlock =
		/:\s*(?:#.*)?$/.test(trimmedBeforeCursor) ||
		/:\s*[|>][-+0-9]*\s*(?:#.*)?$/.test(trimmedBeforeCursor) ||
		/^-\s*(?:#.*)?$/.test(trimmedBeforeCursor);

	if (!startsYamlBlock) return null;

	return indentString(lineBreakState, currentIndent.length + getIndentUnit(lineBreakState));
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
					const textBeforeCursor = currentLine.text.slice(0, Math.max(0, from - currentLine.from));
					const yamlHeuristicIndent =
						language === 'yaml'
							? getYamlHeuristicIndent(lineBreakState, textBeforeCursor, currentIndent, isCursorAtLineEnd)
							: null;

					if (computedIndent !== null) {
						indentation = indentString(lineBreakState, computedIndent);
					}

					if (yamlHeuristicIndent && yamlHeuristicIndent.length > indentation.length) {
						indentation = yamlHeuristicIndent;
					} else if (computedIndent === null) {
						indentation = textBeforeCursor.match(/^\s*/)?.[0] ?? '';
					}
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
