import type { Diagnostic } from '@codemirror/lint';

export type CodeLanguage = 'yaml' | 'env';

export type SchemaStatus = 'ready' | 'cached' | 'unavailable';

export type EditorContext = {
	envContent?: string;
	composeContents?: string[];
	globalVariables?: Record<string, string>;
};

export type DiagnosticSummary = {
	errors: number;
	warnings: number;
	infos: number;
	hints: number;
	schemaStatus: SchemaStatus;
	schemaMessage?: string;
	unresolvedVariables: string[];
	secretWarnings: number;
	duplicateKeyWarnings: number;
	duplicateEnvWarnings: number;
	cursorLine: number;
	cursorCol: number;
	validationReady: boolean;
};

export type OutlineItem = {
	id: string;
	label: string;
	path: Array<string | number>;
	from: number;
	to: number;
	level: number;
};

export type SchemaDoc = {
	title?: string;
	description?: string;
	defaultValue?: string;
	examples?: string[];
};

export type AnalysisResult = {
	diagnostics: Diagnostic[];
	outlineItems: OutlineItem[];
	summaryPatch: Partial<DiagnosticSummary>;
};
