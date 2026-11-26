<script>
  import { createEventDispatcher, onMount } from "svelte";
  import { status, logs, config } from "../lib/stores";
  import { api, isDesktop } from "../lib/api";

  const go = createEventDispatcher();
  const ubah = (tab) => go("goto", { screen: "pengaturan", tab });

  $: st = $status;
  $: cfg = $config;
  $: acct = st && st.account;
  $: fyOn = acct && acct.loggedIn;
  $: c = (st && st.counts) || { done: 0, pending: 0, uploading: 0, failed: 0, duplicate: 0 };
  // Reset tampilan: simpan angka dasar saat tombol Reset ditekan, lalu kurangkan. Hanya
  // tampilan — riwayat/antrean tidak disentuh (riwayat dipakai untuk deteksi duplikat).
  const BASE_KEY = "fotonaik.unggah.base";
  const loadBase = () => {
    try { return JSON.parse(localStorage.getItem(BASE_KEY)) || { done: 0, failed: 0, duplicate: 0 }; }
    catch { return { done: 0, failed: 0, duplicate: 0 }; }
  };
  let base = loadBase();
  $: shown = {
    done: Math.max(0, c.done - base.done),
    failed: Math.max(0, c.failed - base.failed),
    duplicate: Math.max(0, c.duplicate - base.duplicate),
  };
  function resetStats() {
    base = { done: c.done, failed: c.failed, duplicate: c.duplicate };
    try { localStorage.setItem(BASE_KEY, JSON.stringify(base)); } catch {}
    peak = { antre: 1, gagal: 1, permin: 1, mbps: 1, eta: 1 };
  }
  $: denom = shown.done + c.pending + c.uploading + shown.failed;
  $: pct = denom > 0 ? Math.round((shown.done / denom) * 100) : 0;
  $: circ = 2 * Math.PI * 42;
  $: paused = st && st.paused;
  $: ev = (cfg && cfg.event) || {};
  $: tree = ev.tree;
  $: scan = (st && st.scan) || {};
  $: toFotoyu = (st && st.destination) === "fotoyu";

  // ---- progres kecepatan (bukan grafik riwayat — cuma angka hidup, gaya MAS EDY) ----
  $: speed = (st && st.speed) || { mbps: 0, perMin: 0, etaSec: -1, elapsedSec: 0 };
  $: antreCount = c.pending + c.uploading;

  // ring tiap metrik diisi relatif ke nilai tertinggi yang pernah terlihat sesi ini
  // (MAS EDY menyebutnya "peak") — supaya ring selalu punya arti meski tanpa batas atas tetap.
  let peak = { antre: 1, gagal: 1, permin: 1, mbps: 1, eta: 1 };
  $: peak.antre = Math.max(peak.antre, antreCount);
  $: peak.gagal = Math.max(peak.gagal, shown.failed);
  $: peak.permin = Math.max(peak.permin, speed.perMin);
  $: peak.mbps = Math.max(peak.mbps, speed.mbps);
  $: peak.eta = speed.etaSec > 0 ? Math.max(peak.eta, speed.etaSec) : peak.eta;

  const ringPct = (v, max) => Math.max(0, Math.min(100, Math.round((v / Math.max(max, 1e-6)) * 100)));
  const fmtDur = (sec) => {
    sec = Math.max(0, Math.round(sec || 0));
    const m = Math.floor(sec / 60), s = sec % 60;
    return m + ":" + String(s).padStart(2, "0");
  };
  const fmtEta = (sec) => (sec == null || sec < 0 ? "—" : fmtDur(sec));
  const folderBase = (p) => (p ? p.split(/[\\/]/).filter(Boolean).pop() : "");

  $: bannerTone = paused ? "var(--warn)" : antreCount > 0 ? "var(--good)" : "var(--warn)";
  $: bannerTitle = paused ? "Dijeda" : antreCount > 0 ? "Sedang mengunggah" : "Tidak ada yang diunggah";
  $: bannerSub = paused
    ? "Unggah otomatis sedang dimatikan."
    : antreCount > 0
    ? `${antreCount} file di antrean · ${speed.perMin} foto/menit`
    : cfg && cfg.watchFolder
    ? `Folder ${folderBase(cfg.watchFolder)}: tidak ada file baru untuk diunggah.`
    : "Belum ada folder sumber dipilih.";

  $: rings = [
    { label: "terunggah", text: shown.done.toLocaleString("id-ID"), unit: "file", color: "var(--good)", pct },
    { label: "antre", text: antreCount.toLocaleString("id-ID"), unit: "file", color: "var(--warn)", pct: ringPct(antreCount, peak.antre) },
    { label: "gagal", text: String(shown.failed), unit: "file", color: "var(--bad)", pct: ringPct(shown.failed, peak.gagal) },
    { label: "foto / menit", text: String(speed.perMin), unit: "per menit", color: "var(--ci-but)", pct: ringPct(speed.perMin, peak.permin) },
    { label: "kecepatan", text: speed.mbps.toLocaleString("id-ID"), unit: "Mbps", color: "var(--accent)", pct: ringPct(speed.mbps, peak.mbps) },
    { label: "sisa waktu", text: fmtEta(speed.etaSec), unit: "", color: "var(--ci-lil)", pct: speed.etaSec < 0 ? 0 : 100 - ringPct(speed.etaSec, peak.eta) },
    { label: "lama upload", text: fmtDur(speed.elapsedSec), unit: "", color: "var(--muted)", pct: ringPct(speed.elapsedSec, 1800) },
  ];

  async function patch(obj) {
    const c = await api.updateConfig(JSON.stringify(obj));
    if (c) config.set(c);
  }
  const rp = (n) => "Rp" + (Number(n) || 0).toLocaleString("id-ID");

  // ---- sumber: kamera kabel + folder (dipakai di panel "Sumber foto") ----
  let ftp = null, cam = null;
  const loadSrc = async () => {
    try { ftp = await api.ftpInfo(); } catch { ftp = null; }
    try { cam = await api.usbCamState(); } catch { cam = null; }
  };
  async function toggleCam() {
    const r = await api.usbCamToggle(!(cam && cam.running));
    if (r) cam = r;
    setTimeout(loadSrc, 600);
  }
  onMount(() => { loadSrc(); const t = setInterval(loadSrc, 4000); return () => clearInterval(t); });

  // ---- folder sumber (modal "Tambah folder") ----
  let showFolderModal = false;
  $: folderStats = (st && st.folders) || [];
  $: watchFolders = (cfg && cfg.watchFolders) || [];
  $: folderRows = (cfg && cfg.watchFolder ? [{ path: cfg.watchFolder, primary: true }] : []).concat(
    watchFolders.map((p) => ({ path: p, primary: false }))
  ).map((r) => {
    const st2 = folderStats.find((f) => f.folder === r.path) || { done: 0, pending: 0, failed: 0, total: 0, pct: 0 };
    let status2 = "idle", label = "Menunggu file";
    if (st2.total > 0) {
      if (st2.pending > 0) { status2 = "run"; label = "Memproses…"; }
      else if (st2.failed > 0) { status2 = "err"; label = `Selesai dengan ${st2.failed} error`; }
      else { status2 = "ok"; label = "Selesai"; }
    }
    return { ...r, ...st2, status: status2, label };
  });
  async function addFolder() {
    const c = await api.addWatchFolder();
    if (c) config.set(c);
  }
  async function removeFolder(p) {
    const c = await api.removeWatchFolder(p);
    if (c) config.set(c);
  }
  async function changeFolder() {
    const c = await api.pickFolder();
    if (c) config.set(c);
  }
  async function clearFolder() {
    const c = await api.clearFolder();
    if (c) config.set(c);
  }

  // ---- unggah otomatis: kompresi / watermark / LUT lewat modal (bukan ke Pengaturan) ----
  let showKompresiModal = false, showWatermarkModal = false, showLutModal = false;

  $: wm = (cfg && cfg.watermark) || { enabled: false, type: "text", text: "", pos: "br", size: 22, opacity: 60, logoPath: "" };
  let wmText = "", wmTextSynced = null;
  $: if (cfg && wm.text !== wmTextSynced) { wmTextSynced = wm.text; wmText = wm.text || ""; }
  function wmPatch(obj) { patch({ watermark: obj }); }
  let wmTextEl;
  // Backend otomatis mematikan "enabled" lagi kalau teks/logo masih kosong (supaya switch
  // tak pernah "menyala" tanpa efek) — jadi di sini kita tuntun user isi dulu, gaya MAS EDY
  // ("Isi tulisan watermark dulu"), bukan diam-diam menolak klik.
  function toggleWatermark() {
    const next = !wm.enabled;
    if (next && wm.type === "text" && !wmText.trim()) {
      wmTextEl && wmTextEl.focus();
      return;
    }
    wmPatch({ enabled: next });
    schedulePreview({ enabled: next });
  }
  async function pickLogo() {
    const c = await api.watermarkPickLogo();
    if (c) config.set(c);
    schedulePreview();
  }
  const shortPath = (p) => (p && p.length > 44 ? "…" + p.slice(-43) : p || "—");
  let wmTextTimer;
  function wmTextInput() {
    schedulePreview({ text: wmText });
    clearTimeout(wmTextTimer);
    wmTextTimer = setTimeout(() => wmPatch({ text: wmText }), 400);
  }

  // ---- pratinjau hidup watermark (gaya MAS EDY): foto contoh yang ikut berubah saat
  // geser slider / ganti pilihan, sebelum benar-benar disimpan ----
  let wmPreviewSrc = null;
  let wmPreviewTimer;
  // Uint8Array -> base64 lewat potongan kecil (hindari batas argumen String.fromCharCode
  // untuk gambar besar). data: URI dipakai, bukan blob: — blob: pernah gagal dimuat di
  // webview desktop Wails (ikon gambar rusak), data: jauh lebih aman lintas webview.
  function bytesToBase64(bytes) {
    let binary = "";
    const chunk = 0x8000;
    for (let i = 0; i < bytes.length; i += chunk) {
      binary += String.fromCharCode.apply(null, bytes.subarray(i, i + chunk));
    }
    return btoa(binary);
  }
  function schedulePreview(extra) {
    clearTimeout(wmPreviewTimer);
    wmPreviewTimer = setTimeout(async () => {
      const draft = { ...wm, enabled: true, ...(extra || {}) };
      if (isDesktop()) {
        try {
          const result = await api.watermarkPreview(JSON.stringify(draft));
          // Binding Wails untuk []byte kadang balikin string base64, kadang array angka
          // (tergantung versi/tipe hasil codegen) — tangani dua-duanya, bukan cuma satu.
          let b64 = null;
          if (typeof result === "string" && result) {
            b64 = result;
          } else if (result && result.length) {
            b64 = bytesToBase64(new Uint8Array(result));
          }
          wmPreviewSrc = b64 ? "data:image/jpeg;base64," + b64 : null;
        } catch {
          wmPreviewSrc = null;
        }
      } else {
        wmPreviewSrc = api.watermarkPreviewUrl(draft);
      }
    }, 250);
  }
  function openWatermarkModal() {
    showWatermarkModal = true;
    schedulePreview();
  }

  let luts = [];
  const loadLuts = async () => { try { luts = (await api.listLuts()) || []; } catch { luts = []; } };
  let lutBusy = false;
  async function doImportLut() {
    lutBusy = true;
    try { luts = (await api.importLut()) || luts; } finally { lutBusy = false; }
  }
  async function doDeleteLut(name) {
    luts = (await api.deleteLut(name)) || luts;
  }
  async function pickLut(role, path) {
    const c = await api.setLut(role, path);
    if (c) config.set(c);
  }
  $: lutCfg = (cfg && cfg.lut) || { photo: "", video: "", strength: 100 };
  onMount(loadLuts);

  // ---- shortcut: FotoTree & folder sumber buka modal; harga loncat ke bagiannya
  // sendiri di halaman ini (anchor), internet cuma tampilkan ping — bukan navigasi ----
  let showTreeModal = false;
  let pingMs = null, pinging = false;
  async function loadPing() {
    pinging = true;
    try { pingMs = await api.ping(); } catch { pingMs = -1; } finally { pinging = false; }
  }
  onMount(() => { loadPing(); loadAccounts(); const t = setInterval(loadPing, 15000); return () => clearInterval(t); });
  const scrollToHarga = () => document.getElementById("harga-section")?.scrollIntoView({ behavior: "smooth", block: "center" });

  // ---- akun: dropdown AKUN — multi-akun, semua tersimpan LOKAL (fotoyu-akun.json di
  // data dir), bukan database. Pindah akun = SwitchAccount (tanpa password lagi); tiap
  // akun baru lewat Login tersimpan sendiri, tidak menimpa yang lain. ----
  let akunOpen = false;
  let accountsList = [];
  let ident = "", pass = "", loginErr = "", loggingIn = false;
  const loadAccounts = async () => { try { accountsList = (await api.accounts()) || []; } catch { accountsList = []; } };
  function openAkun() {
    akunOpen = !akunOpen;
    if (akunOpen) loadAccounts();
  }
  async function doLogin() {
    loginErr = ""; loggingIn = true;
    try { await api.login(ident, pass); ident = ""; pass = ""; await loadAccounts(); }
    catch (e) { loginErr = String(e); }
    finally { loggingIn = false; }
  }
  async function doSwitch(id) {
    try { await api.switchAccount(id); await loadAccounts(); }
    catch (e) { loginErr = String(e); }
  }
  async function doRemove(id) {
    accountsList = (await api.removeAccount(id)) || [];
  }

  // ---- tujuan unggah: preset (tombol + modal, gaya MAS EDY — bukan chip inline) ----
  let showPresetModal = false;
  $: presets = (cfg && cfg.presets) || [];
  function presetMatches(p) {
    const a = (p && p.event) || {};
    return (a.tree ? a.tree.tag_id : "") === (tree ? tree.tag_id : "") &&
      (a.price || 0) === (ev.price || 0) && (a.videoPrice || 0) === (ev.videoPrice || 0) &&
      (a.description || "") === (ev.description || "") &&
      JSON.stringify(a.nicknames || []) === JSON.stringify(ev.nicknames || []);
  }
  $: activePreset = presets.find(presetMatches);
  $: presetLabel = activePreset ? activePreset.name : presets.length ? "Pilih preset" : "Belum ada preset";
  async function applyPreset(p) {
    await patch({ event: p.event, compress: p.compress });
    showPresetModal = false;
  }
  const delPreset = (n) => patch({ presets: presets.filter((p) => p.name !== n) });
  // window.prompt() tidak tampil di webview desktop Wails (dibungkam diam-diam) — makanya
  // dulu "Simpan sebagai preset" kelihatan tidak minta nama sama sekali. Input nama inline di sini.
  let showSaveName = false, newPresetName = "";
  function openSaveAsPreset() {
    newPresetName = tree ? tree.name : "";
    showSaveName = true;
  }
  function doSaveAsPreset() {
    const n = newPresetName.trim();
    if (!n) return;
    patch({ presets: [...presets.filter((p) => p.name !== n), { name: n, event: cfg.event, compress: cfg.compress }] });
    showSaveName = false;
    newPresetName = "";
  }

  // ---- tujuan unggah: tab Foto/Video — cuma ganti tampilan (setelan orientasi video),
  // BUKAN switch on/off unggah video (itu tetap di Pengaturan → Opsi, gaya MAS EDY) ----
  let mediaTab = "photo";

  // ---- tujuan unggah: FotoTree ----
  let q = "", trees = [], searching = false;
  async function search() {
    searching = true;
    try { trees = (await api.searchTrees(q)) || []; } finally { searching = false; }
  }
  const choose = (t) => api.selectTree({ id: t.id, tag_id: t.tag_id, name: t.name, location: t.location, is_bib: t.is_bib });
  $: selTag = tree ? tree.tag_id : "";

  // ---- tujuan unggah: rincian biaya foto & video ----
  let fee = null, feeKey = "";
  let feeVideo = null, feeVideoKey = "";
  $: {
    const p = ev.price || 0, tag = (tree && tree.tag_id) || "", key = p + "|" + tag;
    if (fyOn && p > 0 && tag && key !== feeKey) {
      feeKey = key;
      api.fee(p, tag).then((f) => (fee = f)).catch(() => (fee = null));
    }
    if (!tag || !p) fee = null;
  }
  $: {
    const vp = ev.videoPrice || 0, tag = (tree && tree.tag_id) || "", key = vp + "|" + tag;
    if (fyOn && vp > 0 && tag && key !== feeVideoKey) {
      feeVideoKey = key;
      api.fee(vp, tag).then((f) => (feeVideo = f)).catch(() => (feeVideo = null));
    }
    if (!tag || !vp) feeVideo = null;
  }

  // ---- tujuan unggah: nickname ----
  let nickInput = "";
  function addNick() {
    const n = nickInput.trim().replace(/^@/, ""); nickInput = "";
    if (!n) return;
    const cur = ev.nicknames || [];
    if (!cur.includes(n)) patch({ event: { nicknames: [...cur, n] } });
  }
  const delNick = (n) => patch({ event: { nicknames: (ev.nicknames || []).filter((x) => x !== n) } });

  // ---- saringan nomor file (ganjil/genap) ----
  $: par = (cfg && cfg.parity) || { mode: "all" };

  const toggle = () => (paused ? api.resume() : api.pause());
  const fmtTime = (t) => new Date(t).toLocaleTimeString("id-ID", { hour12: false });
</script>

<div class="head-row">
  <div>
    <h1 class="ttl">Unggah</h1>
    <div class="sub">{ev.name || tree?.name || "Belum ada event"}{ev.price ? " · " + rp(ev.price) + " / foto" : ""}</div>
  </div>
</div>

<!-- ============ progres (ring + gauge — gaya MAS EDY) ============ -->
<div class="panel" style="margin-bottom:18px">
  <div class="sbanner" style="--ring-color:{bannerTone}">
    <div class="sic">{antreCount > 0 ? "⬆" : "☁"}</div>
    <div>
      <div class="st">{bannerTitle}</div>
      <div class="ss">{bannerSub}</div>
    </div>
  </div>
  <div class="sgrid">
    {#each rings as r}
      <div>
        <div class="sring" style="--ring-color:{r.color}">
          <svg viewBox="0 0 100 100">
            <circle class="tc" cx="50" cy="50" r="42" fill="none" stroke-width="9" />
            <circle class="ac" cx="50" cy="50" r="42" fill="none" stroke-width="9" stroke-dasharray={circ} stroke-dashoffset={circ * (1 - r.pct / 100)} />
          </svg>
          <div class="mid"><b>{r.text}</b>{#if r.unit}<span>{r.unit}</span>{/if}</div>
        </div>
        <div class="slabel">{r.label}</div>
      </div>
    {/each}
  </div>
  <div style="display:flex;align-items:center;justify-content:space-between;gap:12px;padding:0 18px 16px">
    <span class="muted" style="font-size:11.5px">{shown.duplicate} duplikat dilewati sesi ini</span>
    <button class="btn s sm" on:click={resetStats} title="Nolkan angka terunggah, gagal & duplikat (riwayat tidak dihapus)">Reset</button>
  </div>
</div>

<!-- ============ unggah / jeda + akun / saringan nomor ============ -->
<div class="hero">
  <div class="hero-act">
    <button class="actbox" class:on={!paused} disabled={!paused} on:click={toggle}>
      <svg viewBox="0 0 24 24"><path d="M12 16V4M7 9l5-5 5 5" /><path d="M4 16v3a1 1 0 001 1h14a1 1 0 001-1v-3" /></svg>
      Unggah
    </button>
    <button class="actbox" class:on={paused} disabled={paused} on:click={toggle}>
      <svg viewBox="0 0 24 24"><rect x="6" y="5" width="4" height="14" rx="1" /><rect x="14" y="5" width="4" height="14" rx="1" /></svg>
      Jeda
    </button>
  </div>

  <div class="hero-side">
    <div class="akunwrap">
      <button class="akunbox" on:click={openAkun}>
        <span class="ava" style="width:26px;height:26px;font-size:11px">{acct && acct.username ? acct.username[0].toUpperCase() : "?"}</span>
        {acct && acct.username ? "@" + acct.username : "Akun · belum masuk"}
        {#if accountsList.length > 1}<span class="pill up" style="margin-left:4px">{accountsList.length} akun</span>{/if}
        <svg style="width:14px;height:14px;margin-left:auto;stroke:currentColor;fill:none;stroke-width:2" viewBox="0 0 24 24"><path d="M6 9l6 6 6-6" /></svg>
      </button>
      {#if akunOpen}
        <div class="modal-bg" style="background:transparent" role="presentation" on:click|self={() => (akunOpen = false)} on:keydown={(e) => e.key === "Escape" && (akunOpen = false)}></div>
        <div class="akundrop">
          {#if accountsList.length}
            <div class="acctlist">
              {#each accountsList as a (a.id)}
                <div class="acctrow" class:active={a.active}>
                  <span class="ava" style="width:28px;height:28px;font-size:12px">{a.username ? a.username[0].toUpperCase() : "?"}</span>
                  <div class="oi">
                    <b>{a.name || a.username}</b>
                    <span>@{a.username}{a.active ? " · dipakai sekarang" : ""}</span>
                  </div>
                  {#if !a.active}<button class="btn s sm" on:click={() => doSwitch(a.id)}>Pakai</button>{/if}
                  <button class="mx" style="width:24px;height:24px;font-size:13px" on:click={() => doRemove(a.id)} title="Hapus akun ini">×</button>
                </div>
              {/each}
            </div>
            <div class="muted" style="font-size:11.5px;padding:6px 4px 4px">Semua akun di atas tersimpan lokal di perangkat ini.</div>
          {/if}
          {#if isDesktop()}
            <div style="display:flex;flex-direction:column;gap:8px;padding:8px 4px 2px">
              <input class="input" placeholder="Email / no. HP / username" bind:value={ident} />
              <input class="input" type="password" placeholder="Password" bind:value={pass} />
              {#if loginErr}<div style="color:var(--bad);font-size:12.5px">{loginErr}</div>{/if}
              <button class="btn p sm" on:click={doLogin} disabled={loggingIn || !ident || !pass}>{loggingIn ? "Masuk…" : accountsList.length ? "Tambah akun lain" : "Masuk FotoYu"}</button>
            </div>
          {:else}
            <div class="muted" style="font-size:12px;padding:8px 4px">Tambah/ganti akun FotoYu lewat aplikasi di laptop.</div>
          {/if}
        </div>
      {/if}
    </div>

    <div class="seg" style="width:100%">
      <button class:on={par.mode === "all"} on:click={() => patch({ parity: { mode: "all" } })}>Semua file</button>
      <button class:on={par.mode === "odd"} on:click={() => patch({ parity: { mode: "odd" } })}>Ganjil</button>
      <button class:on={par.mode === "even"} on:click={() => patch({ parity: { mode: "even" } })}>Genap</button>
    </div>
    {#if par.mode !== "all"}
      <div class="muted" style="font-size:11.5px;padding:0 4px">Hanya file bernomor {par.mode === "odd" ? "ganjil" : "genap"} yang diunggah — berhenti sendiri setelah antreannya habis.</div>
    {/if}
  </div>
</div>

<!-- ============ sumber foto + shortcut ============ -->
<div class="split2">
  <div class="panel">
    <div class="ph"><h3>Sumber foto</h3></div>
    <div class="row">
      <div class="oi">
        <b>Kamera kabel</b>
        {#if !cam || !cam.available}
          <span style="color:var(--bad)">program pembantu tidak ditemukan</span>
        {:else if cam.connected}
          <span>{cam.model} tersambung · {cam.files} jepretan masuk</span>
        {:else if cam.running}
          <span>menunggu kamera dicolok</span>
        {:else}
          <span>mati</span>
        {/if}
      </div>
      {#if cam && cam.available}<button class="sw" class:on={cam.running} on:click={toggleCam} aria-label="Kamera kabel"><i></i></button>{/if}
    </div>
    <div class="row">
      <div class="oi">
        <b>Folder</b>
        <span>{(cfg && cfg.watchFolder) || "belum dipilih"}{watchFolders.length ? " + " + watchFolders.length + " folder lain" : ""}</span>
      </div>
      <div style="display:flex;gap:8px">
        {#if isDesktop()}
          <button class="btn s sm" on:click={changeFolder}>Ganti</button>
          {#if cfg && cfg.watchFolder}<button class="btn s sm" on:click={clearFolder}>Kosongkan</button>{/if}
        {/if}
        <button class="btn s sm" on:click={() => (showFolderModal = true)}>Folder</button>
      </div>
    </div>
  </div>

  <div class="panel">
    <div class="ph"><h3>Shortcut</h3></div>
    <div class="shortgrid">
      <button class="shortitem" on:click={() => (showTreeModal = true)}>FotoTree</button>
      <button class="shortitem" on:click={() => (showFolderModal = true)}>Folder sumber</button>
      <button class="shortitem" on:click={scrollToHarga}>Harga</button>
      <div class="shortitem stat" title="Latensi ke server FotoYu">
        Internet
        <b>{pinging && pingMs == null ? "…" : pingMs == null ? "—" : pingMs < 0 ? "putus" : pingMs + " ms"}</b>
      </div>
    </div>
  </div>
</div>

<!-- ============ tujuan unggah + unggah otomatis ============ -->
<div class="layout">
  <div class="col">
    <div class="panel">
      <div class="ph"><h3>Tujuan unggah</h3>{#if tree}<span class="pill up">{tree.name}</span>{/if}</div>
      <div class="sect">
        <div class="f">
          <div class="fieldlbl">Preset <span style="text-transform:none;letter-spacing:0">(opsional)</span></div>
          <div class="row" style="padding:0">
            <div class="oi"><b>{presetLabel}</b></div>
            <button class="btn s sm" on:click={() => (showPresetModal = true)}>Pilih</button>
          </div>
        </div>

        <div class="seg" style="width:max-content">
          <button class:on={mediaTab === "photo"} on:click={() => (mediaTab = "photo")}>Foto</button>
          <button class:on={mediaTab === "video"} on:click={() => (mediaTab = "video")}>Video</button>
        </div>

        {#if mediaTab === "video"}
          <div class="f">
            <div class="muted" style="font-size:12.5px">
              {#if !(cfg && cfg.video && cfg.video.enabled)}
                Unggah video sedang mati — nyalakan di Pengaturan → Opsi.
              {:else}
                Video diunggah sebagai bundel 17 frame pratinjau — file videonya tetap di laptop. Video asli diminta FotoYu saat terjual.
              {/if}
            </div>
            <div class="fieldlbl" style="margin-top:12px">Orientasi video <span style="text-transform:none;letter-spacing:0">(opsional)</span></div>
            <div class="seg" style="width:100%;flex-wrap:wrap">
              {#each [["auto", "Otomatis"], ["0", "0°"], ["90", "90°"], ["180", "180°"], ["270", "270°"]] as [v, label]}
                <button class:on={(cfg && cfg.video && cfg.video.orientation) === v} on:click={() => patch({ video: { orientation: v } })}>{label}</button>
              {/each}
            </div>
            <div class="row" style="padding:10px 0 0">
              <div class="oi"><b>Cek orientasi dulu sebelum video diunggah</b><span>Segera hadir — pratinjau & konfirmasi manual belum ada, jadi belum bisa dinyalakan.</span></div>
              <button class="sw" disabled aria-label="Cek orientasi dulu (segera hadir)" title="Segera hadir"><i></i></button>
            </div>
          </div>
        {/if}

        <div class="f">
          <div class="fieldlbl">Event / FotoTree</div>
          <div class="row" style="padding:0">
            <div class="oi">
              <b>{tree ? tree.name : "Belum dipilih"}</b>
              {#if !fyOn}<span>masuk akun FotoYu dulu (kanan atas)</span>{/if}
            </div>
            <button class="btn s sm" on:click={() => (showTreeModal = true)}>{tree ? "Ganti" : "Pilih"}</button>
          </div>
        </div>

        <div class="fgrid" id="harga-section">
          <div class="f">
            <div class="fieldlbl">Harga per foto</div>
            <input class="input" type="number" min="0" step="1000" value={ev.price || 0} on:change={(e) => patch({ event: { price: +e.currentTarget.value || 0 } })} />
            {#if fee}
              <div class="feebox"><span>Pembeli membayar</span><b>{rp(fee.buyer)}</b></div>
              <div class="feebox" style="margin-top:6px"><span>Kamu menerima</span><b style="color:var(--good)">{rp(fee.net)}</b></div>
            {/if}
          </div>
          <div class="f">
            <div class="fieldlbl">Harga per video <span style="text-transform:none;letter-spacing:0">(0 = sama)</span></div>
            <input class="input" type="number" min="0" step="1000" value={ev.videoPrice || 0} on:change={(e) => patch({ event: { videoPrice: +e.currentTarget.value || 0 } })} />
            {#if feeVideo}
              <div class="feebox"><span>Pembeli membayar</span><b>{rp(feeVideo.buyer)}</b></div>
              <div class="feebox" style="margin-top:6px"><span>Kamu menerima</span><b style="color:var(--good)">{rp(feeVideo.net)}</b></div>
            {:else if !fyOn}
              <div class="muted" style="font-size:12px;margin-top:8px">Masuk akun FotoYu untuk lihat rincian biaya.</div>
            {/if}
          </div>
        </div>

        <div class="f">
          <div class="fieldlbl">Deskripsi <span style="text-transform:none;letter-spacing:0">(opsional)</span></div>
          <textarea class="ta" placeholder="Catatan singkat untuk foto-foto ini…" value={ev.description || ""} on:change={(e) => patch({ event: { description: e.currentTarget.value } })}></textarea>
        </div>

        <div class="f">
          <div class="fieldlbl">Tag nickname user <span style="text-transform:none;letter-spacing:0">(opsional)</span></div>
          <div class="chips" style="margin-bottom:8px">
            {#each ev.nicknames || [] as n}<span class="chip2">@{n}<button on:click={() => delNick(n)}>×</button></span>{/each}
            {#if !(ev.nicknames || []).length}<span class="muted" style="font-size:12.5px">Belum ada nickname</span>{/if}
          </div>
          <div style="display:flex;gap:8px">
            <input class="input" placeholder="Masukkan nickname…" bind:value={nickInput} on:keydown={(e) => e.key === "Enter" && addNick()} />
            <button class="btn s sm" on:click={addNick} disabled={!nickInput.trim()}>Tambah</button>
          </div>
        </div>

        {#if showSaveName}
          <div style="display:flex;gap:8px">
            <input class="input" placeholder="Nama preset (mis. Solo Marathon — Finish)…" bind:value={newPresetName}
                   on:keydown={(e) => e.key === "Enter" && doSaveAsPreset()} />
            <button class="btn p sm" on:click={doSaveAsPreset} disabled={!newPresetName.trim()}>Simpan</button>
            <button class="btn s sm" on:click={() => (showSaveName = false)}>Batal</button>
          </div>
        {:else}
          <button class="btn s" on:click={openSaveAsPreset}>Simpan sebagai preset</button>
        {/if}
      </div>
    </div>

    <!-- ringkasan aktivitas tetap ada walau tak ada di sketsa — berguna untuk memantau -->
    <div class="panel">
      <div class="ph"><h3>Aktivitas terbaru</h3><span class="pc">{scan.media || 0} file terpantau</span></div>
      <div style="padding:4px 18px 14px" class="logwrap">
        {#if $logs.length === 0}
          <p class="muted" style="padding:10px 0">Belum ada aktivitas. Pilih folder sumber & pastikan unggah aktif.</p>
        {/if}
        {#each $logs as l}
          <div class="logl"><time>{fmtTime(l.t)}</time><span class="lv {l.level}"></span><p>{l.msg}</p></div>
        {/each}
      </div>
    </div>
  </div>

  <div class="rail">
    <div class="panel">
      <div class="ph"><h3>Unggah otomatis</h3></div>
      <div class="row">
        <div class="oi"><b>Unggah otomatis</b><span>foto baru langsung naik</span></div>
        <button class="sw" class:on={!paused} on:click={toggle} aria-label="Unggah otomatis"><i></i></button>
      </div>
      <div class="row">
        <div class="oi"><b>Urutan unggah</b></div>
      </div>
      <div style="padding:0 18px 14px">
        <div class="seg" style="width:100%">
          <button class:on={cfg && cfg.uploadOrder === "newest"} on:click={() => patch({ uploadOrder: "newest" })}>Terbaru dulu</button>
          <button class:on={cfg && cfg.uploadOrder === "oldest"} on:click={() => patch({ uploadOrder: "oldest" })}>Terlama dulu</button>
        </div>
      </div>

      <div class="row">
        <div class="oi"><b>Kompresi</b><span>sisi terpanjang sebelum diunggah</span></div>
        <button class="btn s sm" on:click={() => (showKompresiModal = true)}>Ubah</button>
      </div>
      <!-- Watermark & LUT disembunyikan; barisnya hanya muncul bila masih aktif, supaya tetap bisa dimatikan. -->
      {#if wm.enabled}
      <div class="row">
        <div class="oi"><b>Watermark</b><span>ditempel ke foto sebelum diunggah</span></div>
        <button class="btn s sm" on:click={openWatermarkModal}>Ubah</button>
      </div>
      {/if}
      {#if lutCfg.photo || lutCfg.video}
      <div class="row">
        <div class="oi"><b>LUT</b><span>warna khas untuk hasil unggahan</span></div>
        <button class="btn s sm" on:click={() => (showLutModal = true)}>Ubah</button>
      </div>
      {/if}
    </div>

    {#if c.failed > 0}
      <button class="btn s" style="width:100%" on:click={() => api.retryFailed()}>Coba lagi {c.failed} yang gagal</button>
    {/if}
  </div>
</div>

{#if showKompresiModal}
  <div class="modal-bg" role="presentation" on:click|self={() => (showKompresiModal = false)} on:keydown={(e) => e.key === "Escape" && (showKompresiModal = false)}>
    <div class="modal">
      <div class="mh">
        <h3>Kompresi</h3>
        <button class="mx" on:click={() => (showKompresiModal = false)} aria-label="Tutup">×</button>
      </div>
      <div class="mb">
        <div class="row" style="padding:14px 0 0">
          <div class="oi"><b>Kompresi</b><span>sisi terpanjang sebelum diunggah</span></div>
          <div class="seg">
            <button class:on={cfg && cfg.compress && cfg.compress.size === 1440} on:click={() => patch({ compress: { size: 1440 } })}>1440 px</button>
            <button class:on={cfg && cfg.compress && cfg.compress.size === 4096} on:click={() => patch({ compress: { size: 4096 } })}>4096 px</button>
          </div>
        </div>
        <div class="row">
          <div class="oi"><b>Mode performa</b><span>jumlah olah & unggah bersamaan</span></div>
          <div class="seg">
            {#each ["auto", "hemat", "seimbang", "ngebut"] as m}
              <button class:on={cfg && cfg.performance === m} on:click={() => patch({ performance: m })} style="text-transform:capitalize">{m}</button>
            {/each}
          </div>
        </div>
      </div>
    </div>
  </div>
{/if}

{#if showWatermarkModal}
  <div class="modal-bg" role="presentation" on:click|self={() => (showWatermarkModal = false)} on:keydown={(e) => e.key === "Escape" && (showWatermarkModal = false)}>
    <div class="modal">
      <div class="mh">
        <h3>Watermark</h3>
        <button class="mx" on:click={() => (showWatermarkModal = false)} aria-label="Tutup">×</button>
      </div>
      <div class="mb">
        <div class="row" style="padding:14px 0 0">
          <div class="oi"><b>Pasang watermark</b><span>Di foto pratinjau yang diunggah ke FotoYu</span></div>
          <button class="sw" class:on={wm.enabled} on:click={toggleWatermark} aria-label="Pasang watermark"><i></i></button>
        </div>
        <div class="row" style="padding-top:0">
          <div class="oi"></div>
          <div class="seg">
            <button class:on={wm.type === "text"} on:click={() => { wmPatch({ type: "text" }); schedulePreview({ type: "text" }); }}>Tulisan</button>
            <button class:on={wm.type === "logo"} on:click={() => { wmPatch({ type: "logo" }); schedulePreview({ type: "logo" }); }}>Logo (PNG)</button>
          </div>
        </div>
        {#if wm.type === "text"}
          <div class="row">
            <div class="oi"><b>Tulisan</b></div>
            <input class="input" style="max-width:220px" bind:this={wmTextEl} bind:value={wmText} on:input={wmTextInput} placeholder="mis. © larilarisambilfoto" maxlength="60" />
          </div>
        {:else}
          <div class="row">
            <div class="oi"><b>Logo</b><span>{shortPath(wm.logoPath)}</span></div>
            {#if isDesktop()}<button class="btn s sm" on:click={pickLogo}>Pilih logo (PNG latar transparan)</button>{/if}
          </div>
        {/if}
        <div class="row">
          <div class="oi"><b>Posisi</b></div>
          <div class="seg">
            {#each [["tl", "↖"], ["tr", "↗"], ["c", "Tengah"], ["bl", "↙"], ["br", "↘"]] as [p, label]}
              <button class:on={wm.pos === p} on:click={() => { wmPatch({ pos: p }); schedulePreview({ pos: p }); }}>{label}</button>
            {/each}
          </div>
        </div>
        <div class="row">
          <div class="oi"><b>Ukuran</b><span>{wm.size}% lebar foto</span></div>
          <input type="range" min="8" max="60" step="2" value={wm.size}
                 on:input={(e) => schedulePreview({ size: +e.currentTarget.value })}
                 on:change={(e) => wmPatch({ size: +e.currentTarget.value })} />
        </div>
        <div class="row">
          <div class="oi"><b>Ketebalan</b><span>{wm.opacity}%</span></div>
          <input type="range" min="15" max="100" step="5" value={wm.opacity}
                 on:input={(e) => schedulePreview({ opacity: +e.currentTarget.value })}
                 on:change={(e) => wmPatch({ opacity: +e.currentTarget.value })} />
        </div>
        <div style="padding:14px 18px 4px">
          {#if wmPreviewSrc}
            <img src={wmPreviewSrc} alt="Pratinjau watermark" style="width:100%;border-radius:12px;aspect-ratio:3/2;object-fit:cover;background:var(--inset)" />
          {:else}
            <div style="width:100%;aspect-ratio:3/2;border-radius:12px;background:var(--inset)"></div>
          {/if}
        </div>
        <div class="muted" style="font-size:12px;padding:0 18px 16px">Watermark hanya di foto pratinjau. Foto hi-res yang dikirim ke pembeli tetap bersih. Video tidak diberi watermark.</div>
      </div>
    </div>
  </div>
{/if}

{#if showLutModal}
  <div class="modal-bg" role="presentation" on:click|self={() => (showLutModal = false)} on:keydown={(e) => e.key === "Escape" && (showLutModal = false)}>
    <div class="modal">
      <div class="mh">
        <h3>Warna (LUT)</h3>
        <button class="mx" on:click={() => (showLutModal = false)} aria-label="Tutup">×</button>
      </div>
      <div class="mb">
        <div style="padding:14px 0 6px">
          <div class="chips" style="margin-bottom:10px">
            {#each luts as l}
              <span class="chip2">{l.name.replace(/\.cube$/i, "")}<button on:click={() => doDeleteLut(l.name)} title="Hapus">×</button></span>
            {/each}
            {#if !luts.length}<span class="muted" style="font-size:12.5px">Belum ada LUT diimpor</span>{/if}
          </div>
          {#if isDesktop()}<button class="btn s sm" on:click={doImportLut} disabled={lutBusy}>{lutBusy ? "…" : "Tambah preset Lightroom / LUT"}</button>{/if}
        </div>
        <div class="row">
          <div class="oi"><b>Untuk foto</b></div>
          <select class="input" style="max-width:220px" value={lutCfg.photo || ""} on:change={(e) => pickLut("photo", e.currentTarget.value)}>
            <option value="">Tanpa LUT</option>
            {#each luts as l}<option value={l.path}>{l.name.replace(/\.cube$/i, "")}</option>{/each}
          </select>
        </div>
        <div class="row">
          <div class="oi"><b>Untuk video</b><span>dipakai pada 17 frame pratinjau</span></div>
          <select class="input" style="max-width:220px" value={lutCfg.video || ""} on:change={(e) => pickLut("video", e.currentTarget.value)}>
            <option value="">Tanpa LUT</option>
            {#each luts as l}<option value={l.path}>{l.name.replace(/\.cube$/i, "")}</option>{/each}
          </select>
        </div>
        <div class="row">
          <div class="oi"><b>Kekuatan</b><span>{lutCfg.strength}%</span></div>
          <input type="range" min="0" max="100" step="5" value={lutCfg.strength} on:change={(e) => patch({ lut: { strength: +e.currentTarget.value } })} />
        </div>
        <div class="muted" style="font-size:12px;padding:0 18px 16px">Simpan preset warna .cube atau preset Lightroom (.xmp/.dng/.lrtemplate — diubah otomatis jadi .cube) untuk mewarnai foto & video saat diunggah.</div>
      </div>
    </div>
  </div>
{/if}

{#if showPresetModal}
  <div class="modal-bg" role="presentation" on:click|self={() => (showPresetModal = false)} on:keydown={(e) => e.key === "Escape" && (showPresetModal = false)}>
    <div class="modal">
      <div class="mh">
        <h3>Preset</h3>
        <button class="mx" on:click={() => (showPresetModal = false)} aria-label="Tutup">×</button>
      </div>
      <div class="mb">
        {#each presets as p}
          <div class="row">
            <div class="oi">
              <b>{p.name}</b>
              <span>{p.event && p.event.tree ? p.event.tree.name : "tanpa event"}{p.event && p.event.price ? " · " + rp(p.event.price) : ""}{p.event && p.event.videoPrice ? " · video " + rp(p.event.videoPrice) : ""}</span>
            </div>
            <div style="display:flex;gap:8px">
              <button class="btn s sm" on:click={() => applyPreset(p)}>Pakai</button>
              <button class="btn s sm" on:click={() => delPreset(p.name)}>Hapus</button>
            </div>
          </div>
        {/each}
        {#if !presets.length}
          <p class="muted" style="padding:10px 0">Belum ada preset. Isi Tujuan unggah lalu ketuk "Simpan sebagai preset".</p>
        {/if}
      </div>
    </div>
  </div>
{/if}

{#if showTreeModal}
  <div class="modal-bg" role="presentation" on:click|self={() => (showTreeModal = false)} on:keydown={(e) => e.key === "Escape" && (showTreeModal = false)}>
    <div class="modal">
      <div class="mh">
        <h3>FotoTree tujuan</h3>
        <button class="mx" on:click={() => (showTreeModal = false)} aria-label="Tutup">×</button>
      </div>
      <div class="mb">
        {#if fyOn}
          <div style="display:flex;gap:8px">
            <input class="input" placeholder="Cari nama event / FotoTree" bind:value={q} on:keydown={(e) => e.key === "Enter" && search()} />
            <button class="btn s" on:click={search} disabled={searching}>{searching ? "…" : "Cari"}</button>
          </div>
          {#if trees.length}
            <div class="treelist" style="margin-top:10px">
              {#each trees as t}
                <button type="button" class="treerow" class:sel={t.tag_id === selTag} on:click={() => { choose(t); showTreeModal = false; }}>
                  <div style="flex:1;min-width:0;text-align:left">
                    <b style="font-size:13.5px">{t.name}</b>
                    <div class="muted" style="font-size:12px">{t.category || ""}{t.count != null ? " · " + t.count + " foto" : ""}</div>
                  </div>
                  {#if t.tag_id === selTag}<span class="pill up">dipakai</span>{/if}
                </button>
              {/each}
            </div>
          {:else if !searching}
            <div class="muted" style="font-size:12.5px;padding:10px 0">Cari nama event atau lokasi untuk menemukan FotoTree-nya.</div>
          {/if}
        {:else}
          <div class="muted" style="font-size:12.5px;padding:10px 0">Masuk akun FotoYu dulu (kanan atas) untuk mencari FotoTree.</div>
        {/if}
      </div>
    </div>
  </div>
{/if}

{#if showFolderModal}
  <div class="modal-bg" role="presentation" on:click|self={() => (showFolderModal = false)} on:keydown={(e) => e.key === "Escape" && (showFolderModal = false)}>
    <div class="modal">
      <div class="mh">
        <h3>Folder sumber</h3>
        <button class="mx" on:click={() => (showFolderModal = false)} aria-label="Tutup">×</button>
      </div>
      <div class="mb">
        {#each folderRows as f}
          <div class="frow">
            <div class="fh">
              <div class="fp">
                {folderBase(f.path)}{#if f.primary}<span class="pill ok" style="margin-left:6px;vertical-align:middle">utama</span>{/if}
                <u>{f.path}</u>
              </div>
              {#if f.primary}
                <div style="display:flex;gap:6px;flex:none">
                  {#if isDesktop()}<button class="btn s sm" on:click={changeFolder}>Ganti</button>{/if}
                  <button class="mx" style="width:24px;height:24px;font-size:13px" on:click={clearFolder} title="Kosongkan">×</button>
                </div>
              {:else}
                <button class="mx" style="width:24px;height:24px;font-size:13px" on:click={() => removeFolder(f.path)} title="Hapus">×</button>
              {/if}
            </div>
            <div class="bar"><i class:err={f.status === "err"} style="width:{f.pct}%"></i></div>
            <div class="fstat {f.status}" style="margin-top:6px">
              {f.label}{f.total ? ` · ${f.done}/${f.total}` : ""}
            </div>
          </div>
        {/each}
        {#if !folderRows.length}
          <p class="muted" style="padding:10px 0">Belum ada folder sumber dipilih.</p>
        {/if}
        <div style="display:flex;gap:8px;margin-top:14px">
          <button class="btn s" style="flex:1" on:click={addFolder}>+ Tambah folder</button>
          <button class="btn p" style="flex:1" on:click={toggle} disabled={!folderRows.length}>
            {paused ? "Unggah" : "Jeda unggah"}
          </button>
        </div>
      </div>
    </div>
  </div>
{/if}
