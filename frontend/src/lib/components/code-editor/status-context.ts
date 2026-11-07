import { createContext } from 'svelte';

import type { DiagnosticSummary } from './analysis/types';

export type EditorStatus = DiagnosticSummary & {
	diagnostics: number;
	validating: boolean;
};

export type EditorStatusHost = {
	current: (() => EditorStatus) | null;
};

export const [getEditorStatusHost, setEditorStatusHost, hasEditorStatusHost] = createContext<EditorStatusHost>();
