import { Events, type CancellablePromise } from "@wailsio/runtime";
import { ServerService, type WineSetupResult } from "../../bindings/github.com/diablo2org/launcher/internal/app";
import type { Progress } from "../../bindings/github.com/diablo2org/launcher/internal/wine";
import { bytes, errorText } from "./format";

export type RowState = "pending" | "active" | "done" | "failed";
export type Row = { label: string; state: RowState; detail: string };

// Setting Wine up on macOS and Linux: the runtime, then the prefix. Shared by
// the welcome screen's install list and the setup page Play opens when Wine
// isn't ready.
export class WineSetup {
  running = $state(false);
  done = $state(false);
  error = $state("");
  needsRosetta = $state(false);
  installingRosetta = $state(false);
  progress = $state<Progress | null>(null);

  private call: CancellablePromise<WineSetupResult> | null = null;
  private off: (() => void) | null = null;

  // Runs the setup; true once Wine is ready.
  async run(): Promise<boolean> {
    this.running = true;
    this.error = "";
    this.needsRosetta = false;
    this.progress = null;
    this.off = Events.On("wine:progress", (ev: { data: Progress }) => {
      this.progress = ev.data;
    });

    let result: WineSetupResult;
    try {
      this.call = ServerService.SetupWine();
      result = await this.call;
    } catch (err) {
      // Cancelling rejects the call.
      result = { ok: false, cancelled: true, needsRosetta: false, error: "" };
    } finally {
      this.call = null;
      this.off?.();
      this.off = null;
      this.running = false;
    }

    this.needsRosetta = result.needsRosetta;
    this.error = result.needsRosetta ? "" : result.error;
    this.done = result.ok;
    return result.ok;
  }

  cancel() {
    this.call?.cancel();
  }

  // Installs Rosetta through macOS's own prompt, then carries on.
  async installRosetta(): Promise<boolean> {
    this.installingRosetta = true;
    this.error = "";
    try {
      await ServerService.InstallRosetta();
    } catch (err) {
      this.error = errorText(err);
      return false;
    } finally {
      this.installingRosetta = false;
    }
    return this.run();
  }

  get rows(): Row[] {
    const p = this.progress;
    const failed = !this.running && (!!this.error || this.needsRosetta);
    const atPrefix = this.done || p?.step === "prefix";

    let wineDetail = "";
    if (this.needsRosetta) wineDetail = "Needs Rosetta";
    else if (!atPrefix && p?.step === "wine" && p.total > 0) wineDetail = `${bytes(p.done)} of ${bytes(p.total)}`;

    return [
      {
        label: "Wine",
        state: atPrefix ? "done" : failed ? "failed" : this.running ? "active" : "pending",
        detail: wineDetail,
      },
      {
        label: "Wine settings",
        state: this.done ? "done" : failed && atPrefix ? "failed" : this.running && atPrefix ? "active" : "pending",
        detail: this.running && atPrefix ? "First time: about a minute" : "",
      },
    ];
  }
}
