import type { MergeView } from '@codemirror/merge';
import { Compartment, EditorState, type Extension } from '@codemirror/state';
import { EditorView } from '@codemirror/view';
import { untrack } from 'svelte';
import type { Attachment } from 'svelte/attachments';

import type { CodeLanguage } from './analysis/types';

export type MergeAttachmentParams = {
	diffActive: boolean;
	language: CodeLanguage;
	value: string;
	baseline: string;
};

type CreateMergeHostAttachmentOptions = {
	getTheme: () => Extension;
	getLanguageExtension: (
		lang: CodeLanguage,
		options?: { lightweight?: boolean; languageExtensions?: Extension[] }
	) => Extension[];
	loadLanguageExtension: (lang: CodeLanguage) => Promise<Extension[]>;
	onValueChange: (nextValue: string) => void;
	onPrimaryViewReady: (view: EditorView) => void;
	onLoadError: (error: unknown) => void;
};

export function createMergeHostAttachment(
	options: CreateMergeHostAttachmentOptions,
	getParams: () => MergeAttachmentParams
): Attachment<HTMLDivElement> {
	const { getTheme, getLanguageExtension, loadLanguageExtension, onValueChange, onPrimaryViewReady, onLoadError } = options;

	return (node) => {
		let currentParams = untrack(getParams);
		let currentMergeView: MergeView | null = null;
		let destroyed = false;
		let createGeneration = 0;
		const themeCompartmentA = new Compartment();
		const themeCompartmentB = new Compartment();

		const destroyCurrentMergeView = () => {
			createGeneration += 1;
			if (!currentMergeView) return;
			currentMergeView.destroy();
			currentMergeView = null;
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

		const createCurrentMergeView = async () => {
			if (!currentParams.diffActive || currentMergeView) return;
			const generation = ++createGeneration;
			const language = currentParams.language;

			let MergeViewCtor: typeof MergeView;
			let languageExtensions: Extension[];
			try {
				const [mergeModule, loadedLanguage] = await Promise.all([import('@codemirror/merge'), loadLanguageExtension(language)]);
				MergeViewCtor = mergeModule.MergeView;
				languageExtensions = loadedLanguage;
			} catch (error) {
				if (!destroyed && generation === createGeneration && currentParams.diffActive) {
					onLoadError(error);
				}
				return;
			}
			if (destroyed || generation !== createGeneration || !currentParams.diffActive || currentMergeView) return;

			const theme = untrack(getTheme);
			const readonlyExtension = [EditorState.readOnly.of(true), EditorView.editable.of(false), themeCompartmentB.of(theme)];

			currentMergeView = new MergeViewCtor({
				parent: node,
				a: {
					doc: currentParams.value,
					extensions: [
						...getLanguageExtension(language, { lightweight: true, languageExtensions }),
						themeCompartmentA.of(theme),
						EditorView.updateListener.of((update) => {
							if (update.docChanged) {
								onValueChange(update.state.doc.toString());
							}
						})
					]
				},
				b: {
					doc: currentParams.baseline,
					extensions: [...languageExtensions, ...readonlyExtension]
				}
			});

			onPrimaryViewReady(currentMergeView.a);
			syncCurrentMergeView();
		};

		const applyParams = (nextParams: MergeAttachmentParams) => {
			const mustRecreate = Boolean(currentMergeView && nextParams.language !== currentParams.language);
			currentParams = nextParams;

			if (!currentParams.diffActive) {
				destroyCurrentMergeView();
				return;
			}

			if (mustRecreate) {
				destroyCurrentMergeView();
			}

			if (!currentMergeView) {
				void createCurrentMergeView();
				return;
			}

			syncCurrentMergeView();
		};

		untrack(() => applyParams(currentParams));

		$effect(() => {
			const nextParams = getParams();
			untrack(() => applyParams(nextParams));
		});

		$effect(() => {
			const theme = getTheme();
			untrack(() => {
				if (!currentMergeView) return;
				currentMergeView.a.dispatch({ effects: themeCompartmentA.reconfigure(theme) });
				currentMergeView.b.dispatch({ effects: themeCompartmentB.reconfigure(theme) });
			});
		});

		return () => {
			destroyed = true;
			destroyCurrentMergeView();
		};
	};
}
