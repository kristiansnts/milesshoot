import { writable } from "svelte/store";
import { api, onEvent } from "./api";

export const status = writable<any>(null);
export const config = writable<any>(null);
export const logs = writable<any[]>([]);
export const hires = writable<any>(null);

// FotoTree & rentang yang sedang dilihat di layar Ranking — dipisah dari
// FotoTree tujuan unggah (Pengaturan → Tujuan unggah) supaya bisa cek ranking
// event mana pun tanpa mengubah tujuan unggah aktif. Disimpan di store (bukan
// state lokal komponen) supaya tidak reset saat pindah tab lalu kembali.
export const rankingTree = writable<any>(null);
export const rankingRange = writable<string>("today");

let started = false;
export function initStores() {
  if (started) return;
  started = true;
  api.status().then((s) => status.set(s)).catch(() => {});
  api.getConfig().then((c) => config.set(c)).catch(() => {});

  onEvent("status", (s) => status.set(s));
  onEvent("change", () => {
    api.status().then((s) => status.set(s)).catch(() => {});
  });
  onEvent("hires", (h) => hires.set(h));
  api.hiresState().then((h) => h && hires.set(h)).catch(() => {});

  onEvent("log", (l) => {
    if (!l) return;
    logs.update((xs) => [l, ...xs].slice(0, 200));
  });
}

export async function refreshConfig() {
  const c = await api.getConfig();
  config.set(c);
  return c;
}
