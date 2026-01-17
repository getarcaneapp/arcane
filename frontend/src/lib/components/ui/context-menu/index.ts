import { ContextMenu as ContextMenuPrimitive } from 'bits-ui';
import Content from './context-menu-content.svelte';
import Item from './context-menu-item.svelte';
import Separator from './context-menu-separator.svelte';
import Group from './context-menu-group.svelte';

const Root = ContextMenuPrimitive.Root;
const Trigger = ContextMenuPrimitive.Trigger;

export {
	Content,
	Root as ContextMenu,
	Content as ContextMenuContent,
	Group as ContextMenuGroup,
	Item as ContextMenuItem,
	Separator as ContextMenuSeparator,
	Trigger as ContextMenuTrigger,
	Group,
	Item,
	Root,
	Separator,
	Trigger
};
