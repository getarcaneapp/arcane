import PaneGroup from './resizable-pane-group.svelte';
import Pane from './resizable-pane.svelte';
import Handle from './resizable-handle.svelte';

export { 
	type PaneConfig, 
	type ResizableGroupState,
	ResizableGroup,
	getResizableGroupContext,
	setResizableGroupContext
} from './resizable.svelte';

export const Resizable = {
	PaneGroup,
	Pane,
	Handle
};
