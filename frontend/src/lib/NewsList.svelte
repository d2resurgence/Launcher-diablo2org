<script lang="ts">
  import { Browser } from "@wailsio/runtime";
  import type { ServerData } from "./state.svelte";
  import Icon from "./Icon.svelte";

  let { data }: { data: ServerData } = $props();

  const format = new Intl.DateTimeFormat("en-GB", { day: "2-digit", month: "short", year: "numeric" });

  function date(value: unknown): string {
    const d = new Date(String(value));
    return isNaN(d.getTime()) || d.getFullYear() < 2000 ? "" : format.format(d).toUpperCase();
  }
</script>

<div class="flex min-h-0 flex-col px-5 pt-3.5 pb-4">
  <h2 class="title mb-3 text-[13px] tracking-[0.14em] !text-gold">NEWS</h2>
  <div class="min-h-0 overflow-y-auto pr-1">
    {#if data.news === null && !data.newsError}
      <p class="text-[13px] text-body">Loading news…</p>
    {:else if data.newsError && !data.news?.length}
      <p class="text-[13px] text-body">News is unavailable right now.</p>
    {:else if !data.news?.length}
      <p class="text-[13px] text-body">No news yet.</p>
    {:else}
      <ul class="divide-y divide-gold-line/30">
        {#each data.news as item, i (i)}
          <li class="py-3 first:pt-0 last:pb-0">
            {#if date(item.date)}
              <p class="text-[11px] tracking-[0.08em] text-date">{date(item.date)}</p>
            {/if}
            {#if item.url}
              <button class="title mt-0.5 flex items-start gap-1.5 text-left text-[14px] !text-parchment hover:!text-[#fff0c4]" onclick={() => Browser.OpenURL(item.url)}>
                <span>{item.title}</span>
                <span class="mt-0.5 shrink-0 opacity-40"><Icon name="out" size={12} /></span>
              </button>
            {:else}
              <h3 class="title mt-0.5 text-[14px] !text-parchment">{item.title}</h3>
            {/if}
            {#if item.summary}
              <p class="mt-1.5 text-[13px] leading-[1.45] text-body">{item.summary}</p>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}
  </div>
</div>
