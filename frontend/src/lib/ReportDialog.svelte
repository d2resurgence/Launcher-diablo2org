<script lang="ts">
  import { SupportService } from "../../bindings/github.com/diablo2org/launcher/internal/app";
  import type { SendResult } from "../../bindings/github.com/diablo2org/launcher/internal/app";
  import type { Item } from "../../bindings/github.com/diablo2org/launcher/internal/report";
  import reportIcon from "../assets/ui/link-icon-report.png";
  import { bytes, errorText } from "./format";
  import { app } from "./state.svelte";

  // A bug report for the open server. What goes in is listed before anything
  // leaves: a server with a report URL gets it sent, with only its own files;
  // otherwise it's saved as a zip for the player to pass on.
  const info = $derived(app.current);
  const profile = $derived(info?.profile);
  const sends = $derived(!!profile?.report?.url);

  let items = $state<Item[] | null>(null);
  let host = $state("");
  let message = $state("");
  let contact = $state("");
  let showFiles = $state(false);
  let sending = $state(false);
  let error = $state("");
  let sent = $state<SendResult | null>(null);
  let saved = $state("");

  const total = $derived((items ?? []).reduce((n, it) => n + it.size, 0));

  async function load() {
    error = "";
    try {
      const sr = sends && info ? await SupportService.ServerReport(info.id) : null;
      host = sr?.host ?? "";
      items = sr ? (sr.items ?? []) : ((await SupportService.ReportContents()) ?? []);
    } catch (err) {
      error = errorText(err);
    }
  }
  load();

  async function send() {
    sending = true;
    error = "";
    try {
      sent = await SupportService.SendReport(info!.id, message, contact);
    } catch (err) {
      error = errorText(err);
    } finally {
      sending = false;
    }
  }

  async function save() {
    error = "";
    try {
      const path = await SupportService.SaveReport();
      if (path) saved = path;
    } catch (err) {
      error = errorText(err);
    }
  }

  async function showSaved(path: string) {
    try {
      await SupportService.ShowSavedReport(path);
    } catch (err) {
      error = errorText(err);
    }
  }

  // Closing mid-send would lose the result, so the dialog stays until it's in.
  function close() {
    if (!sending) app.reportOpen = false;
  }
</script>

<svelte:window onkeydown={(e) => e.key === "Escape" && close()} />

<div class="fixed inset-0 z-40 flex items-start justify-center bg-[rgba(3,2,2,0.78)] pt-14" role="presentation" onclick={(e) => e.target === e.currentTarget && close()}>
  <div class="frame-dialog relative w-[580px] px-[30px] pt-1 pb-6" role="dialog" aria-modal="true" aria-labelledby="report-title">
    <button class="absolute top-0.5 right-0.5 p-1.5 text-muted hover:text-gold disabled:opacity-45" aria-label="Close" disabled={sending} onclick={close}>
      <svg width="14" height="14" viewBox="0 0 14 14" stroke="currentColor" stroke-width="1.6" aria-hidden="true"><path d="M3 3l8 8M11 3l-8 8" /></svg>
    </button>

    <div class="mt-[18px] flex items-center gap-3.5">
      <span class="link-tile !h-10 !w-10" aria-hidden="true"><span class="link-icon" style:--mask={`url(${reportIcon})`}></span></span>
      <div class="min-w-0">
        <h2 id="report-title" class="title text-[19px] !text-title">Report a problem</h2>
        <p class="mt-0.5 truncate text-[13px] text-body">
          {#if sends}
            Sent to the {profile?.name} team{host ? " at " : ""}<span class="text-[#d9c49a]">{host}</span>
          {:else}
            Saved as a zip, for you to send to {profile?.name ?? "the server"}'s team
          {/if}
        </p>
      </div>
    </div>

    {#if sent && !sent.error}
      <p class="mt-6 text-[14px] text-parchment" role="status">
        Report sent{sent.id ? "." : ""}
        {#if sent.id}Your reference is <span class="text-gold">{sent.id}</span>; quote it if you ask about it.{/if}
        {#if sent.message}<span class="text-body"> {sent.message}</span>{/if}
      </p>
      <div class="mt-5 flex justify-end">
        <button class="btn-stone primary h-[38px] px-6 text-[12px]" onclick={close}>DONE</button>
      </div>
    {:else if saved}
      <p class="mt-6 text-[14px] text-parchment" role="status">Saved to <span class="text-gold">{saved}</span>.</p>
      <div class="mt-5 flex justify-end gap-3.5">
        <button class="btn-stone h-[38px] px-5 text-[12px]" onclick={() => showSaved(saved)}>SHOW FILE</button>
        <button class="btn-stone primary h-[38px] px-6 text-[12px]" onclick={close}>DONE</button>
      </div>
    {:else}
      {#if sends}
        <label for="report-what" class="title mt-[22px] mb-1.5 block text-[12px] tracking-[0.08em] !text-[#c9b48a]">WHAT HAPPENED?</label>
        <textarea
          id="report-what"
          class="field h-[104px] w-full resize-none px-3 py-2.5 text-[13.5px] leading-[1.45]"
          placeholder="What were you doing when it went wrong?"
          maxlength="2000"
          bind:value={message}
        ></textarea>

        <label for="report-contact" class="title mt-3.5 mb-1.5 block text-[12px] tracking-[0.08em] !text-[#c9b48a]">
          YOUR DISCORD NAME <span class="font-sans font-normal tracking-normal text-muted">(optional)</span>
        </label>
        <input id="report-contact" class="field h-9 w-[260px] px-3 text-[13.5px]" placeholder="So the team can reply" maxlength="100" bind:value={contact} />
      {:else}
        <p class="mt-5 text-[13.5px] leading-[1.45] text-body">
          The launcher's logs and Diablo II's crash logs are saved as a zip. Nothing is sent anywhere: you choose where to save it and who to give it to.
        </p>
      {/if}

      <div class="mt-4 border border-[#33291b] bg-[rgba(5,4,3,0.55)] px-3 py-2.5">
        {#if items === null && !error}
          <p class="text-[13px] text-muted">Gathering the files…</p>
        {:else if items}
          <button class="flex w-full items-center gap-2 text-left text-[13px] text-[#c9b48a] hover:text-title" aria-expanded={showFiles} onclick={() => (showFiles = !showFiles)}>
            <svg class="transition-transform" class:rotate-90={showFiles} width="10" height="10" viewBox="0 0 10 10" fill="currentColor" aria-hidden="true"><path d="M3 1.5l4 3.5-4 3.5z" /></svg>
            <span class="flex-1">{items.length} {items.length === 1 ? "file goes" : "files go"} with it</span>
            <span class="text-muted">{bytes(total)}</span>
          </button>
          {#if showFiles}
            <ul class="mt-2 ml-[18px] flex max-h-28 flex-col gap-0.5 overflow-y-auto pr-1 text-[12px]">
              {#each items as it (it.name)}
                <li class="flex gap-3">
                  <span class="min-w-0 flex-1 truncate text-parchment">{it.name}</span>
                  <span class="text-muted">{it.what}</span>
                  <span class="w-14 text-right text-muted">{bytes(it.size)}</span>
                </li>
              {/each}
            </ul>
          {/if}
          <p class="mt-1.5 ml-[18px] text-[12px] text-muted">
            Your Windows user folder is replaced with %USERPROFILE%.{host ? " Large files are cut to their newest part to fit." : ""}
          </p>
        {/if}
      </div>

      {#if sent?.error}
        <div class="mt-4 text-[13px]" role="alert">
          <p class="text-bad">Couldn't send the report: {sent.error}</p>
          {#if sent.savedTo}
            <p class="mt-1 text-body">
              It was saved to {sent.savedTo}, so you can send it to {profile?.name} yourself.
              <button class="text-gold underline decoration-gold-line underline-offset-2 hover:text-[#fff1c8]" onclick={() => showSaved(sent!.savedTo)}>Show file</button>
            </p>
          {/if}
        </div>
      {/if}
      {#if error}
        <p class="mt-4 text-[13px] text-bad" role="alert">{error}</p>
      {/if}

      <div class="mt-5 flex justify-end gap-3.5">
        <button class="btn-stone h-[38px] px-[22px] text-[12px]" disabled={sending} onclick={close}>CANCEL</button>
        {#if sends}
          <button class="btn-stone primary h-[38px] px-[26px] text-[12px]" disabled={sending || !items} onclick={send}>{sending ? "SENDING…" : "SEND REPORT"}</button>
        {:else}
          <button class="btn-stone primary h-[38px] px-[26px] text-[12px]" disabled={!items} onclick={save}>SAVE REPORT</button>
        {/if}
      </div>
    {/if}
  </div>
</div>
