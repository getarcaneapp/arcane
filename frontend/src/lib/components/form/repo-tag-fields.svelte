<script lang="ts">
	import FormInput from '$lib/components/form/form-input.svelte';
	import { m } from '$lib/paraglide/messages';
	import type { FormInput as FormInputType } from '$lib/utils/settings';

	interface Props {
		repository: FormInputType<string | boolean | number | Date | undefined>;
		tag: FormInputType<string | boolean | number | Date | undefined>;
	}

	let { repository = $bindable(), tag = $bindable() }: Props = $props();
</script>

<FormInput label={m.resource_repository_cap()} placeholder={m.images_tag_repository_placeholder()} bind:input={repository} />
<FormInput label={m.tag()} placeholder={m.images_tag_latest()} bind:input={tag} />
