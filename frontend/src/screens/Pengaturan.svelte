<script>
  import { onMount } from "svelte";
  import { status, config } from "../lib/stores";
  import { api, isDesktop } from "../lib/api";

  export let tab = "sumber";
  const tabs = [
    { id: "sumber", label: "Sumber" },
    { id: "tujuan", label: "Tujuan unggah" },
    { id: "saring", label: "Saring file" },
    { id: "opsi", label: "Opsi" },
    { id: "akun", label: "Akun" },
  ];

  $: st = $status;
  $: cfg = $config;
  $: acct = st && st.account;
  $: fyOn = acct && acct.loggedIn;
  $: ev = (cfg && cfg.event) || {};
  $: tree = ev.tree;
  $: par = (cfg && cfg.parity) || { mode: "all" };

  async function patch(obj) {
    const c = await api.updateConfig(JSON.stringify(obj));
    if (c) config.set(c);
  }
  const rp = (n) => "Rp" + (Number(n) || 0).toLocaleString("id-ID");

  // ---- kartu memori / card reader ----
  let drives = [];
  const loadDrives = async () => { try { drives = (await api.cardDrives()) || []; } catch { drives = []; } };
  async function useDrive(d) {
    const c = await api.useCardDrive(d.path);
    if (c) config.set(c);
  }

  // ---- folder sumber tambahan (multi-folder konkuren) ----
  $: watchFolders = (cfg && cfg.watchFolders) || [];
  async function addWatchFolder() {
    const c = await api.addWatchFolder();
    if (c) config.set(c);
  }
  async function delWatchFolder(p) {
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

  // ---- kamera kabel (PTP) ----
  let cam = null;
  const loadCam = async () => { try { cam = await api.usbCamState(); } catch { cam = null; } };
  async function toggleCam() {
    const r = await api.usbCamToggle(!(cam && cam.running));
    if (r) cam = r;
    setTimeout(loadCam, 600);
  }

  // ---- FTP ----
  let ftp = null, showPass = false;
  const loadFtp = async () => { try { ftp = await api.ftpInfo(); } catch { ftp = null; } };
  async function toggleFtp() {
    await patch({ ftp: { enabled: !(cfg && cfg.ftp && cfg.ftp.enabled) } });
    setTimeout(loadFtp, 400);
  }
  onMount(() => {
    loadFtp(); loadCam(); loadDrives();
    const t = setInterval(() => { loadFtp(); loadCam(); loadDrives(); }, 3000);
    return () => clearInterval(t);
  });

  // ---- rincian biaya ----
  let fee = null, feeKey = "";
  $: {
    const p = ev.price || 0, tag = (tree && tree.tag_id) || "", key = p + "|" + tag;
    if (fyOn && p > 0 && tag && key !== feeKey) {
      feeKey = key;
      api.fee(p, tag).then((f) => (fee = f)).catch(() => (fee = null));
    }
    if (!tag || !p) fee = null;
  }

  // ---- akun ----
  let ident = "", pass = "", loginErr = "", loggingIn = false;
  async function doLogin() {
    loginErr = ""; loggingIn = true;
    try { await api.login(ident, pass); pass = ""; }
    catch (e) { loginErr = String(e); }
    finally { loggingIn = false; }
  }

  // ---- FotoTree ----
  let q = "", trees = [], searching = false;
  async function search() {
    searching = true;
    try { trees = (await api.searchTrees(q)) || []; } finally { searching = false; }
  }
  const choose = (t) => api.selectTree({ id: t.id, tag_id: t.tag_id, name: t.name, location: t.location, is_bib: t.is_bib });
  $: selTag = tree ? tree.tag_id : "";

  // ---- nickname & preset ----
  let nickInput = "";
  function addNick() {
    const n = nickInput.trim().replace(/^@/, ""); nickInput = "";
    if (!n) return;
    const cur = ev.nicknames || [];
    if (!cur.includes(n)) patch({ event: { nicknames: [...cur, n] } });
  }
  const delNick = (n) => patch({ event: { nicknames: (ev.nicknames || []).filter((x) => x !== n) } });

  let presetName = "";
  function savePreset() {
    const n = presetName.trim(); presetName = "";
    if (!n || !cfg) return;
    patch({ presets: [...(cfg.presets || []).filter((p) => p.name !== n), { name: n, event: cfg.event, compress: cfg.compress }] });
  }
  const applyPreset = (p) => patch({ event: p.event, compress: p.compress });
  const delPreset = (n) => patch({ presets: (cfg.presets || []).filter((p) => p.name !== n) });

  // ---- watermark ----
  $: wm = (cfg && cfg.watermark) || { enabled: false, type: "text", text: "", pos: "br", size: 22, opacity: 60, logoPath: "" };
  let wmText = "", wmTextSynced = null;
  $: if (cfg && wm.text !== wmTextSynced) { wmTextSynced = wm.text; wmText = wm.text || ""; }
  function wmPatch(obj) { patch({ watermark: obj }); }
  async function pickLogo() {
    const c = await api.watermarkPickLogo();
    if (c) config.set(c);
  }
  const shortPath = (p) => (p && p.length > 44 ? "…" + p.slice(-43) : p || "—");
  let wmTextTimer;
  function wmTextInput() {
    clearTimeout(wmTextTimer);
    wmTextTimer = setTimeout(() => wmPatch({ text: wmText }), 400);
  }

  // ---- pustaka LUT (warna) ----
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
</script>

<div class="head-row">
  <div><h1 class="ttl">Pengaturan</h1><div class="sub">Semua setelan unggah ada di sini — layar Unggah khusus untuk memantau.</div></div>
</div>

<div class="seg" style="width:max-content;margin-bottom:18px;flex-wrap:wrap">
  {#each tabs as t}<button class:on={tab === t.id} on:click={() => (tab = t.id)}>{t.label}</button>{/each}
</div>

<div class="single">
  <!-- ============ SUMBER ============ -->
  {#if tab === "sumber"}
    <div class="panel">
      <div class="ph"><h3>Folder</h3><span class="pc">foto baru di folder ini otomatis diunggah</span></div>
      <div class="row">
        <div class="oi"><b>Folder dipantau</b><span>{(cfg && cfg.watchFolder) || "belum dipilih"}</span></div>
        {#if isDesktop()}
          <div style="display:flex;gap:8px">
            <button class="btn s sm" on:click={changeFolder}>Ganti</button>
            {#if cfg && cfg.watchFolder}<button class="btn s sm" on:click={clearFolder}>Kosongkan</button>{/if}
          </div>
        {:else}<span class="muted">atur di laptop</span>{/if}
      </div>
      <div class="row">
        <div class="oi"><b>Ikut subfolder</b><span>pindai juga folder di dalamnya</span></div>
        <button class="sw" class:on={cfg && cfg.includeSubfolders} on:click={() => patch({ includeSubfolders: !(cfg && cfg.includeSubfolders) })} aria-label="Ikut subfolder"><i></i></button>
      </div>
      <div class="row">
        <div class="oi"><b>Pindah setelah terunggah</b><span>ke folder "{(cfg && cfg.afterUpload && cfg.afterUpload.folder) || "Sudah upload"}"</span></div>
        <button class="sw" class:on={cfg && cfg.afterUpload && cfg.afterUpload.move} on:click={() => patch({ afterUpload: { move: !(cfg && cfg.afterUpload && cfg.afterUpload.move) } })} aria-label="Pindah setelah terunggah"><i></i></button>
      </div>
    </div>

    <div class="panel">
      <div class="ph"><h3>Folder tambahan</h3><span class="pc">dipindai & diunggah bersamaan dengan folder utama</span></div>
      <div style="padding:4px 18px 16px">
        <div class="chips" style="margin-bottom:10px">
          {#each watchFolders as f}
            <span class="chip2">{shortPath(f)}<button on:click={() => delWatchFolder(f)} title="Hapus">×</button></span>
          {/each}
          {#if !watchFolders.length}<span class="muted" style="font-size:12.5px">Belum ada folder tambahan</span>{/if}
        </div>
        {#if isDesktop()}<button class="btn s sm" on:click={addWatchFolder}>Tambah folder</button>{/if}
        <div class="muted" style="font-size:12px;margin-top:8px">
          Misal mau unggah folder berbeda sekaligus (ganjil dari satu kartu, genap dari kartu lain) —
          file dari semua folder diolah & diunggah bersamaan, masing-masing dapat "Sudah upload" sendiri.
        </div>
      </div>
    </div>

    <div class="panel">
      <div class="ph"><h3>Kartu memori / card reader</h3><span class="pc">{drives.length ? drives.length + " terdeteksi" : "tidak ada yang tercolok"}</span></div>
      {#each drives as d}
        <div class="row">
          <div class="oi">
            <b>{d.label}</b>
            <span>{d.files.toLocaleString("id-ID")} foto · {d.path}</span>
          </div>
          {#if cfg && cfg.watchFolder === d.path}
            <span class="pill ok">dipakai</span>
          {:else}
            <button class="btn p sm" on:click={() => useDrive(d)}>Pakai</button>
          {/if}
        </div>
      {/each}
      {#if !drives.length}
        <div class="muted" style="font-size:12.5px;padding:4px 18px 14px">
          Colok kartu memori / card reader, atau setel kamera ke mode <b>Mass Storage</b>.
          Drive yang punya folder <b>DCIM</b> akan muncul di sini.
        </div>
      {:else}
        <div class="muted" style="font-size:12px;padding:0 18px 16px">
          Saat kartu dipakai: unggahan otomatis <b>dijeda dulu</b> (kartu bisa berisi ribuan foto lama —
          periksa saringan, lalu tekan Lanjutkan), dan file di kartu <b>tidak akan dipindah</b>.
        </div>
      {/if}
    </div>

    <div class="panel">
      <div class="ph"><h3>Kamera kabel (USB / tether)</h3>{#if cam && cam.connected}<span class="pill ok">{cam.model}</span>{/if}</div>
      <div class="row">
        <div class="oi">
          <b>Ambil jepretan langsung dari kamera</b>
          {#if !cam || !cam.available}
            <span style="color:var(--bad)">program pembantu tidak ditemukan di aplikasi ini</span>
          {:else if cam.connected}
            <span>{cam.model} tersambung · {cam.files} jepretan masuk{cam.ignored ? " · " + cam.ignored + " foto lama diabaikan" : ""}</span>
          {:else if cam.running}
            <span>menunggu kamera dicolok (mode tether/PTP)</span>
          {:else}
            <span>mati</span>
          {/if}
        </div>
        {#if cam && cam.available}
          <button class="sw" class:on={cam.running} on:click={toggleCam} aria-label="Kamera kabel"><i></i></button>
        {/if}
      </div>
      {#if cam && cam.error}
        <div class="row"><div class="oi" style="color:var(--bad)">{cam.error}</div></div>
      {/if}
      <div class="muted" style="font-size:12px;padding:0 18px 16px">
        Hanya jepretan <b>baru</b> yang diunduh — foto lama di kartu diabaikan. Saat kamera tersambung,
        folder sumber otomatis dialihkan ke folder unduhan kamera.
        Kalau kameramu tidak mendukung tether, pakai software bawaannya (mis. Nikon NX Tether) dan
        arahkan folder simpannya ke folder yang dipantau.
      </div>
    </div>

    <div class="panel">
      <div class="ph"><h3>Kamera Wi-Fi (FTP)</h3>{#if ftp && ftp.running}<span class="pill ok">nyala</span>{/if}</div>
      <div class="row">
        <div class="oi">
          <b>Terima foto langsung dari kamera</b>
          {#if ftp && ftp.running}<span>{ftp.files || 0} file diterima{ftp.connected ? " · " + ftp.connected + " kamera tersambung" : ""}</span>
          {:else if ftp && ftp.enabled}<span style="color:var(--bad)">{ftp.error || "belum menyala"}</span>
          {:else}<span>mati</span>{/if}
        </div>
        <button class="sw" class:on={cfg && cfg.ftp && cfg.ftp.enabled} on:click={toggleFtp} aria-label="Kamera Wi-Fi"><i></i></button>
      </div>
      {#if ftp && ftp.running && isDesktop()}
        <div style="padding:4px 18px 16px">
          <div class="feebox" style="margin-top:0"><span>Alamat</span><b>{(ftp.addresses || []).join(" / ") || "—"}</b></div>
          <div class="feebox" style="margin-top:6px"><span>Port · User</span><b>{ftp.port} · {ftp.user}</b></div>
          <div class="feebox" style="margin-top:6px">
            <span>Password</span>
            <b>{showPass ? ftp.pass : "••••••"}
              <button class="chip2" style="padding:2px 8px;margin-left:6px" on:click={() => (showPass = !showPass)}>{showPass ? "sembunyikan" : "lihat"}</button></b>
          </div>
          <div class="muted" style="font-size:12px;margin-top:8px">Di kamera: MENU → Jaringan → server <b>FTP</b> biasa (tanpa SSL), <b>mode pasif</b> aktif, lalu isi alamat/port/user/password di atas.</div>
        </div>
      {/if}
    </div>
  {/if}

  <!-- ============ TUJUAN UNGGAH ============ -->
  {#if tab === "tujuan"}
    <div class="panel">
      <div class="ph"><h3>Tujuan</h3></div>
      <div class="row">
        <div class="oi"><b>Kirim ke</b><span>FotoYu, atau folder lokal untuk uji coba</span></div>
        <div class="seg">
          <button class:on={cfg && cfg.destination === "local"} on:click={() => patch({ destination: "local" })}>Lokal</button>
          <button class:on={cfg && cfg.destination === "fotoyu"} on:click={() => patch({ destination: "fotoyu" })}>FotoYu</button>
        </div>
      </div>
    </div>

    <div class="panel">
      <div class="ph"><h3>Event &amp; harga</h3>{#if tree}<span class="pill up">{tree.name}</span>{/if}</div>
      <div class="sect">
        <div class="f">
          <div class="fieldlbl">Preset</div>
          <div class="chips" style="margin-bottom:8px">
            {#each (cfg && cfg.presets) || [] as p}
              <span class="chip2">
                <button on:click={() => applyPreset(p)} style="opacity:1;font-weight:600">{p.name}</button>
                <button on:click={() => delPreset(p.name)} title="Hapus">×</button>
              </span>
            {/each}
            {#if !cfg || !(cfg.presets || []).length}<span class="muted" style="font-size:12.5px">Belum ada preset</span>{/if}
          </div>
          <div style="display:flex;gap:8px">
            <input class="input" placeholder="Nama preset baru…" bind:value={presetName} on:keydown={(e) => e.key === "Enter" && savePreset()} />
            <button class="btn s sm" on:click={savePreset} disabled={!presetName.trim()}>Simpan</button>
          </div>
        </div>

        <div class="f">
          <div class="fieldlbl">FotoTree (lokasi event)</div>
          {#if fyOn}
            <div style="display:flex;gap:8px">
              <input class="input" placeholder="Cari nama event / FotoTree" bind:value={q} on:keydown={(e) => e.key === "Enter" && search()} />
              <button class="btn s" on:click={search} disabled={searching}>{searching ? "…" : "Cari"}</button>
            </div>
            <div class="treelist" style="margin-top:8px">
              {#each trees as t}
                <button type="button" class="treerow" class:sel={t.tag_id === selTag} on:click={() => choose(t)}>
                  <div style="flex:1;min-width:0;text-align:left">
                    <b style="font-size:13.5px">{t.name}</b>
                    <div class="muted" style="font-size:12px">{t.category || ""}{t.count != null ? " · " + t.count + " foto" : ""}{t.official ? " · resmi" : ""}</div>
                  </div>
                  {#if t.tag_id === selTag}<span class="pill up">dipakai</span>{/if}
                </button>
              {/each}
            </div>
          {:else}
            <div class="muted" style="font-size:12.5px">Masuk akun FotoYu dulu (tab Akun) untuk mencari FotoTree.</div>
          {/if}
        </div>

        <div class="f">
          <div class="fieldlbl">Nama event (catatan)</div>
          <input class="input" value={ev.name || ""} placeholder="mis. Borobudur Marathon" on:change={(e) => patch({ event: { name: e.currentTarget.value } })} />
        </div>

        <div class="fgrid">
          <div class="f">
            <div class="fieldlbl">📷 Harga per foto</div>
            <input class="input" type="number" min="0" step="1000" value={ev.price || 0} on:change={(e) => patch({ event: { price: +e.currentTarget.value || 0 } })} />
            {#if fee}
              <div class="feebox"><span>Pembeli bayar</span><b>{rp(fee.buyer)}</b></div>
              <div class="feebox" style="margin-top:6px"><span>Kamu terima</span><b style="color:var(--good)">{rp(fee.net)}</b></div>
            {/if}
          </div>
          <div class="f">
            <div class="fieldlbl">🎬 Harga per video <span style="text-transform:none;letter-spacing:0">(0 = sama)</span></div>
            <input class="input" type="number" min="0" step="1000" value={ev.videoPrice || 0} on:change={(e) => patch({ event: { videoPrice: +e.currentTarget.value || 0 } })} />
            {#if !fyOn}<div class="muted" style="font-size:12px;margin-top:8px">Masuk akun FotoYu untuk lihat rincian biaya.</div>{/if}
          </div>
        </div>

        <div class="f">
          <div class="fieldlbl">Deskripsi <span style="text-transform:none;letter-spacing:0">(opsional)</span></div>
          <textarea class="ta" placeholder="Catatan singkat untuk foto-foto ini…" value={ev.description || ""} on:change={(e) => patch({ event: { description: e.currentTarget.value } })}></textarea>
        </div>

        <div class="f">
          <div class="fieldlbl">Tag nickname <span style="text-transform:none;letter-spacing:0">(opsional)</span></div>
          <div class="chips" style="margin-bottom:8px">
            {#each ev.nicknames || [] as n}<span class="chip2">@{n}<button on:click={() => delNick(n)}>×</button></span>{/each}
            {#if !(ev.nicknames || []).length}<span class="muted" style="font-size:12.5px">Belum ada nickname</span>{/if}
          </div>
          <div style="display:flex;gap:8px">
            <input class="input" placeholder="Masukkan nickname…" bind:value={nickInput} on:keydown={(e) => e.key === "Enter" && addNick()} />
            <button class="btn s sm" on:click={addNick} disabled={!nickInput.trim()}>Tambah</button>
          </div>
        </div>
      </div>
    </div>
  {/if}

  <!-- ============ SARING FILE ============ -->
  {#if tab === "saring"}
    <div class="panel">
      <div class="ph"><h3>Nomor file</h3><span class="pc">bagi kerja dengan laptop/akun kedua</span></div>
      <div class="optseg" style="padding-top:4px">
        <div class="seg">
          <button class:on={par.mode === "all"} on:click={() => patch({ parity: { mode: "all" } })}>Semua</button>
          <button class:on={par.mode === "odd"} on:click={() => patch({ parity: { mode: "odd" } })}>Ganjil</button>
          <button class:on={par.mode === "even"} on:click={() => patch({ parity: { mode: "even" } })}>Genap</button>
        </div>
      </div>
      {#if par.mode !== "all"}
        <div class="muted" style="font-size:12px;padding:8px 4px 2px">Hanya file bernomor {par.mode === "odd" ? "ganjil" : "genap"} yang diunggah, sisanya dilewati. Begitu antreannya habis, unggah otomatis berhenti sendiri.</div>
      {/if}
    </div>
  {/if}

  <!-- ============ OPSI ============ -->
  {#if tab === "opsi"}
    <div class="panel">
      <div class="ph"><h3>Unggah</h3></div>
      <div class="row">
        <div class="oi"><b>Unggah otomatis</b><span>foto & video baru langsung naik</span></div>
        <button class="sw" class:on={st && !st.paused} on:click={() => (st && st.paused ? api.resume() : api.pause())} aria-label="Unggah otomatis"><i></i></button>
      </div>
      <div class="row">
        <div class="oi"><b>Urutan unggah</b><span>foto baru dijepret naik duluan</span></div>
        <div class="seg">
          <button class:on={cfg && cfg.uploadOrder === "newest"} on:click={() => patch({ uploadOrder: "newest" })}>Terbaru</button>
          <button class:on={cfg && cfg.uploadOrder === "oldest"} on:click={() => patch({ uploadOrder: "oldest" })}>Terlama</button>
        </div>
      </div>
      <div class="row">
        <div class="oi"><b>Unggah video</b><span>.mp4/.mov sebagai 17 frame pratinjau</span></div>
        <button class="sw" class:on={cfg && cfg.video && cfg.video.enabled} on:click={() => patch({ video: { enabled: !(cfg && cfg.video && cfg.video.enabled) } })} aria-label="Unggah video"><i></i></button>
      </div>
    </div>

    <div class="panel">
      <div class="ph"><h3>Olah gambar</h3></div>
      <div class="row">
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

    <!-- LUT & watermark disembunyikan; panelnya hanya muncul bila masih aktif, supaya tetap bisa dimatikan. -->
    {#if lutCfg.photo || lutCfg.video}
    <div class="panel">
      <div class="ph"><h3>Warna (LUT)</h3><span class="pc">{luts.length} di pustaka</span></div>
      <div style="padding:4px 18px 16px">
        <div class="chips" style="margin-bottom:10px">
          {#each luts as l}
            <span class="chip2">{l.name.replace(/\.cube$/i, "")}<button on:click={() => doDeleteLut(l.name)} title="Hapus">×</button></span>
          {/each}
          {#if !luts.length}<span class="muted" style="font-size:12.5px">Belum ada LUT diimpor</span>{/if}
        </div>
        {#if isDesktop()}<button class="btn s sm" on:click={doImportLut} disabled={lutBusy}>{lutBusy ? "…" : "Impor .cube"}</button>{/if}
      </div>
      <div class="row">
        <div class="oi"><b>LUT foto</b></div>
        <select class="input" style="max-width:220px" value={lutCfg.photo || ""} on:change={(e) => pickLut("photo", e.currentTarget.value)}>
          <option value="">Tanpa LUT</option>
          {#each luts as l}<option value={l.path}>{l.name.replace(/\.cube$/i, "")}</option>{/each}
        </select>
      </div>
      <div class="row">
        <div class="oi"><b>LUT video</b><span>dipakai pada 17 frame pratinjau</span></div>
        <select class="input" style="max-width:220px" value={lutCfg.video || ""} on:change={(e) => pickLut("video", e.currentTarget.value)}>
          <option value="">Tanpa LUT</option>
          {#each luts as l}<option value={l.path}>{l.name.replace(/\.cube$/i, "")}</option>{/each}
        </select>
      </div>
      <div class="row">
        <div class="oi"><b>Kekuatan</b><span>{lutCfg.strength}%</span></div>
        <input type="range" min="0" max="100" value={lutCfg.strength} on:change={(e) => patch({ lut: { strength: +e.currentTarget.value } })} />
      </div>
    </div>
    {/if}

    {#if wm.enabled}
    <div class="panel">
      <div class="ph"><h3>Watermark</h3></div>
      <div class="row">
        <div class="oi"><b>Pakai watermark</b><span>ditempel ke foto sebelum diunggah</span></div>
        <button class="sw" class:on={wm.enabled} on:click={() => wmPatch({ enabled: !wm.enabled })} aria-label="Pakai watermark"><i></i></button>
      </div>
      {#if wm.enabled}
      <div class="row">
        <div class="oi"><b>Jenis</b><span>teks atau gambar logo</span></div>
        <div class="seg">
          <button class:on={wm.type === "text"} on:click={() => wmPatch({ type: "text" })}>Teks</button>
          <button class:on={wm.type === "logo"} on:click={() => wmPatch({ type: "logo" })}>Logo</button>
        </div>
      </div>
      {#if wm.type === "text"}
        <div class="row">
          <div class="oi"><b>Teks</b><span>mis. nama studio / @username</span></div>
          <input class="input" style="max-width:220px" bind:value={wmText} on:input={wmTextInput} placeholder="Teks watermark" />
        </div>
      {:else}
        <div class="row">
          <div class="oi"><b>Logo</b><span>{shortPath(wm.logoPath)}</span></div>
          {#if isDesktop()}<button class="btn s sm" on:click={pickLogo}>Pilih gambar</button>{/if}
        </div>
      {/if}
      <div class="row">
        <div class="oi"><b>Posisi</b></div>
        <div class="seg">
          {#each [["tl","Kiri atas"],["tr","Kanan atas"],["c","Tengah"],["bl","Kiri bawah"],["br","Kanan bawah"]] as [p, label]}
            <button class:on={wm.pos === p} on:click={() => wmPatch({ pos: p })}>{label}</button>
          {/each}
        </div>
      </div>
      <div class="row">
        <div class="oi"><b>Ukuran</b><span>{wm.size}% lebar foto</span></div>
        <input type="range" min="5" max="60" value={wm.size} on:change={(e) => wmPatch({ size: +e.currentTarget.value })} />
      </div>
      <div class="row">
        <div class="oi"><b>Transparansi</b><span>{wm.opacity}%</span></div>
        <input type="range" min="5" max="100" value={wm.opacity} on:change={(e) => wmPatch({ opacity: +e.currentTarget.value })} />
      </div>
      {/if}
    </div>
    {/if}

    <div class="panel">
      <div class="ph"><h3>Tampilan</h3></div>
      <div class="row">
        <div class="oi"><b>Tema</b><span>terang / gelap / ikut sistem</span></div>
        <div class="seg">
          <button on:click={() => document.documentElement.setAttribute("data-theme", "light")}>Terang</button>
          <button on:click={() => document.documentElement.setAttribute("data-theme", "dark")}>Gelap</button>
          <button on:click={() => document.documentElement.removeAttribute("data-theme")}>Sistem</button>
        </div>
      </div>
    </div>
  {/if}

  <!-- ============ AKUN ============ -->
  {#if tab === "akun"}
    <div class="panel">
      <div class="ph"><h3>Akun FotoYu</h3>{#if fyOn}<span class="pill ok">@{acct.username}</span>{/if}</div>
      {#if fyOn}
        <div class="row">
          <div class="oi"><b>{acct.name || acct.username}</b><span>tersambung</span></div>
          <button class="btn s sm" on:click={() => api.logout()}>Keluar</button>
        </div>
      {:else if isDesktop()}
        <div style="padding:4px 18px 16px;display:flex;flex-direction:column;gap:10px">
          <input class="input" placeholder="Email / no. HP / username" bind:value={ident} />
          <input class="input" type="password" placeholder="Password" bind:value={pass} />
          {#if loginErr}<div style="color:var(--bad);font-size:12.5px">{loginErr}</div>{/if}
          <button class="btn p" on:click={doLogin} disabled={loggingIn || !ident || !pass}>{loggingIn ? "Masuk…" : "Masuk FotoYu"}</button>
        </div>
      {:else}
        <div style="padding:4px 18px 16px" class="muted">Masuk akun FotoYu lewat aplikasi di laptop.</div>
      {/if}
    </div>
  {/if}
</div>
