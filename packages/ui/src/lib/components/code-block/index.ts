import Actions from './code-block-actions.svelte';
import Content from './code-block-content.svelte';
import Footer from './code-block-footer.svelte';
import Header from './code-block-header.svelte';
import Line, { codeBlockLineVariants, type CodeBlockLineToken, type CodeBlockLineVariant } from './code-block-line.svelte';
import Title from './code-block-title.svelte';
import Root, { codeBlockVariants, type CodeBlockSize, type CodeBlockVariant } from './code-block.svelte';

export {
	Root,
	Header,
	Title,
	Actions,
	Content,
	Line,
	Footer,
	codeBlockVariants,
	codeBlockLineVariants,
	type CodeBlockVariant,
	type CodeBlockSize,
	type CodeBlockLineVariant,
	type CodeBlockLineToken,
	//
	Root as CodeBlock,
	Header as CodeBlockHeader,
	Title as CodeBlockTitle,
	Actions as CodeBlockActions,
	Content as CodeBlockContent,
	Line as CodeBlockLine,
	Footer as CodeBlockFooter
};
