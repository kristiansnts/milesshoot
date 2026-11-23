<script>
  import { status } from "../lib/stores";
  $: st = $status;
  $: acct = st && st.account;
  $: who = acct && acct.username ? "@" + acct.username : "belum masuk";
  const check = '<svg viewBox="0 0 24 24"><path d="M20 6L9 17l-5-5"/></svg>';
  const plans = [
    { n: "Gratis", p: "Rp0", feats: ["Batas 500 foto", "Semua fitur unggah", "1 akun FotoYu"], cur: true },
    { n: "Bulanan", p: "Rp49rb", u: " /bln", feats: ["Tanpa batas foto", "Hi-res & video", "Pembaruan otomatis"], go: true },
    { n: "Tahunan", p: "Rp399rb", u: " /thn", feats: ["Semua fitur Bulanan", "Prioritas saat sibuk", "Dukungan diutamakan"], rec: true },
    { n: "Selamanya", p: "Rp999rb", u: " sekali", feats: ["Bayar sekali, selamanya", "Semua pembaruan", "1 akun FotoYu"], ghost: true },
  ];
</script>

<div class="head-row">
  <div><h1 class="ttl">Lisensi &amp; Upgrade</h1><div class="sub">1 lisensi untuk 1 akun FotoYu · aktivasi kode dan pilihan paket</div></div>
  <span class="pill wait" style="padding:7px 13px">Gratis</span>
</div>

<div class="single">
  <div class="stats">
    <div class="stat lav"><span class="sl">Status</span><div class="sv">Aktif</div><div class="sm">paket gratis</div></div>
    <div class="stat grey"><span class="sl">Akun terkait</span><div class="sv" style="font-size:20px">{who}</div><div class="sm">1 lisensi = 1 akun</div></div>
    <div class="stat but"><span class="sl">Terpakai</span><div class="sv num">128<span style="font-size:16px;opacity:.7"> / 500</span></div><div class="sm">foto gratis · sisa 372</div></div>
  </div>

  <div class="panel">
    <div class="ph"><h3>Paket Gratis</h3><span class="pc">tanpa batas waktu · batas 500 foto</span></div>
    <div style="padding:0 18px 4px"><div class="bar" style="height:9px"><i style="width:25.6%;background:var(--warn)"></i></div>
      <div style="display:flex;justify-content:space-between;font-size:12px;color:var(--muted);margin-top:7px;font-weight:600"><span class="num">128 / 500 foto</span><span>upgrade untuk unggah tanpa batas</span></div></div>
    <div class="ph" style="padding-top:14px"><h3 style="font-size:13.5px">Sudah punya kode lisensi?</h3></div>
    <div style="display:flex;gap:10px;padding:0 18px 18px;flex-wrap:wrap">
      <input class="input" style="flex:1;min-width:200px" placeholder="MEDY-XXXXX-XXXXX" />
      <button class="btn p">Aktifkan</button>
    </div>
  </div>

  <div class="panel">
    <div class="ph"><h3>Pilih paket</h3><span class="pc">bayar via QRIS / transfer · lisensi langsung aktif untuk {who}</span></div>
    <div class="plans">
      {#each plans as pl}
        <div class="plan" class:rec={pl.rec}>
          {#if pl.rec}<span class="rib">Hemat 2 bulan</span>{/if}
          <div class="pn">{pl.n}</div>
          <div class="pp">{pl.p}{#if pl.u}<u>{pl.u}</u>{/if}</div>
          <div style="display:flex;flex-direction:column;gap:8px;margin:2px 0 4px">
            {#each pl.feats as f}<div class="fi"><svg viewBox="0 0 24 24"><path d="M20 6L9 17l-5-5"/></svg>{f}</div>{/each}
          </div>
          {#if pl.cur}<div class="pbtn cur">Paket sekarang</div>
          {:else}<button class="pbtn {pl.ghost ? 'ghost' : 'go'}">Pilih &amp; bayar</button>{/if}
        </div>
      {/each}
    </div>
  </div>

  <p class="muted" style="font-size:12px;padding:0 2px">Catatan: layar lisensi masih desain — logika lisensi (trial, aktivasi, pembayaran) belum tersambung ke backend.</p>
</div>
