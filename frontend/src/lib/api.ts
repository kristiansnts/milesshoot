// Satu lapisan untuk dua lingkungan:
//  - Desktop (Wails): panggil binding window.go.main.App.* dan dengar event window.runtime.
//  - HP (server HTTP): fetch /api/* dan SSE /api/events.
// Perintah native (pilih folder, login) hanya ada di desktop; di HP dikembalikan null.

type AnyObj = Record<string, any>;

function desktop(): AnyObj | undefined {
  return (window as any)?.go?.main?.App;
}
export function isDesktop(): boolean {
  return !!desktop();
}

async function get(url: string) {
  const r = await fetch(url);
  return r.json();
}
async function post(url: string, body?: any) {
  const r = await fetch(url, {
    method: "POST",
    headers: body != null ? { "Content-Type": "application/json" } : undefined,
    body: body != null ? (typeof body === "string" ? body : JSON.stringify(body)) : undefined,
  });
  return r.ok ? r.json().catch(() => null) : null;
}

export const api = {
  isDesktop,
  status: () => (desktop() ? desktop()!.Status() : get("/api/status")),
  getConfig: () => (desktop() ? desktop()!.GetConfig() : get("/api/config")),
  updateConfig: (patch: string) => (desktop() ? desktop()!.UpdateConfig(patch) : post("/api/config", patch)),
  pause: () => (desktop() ? desktop()!.Pause() : post("/api/pause")),
  resume: () => (desktop() ? desktop()!.Resume() : post("/api/resume")),
  retryFailed: () => desktop()?.RetryFailed?.(),
  pickFolder: () => desktop()?.PickFolder?.() ?? Promise.resolve(null),
  clearFolder: () => desktop()?.ClearFolder?.() ?? Promise.resolve(null),
  // Folder sumber tambahan (multi-folder, unggah konkuren)
  addWatchFolder: () => desktop()?.AddWatchFolder?.() ?? Promise.resolve(null),
  removeWatchFolder: (p: string) => desktop()?.RemoveWatchFolder?.(p) ?? Promise.resolve(null),
  login: (id: string, pw: string) => desktop()?.Login?.(id, pw) ?? Promise.reject("hanya di desktop"),
  logout: () => desktop()?.Logout?.(),
  searchTrees: (name: string) => desktop()?.SearchTrees?.(name) ?? Promise.resolve([]),
  selectTree: (t: any) => desktop()?.SelectTree?.(t),
  account: () => (desktop() ? desktop()!.Account() : get("/api/account")),
  // Multi-akun FotoYu (tersimpan lokal, bukan database) — dropdown "Akun" di layar Unggah.
  accounts: () => desktop()?.Accounts?.() ?? Promise.resolve([]),
  switchAccount: (id: string) => desktop()?.SwitchAccount?.(id) ?? Promise.reject("hanya di desktop"),
  removeAccount: (id: string) => desktop()?.RemoveAccount?.(id) ?? Promise.resolve([]),
  storage: () => desktop()?.Storage?.() ?? Promise.resolve(null),
  // Latensi (ms) ke server FotoYu — kartu "Internet" di layar Unggah.
  ping: () => (desktop() ? desktop()!.Ping?.() : get("/api/ping").then((r: any) => r?.ms)) as Promise<number>,
  fee: (price: number, tagID: string) => desktop()?.Fee?.(price, tagID) ?? Promise.resolve(null),
  // Info FTP (termasuk password) hanya tersedia di desktop — sengaja tidak lewat /api jaringan.
  ftpInfo: () => desktop()?.FTPInfo?.() ?? Promise.resolve(null),
  restartFTP: () => desktop()?.RestartFTP?.() ?? Promise.resolve(null),
  // Uji saringan terhadap folder sumber tanpa menyimpan (patch = JSON setelan calon).
  // Hi-res (foto terjual menunggu file asli)
  hiresState: () => desktop()?.HiresState?.() ?? Promise.resolve(null),
  hiresRefresh: () => desktop()?.HiresRefresh?.() ?? Promise.resolve(null),
  hiresSend: (id: string) => desktop()?.HiresSend?.(id) ?? Promise.resolve(null),
  hiresSendAll: () => desktop()?.HiresSendAll?.() ?? Promise.resolve(null),
  hiresAddFolder: () => desktop()?.HiresAddFolder?.() ?? Promise.resolve(null),
  hiresRemoveFolder: (p: string) => desktop()?.HiresRemoveFolder?.(p) ?? Promise.resolve(null),
  hiresSetAuto: (on: boolean) => desktop()?.HiresSetAuto?.(on) ?? Promise.resolve(null),
  // Watermark (teks/logo)
  watermarkPickLogo: () => desktop()?.WatermarkPickLogo?.() ?? Promise.resolve(null),
  // Pratinjau hidup watermark (gaya MAS EDY). Desktop: binding Wails balikin byte JPEG
  // sebagai array angka (bukan base64) — caller yang bungkus jadi Blob URL.
  // HP: balik URL gambar langsung (bisa dipakai sebagai <img src>), server yang merender.
  watermarkPreview: (patch: string) => desktop()?.WatermarkPreview?.(patch) ?? Promise.resolve(null),
  watermarkPreviewUrl: (wm: any) => "/api/watermark/preview?wm=" + encodeURIComponent(JSON.stringify(wm)) + "&t=" + Date.now(),
  // Pustaka LUT (.cube)
  listLuts: () => desktop()?.ListLUTs?.() ?? Promise.resolve([]),
  importLut: () => desktop()?.ImportLUT?.() ?? Promise.resolve(null),
  deleteLut: (name: string) => desktop()?.DeleteLUT?.(name) ?? Promise.resolve(null),
  setLut: (role: string, path: string) => desktop()?.SetLUT?.(role, path) ?? Promise.resolve(null),
  // Kamera kabel (PTP/tethering)
  usbCamState: () => desktop()?.UsbCamState?.() ?? Promise.resolve(null),
  usbCamToggle: (on: boolean) => desktop()?.UsbCamToggle?.(on) ?? Promise.resolve(null),
  // Ranking (peringkat daun per akun di sebuah FotoTree)
  rankingStart: (treeId: string, name: string, range: string) => desktop()?.RankingStart?.(treeId, name, range) ?? Promise.resolve(null),
  rankingStatus: (treeId: string, range: string) => desktop()?.RankingStatus?.(treeId, range) ?? Promise.resolve(null),
  rankingCancel: (treeId: string, range: string) => desktop()?.RankingCancel?.(treeId, range),
  // Kartu memori / card reader
  cardDrives: () => desktop()?.CardDrives?.() ?? Promise.resolve([]),
  useCardDrive: (p: string) => desktop()?.UseCardDrive?.(p) ?? Promise.resolve(null),
  // Log (debug sambungan kamera)
  logTail: (n: number) => desktop()?.LogTail?.(n) ?? Promise.resolve([]),
  logFile: () => desktop()?.LogFile?.() ?? Promise.resolve(""),
  openLogFolder: () => desktop()?.OpenLogFolder?.(),
  phoneURLs: () => desktop()?.PhoneURLs?.() ?? Promise.resolve([]),
};

// ---- event ----
let _es: EventSource | null = null;
function sharedES(): EventSource {
  if (!_es) _es = new EventSource("/api/events");
  return _es;
}

type Handler = (data: any) => void;

export function onEvent(name: string, cb: Handler): () => void {
  const d = (window as any)?.runtime;
  if (desktop() && d?.EventsOn) {
    d.EventsOn(name, cb);
    return () => d.EventsOff?.(name);
  }
  const es = sharedES();
  const l = (e: MessageEvent) => cb(e.data ? JSON.parse(e.data) : null);
  es.addEventListener(name, l as any);
  return () => es.removeEventListener(name, l as any);
}
