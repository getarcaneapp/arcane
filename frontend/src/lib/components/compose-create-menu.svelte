<script lang="ts">
	import { mergeProps } from 'bits-ui';

	import { ArcaneButton } from '#lib/components/arcane-button/index.js';
	import * as ArcaneTooltip from '#lib/components/arcane-tooltip/index.js';
	import IfPermitted from '#lib/components/if-permitted.svelte';
	import * as ButtonGroup from '#lib/components/ui/button-group/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import { Spinner } from '#lib/components/ui/spinner/index.js';
	import { TerminalIcon, TemplateIcon, AddIcon, ArrowDownIcon as ChevronDown, GitBranchIcon } from '#lib/icons/index.js';
	import { templateBtnClass } from '#lib/utils/compose-flow.js';
	import type { ShortcutKey } from '#lib/utils/navigation.js';

	interface Props {
		tooltipOpen: boolean | undefined;
		tooltipTitle: string;
		tooltipDescription: string;
		tooltipExample: string;
		tooltipVisible: boolean;

		showCreateButton?: boolean;
		createDisabled: boolean;
		createLoading: boolean;
		createLabel: string;
		createLoadingLabel: string;
		createShortcut?: ShortcutKey[];
		onCreate: () => void;

		itemsDisabled: boolean;
		showUseTemplate?: boolean;
		useTemplateLabel: string;
		onUseTemplate: () => void;
		convertLabel: string;
		onConvert: () => void;
		fromGitLabel: string;
		onFromGit: () => void | Promise<void>;

		createTemplateLabel: string;
		createTemplateDisabled: boolean;
		createTemplateLoading: boolean;
		onCreateTemplate: () => void;
		createTemplatePermission?: string;
	}

	let {
		tooltipOpen,
		tooltipTitle,
		tooltipDescription,
		tooltipExample,
		tooltipVisible,
		showCreateButton = true,
		createDisabled,
		createLoading,
		createLabel,
		createLoadingLabel,
		createShortcut,
		onCreate,
		itemsDisabled,
		showUseTemplate = true,
		useTemplateLabel,
		onUseTemplate,
		convertLabel,
		onConvert,
		fromGitLabel,
		onFromGit,
		createTemplateLabel,
		createTemplateDisabled,
		createTemplateLoading,
		onCreateTemplate,
		createTemplatePermission
	}: Props = $props();
</script>

{#snippet createTemplateItem()}
	<DropdownMenu.Item disabled={createTemplateDisabled} onclick={onCreateTemplate}>
		{#if createTemplateLoading}
			<Spinner class="size-4" />
		{:else}
			<AddIcon class="size-4" />
		{/if}
		{createTemplateLabel}
	</DropdownMenu.Item>
{/snippet}

<ButtonGroup.Root>
	{#if showCreateButton}
		<ArcaneTooltip.Root open={tooltipOpen}>
			<ArcaneTooltip.Trigger disabledChild={createDisabled || createLoading}>
				{#snippet child({ props })}
					{@const triggerProps = mergeProps(props, { onclick: onCreate })}
					<ArcaneButton
						{...triggerProps}
						action="create"
						tone="ghost"
						disabled={createDisabled}
						class={`${templateBtnClass} gap-2 rounded-r-none`}
						loading={createLoading}
						customLabel={createLabel}
						loadingLabel={createLoadingLabel}
						shortcut={createShortcut}
					/>
				{/snippet}
			</ArcaneTooltip.Trigger>
			<ArcaneTooltip.Content class="max-w-70">
				{#if tooltipVisible}
					<p class="mb-1 text-sm font-medium">{tooltipTitle}</p>
					<p class="text-xs text-muted-foreground">{tooltipDescription}</p>
					<p class="mt-1.5 inline-block rounded-xl bg-muted px-1.5 py-0.5 font-mono text-xs">
						{tooltipExample}
					</p>
				{/if}
			</ArcaneTooltip.Content>
		</ArcaneTooltip.Root>
	{/if}

	<DropdownMenu.Root>
		<DropdownMenu.Trigger>
			{#snippet child({ props })}
				<ArcaneButton
					{...props}
					action="base"
					tone="ghost"
					class={`${templateBtnClass} -ml-px rounded-l-none px-2`}
					icon={ChevronDown}
				/>
			{/snippet}
		</DropdownMenu.Trigger>
		<DropdownMenu.Content align="end" class="min-w-55">
			<DropdownMenu.Group>
				{#if showUseTemplate}
					<DropdownMenu.Item disabled={itemsDisabled} onclick={onUseTemplate}>
						<TemplateIcon class="size-4" />
						{useTemplateLabel}
					</DropdownMenu.Item>
				{/if}
				<DropdownMenu.Item onclick={onConvert}>
					<TerminalIcon class="size-4" />
					{convertLabel}
				</DropdownMenu.Item>
				<DropdownMenu.Item onclick={onFromGit}>
					<GitBranchIcon class="size-4" />
					{fromGitLabel}
				</DropdownMenu.Item>
				{#if createTemplatePermission}
					<IfPermitted perm={createTemplatePermission}>
						<DropdownMenu.Separator />
						{@render createTemplateItem()}
					</IfPermitted>
				{:else}
					<DropdownMenu.Separator />
					{@render createTemplateItem()}
				{/if}
			</DropdownMenu.Group>
		</DropdownMenu.Content>
	</DropdownMenu.Root>
</ButtonGroup.Root>
