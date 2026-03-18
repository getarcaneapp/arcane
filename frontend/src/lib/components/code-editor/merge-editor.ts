import { yaml } from '@codemirror/lang-yaml';
import { StreamLanguage } from '@codemirror/language';
import { properties } from '@codemirror/legacy-modes/mode/properties';
import { EditorState, type Extension } from '@codemirror/state';
import { MergeView } from '@codemirror/merge';
import { EditorView } from '@codemirror/view';
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
	getLanguageExtension: (lang: CodeLanguage, options?: { lightweight?: boolean }) => Extension[];
	onValueChange: (nextValue: string) => void;
	onPrimaryViewReady: (view: EditorView) => void;
};

export function createMergeHostAction(options: CreateMergeHostActionOptions): Action<HTMLDivElement, MergeActionParams> {
	const { getTheme, getLanguageExtension, onValueChange, onPrimaryViewReady } = options;

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
