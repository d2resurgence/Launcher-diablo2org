<script lang="ts">
  import { Browser } from "@wailsio/runtime";
  import type { Links } from "../../bindings/github.com/diablo2org/launcher/internal/spec";
  import LinkTile from "./LinkTile.svelte";

  let { links }: { links: Links | undefined } = $props();

  // Every link the profile gives, in the spec's order.
  const labels: [keyof Links, string][] = [
    ["website", "Website"],
    ["discord", "Discord"],
    ["forum", "Forum"],
    ["wiki", "Wiki"],
    ["trade", "Trade"],
    ["register", "Register"],
    ["support", "Support"],
    ["donate", "Donate"],
  ];

  const shown = $derived(labels.filter(([key]) => links?.[key]).map(([key, label]) => ({ key, label, url: links![key]! })));

  // Up to four to a column, bottom-aligned; more start a second column.
  const rows = $derived(Math.min(4, shown.length));

  // The hover text says where a link goes, since the server chose it.
  function host(url: string): string {
    try {
      return new URL(url).host;
    } catch {
      return url;
    }
  }
</script>

{#if shown.length}
  <nav class="grid grid-flow-col content-end gap-x-8 gap-y-2" style:grid-template-rows={`repeat(${rows}, auto)`} aria-label="Server links">
    {#each shown as l (l.key)}
      <LinkTile kind={l.key} label={l.label} title={`Opens ${host(l.url)}`} onclick={() => Browser.OpenURL(l.url)} />
    {/each}
  </nav>
{/if}
