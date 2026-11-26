package core

import (
	"milesshoot/internal/fotoyu"
)

// Engine merangkai seluruh core: config, store, scanner, queue, client FotoYu, processor.
// Tidak bergantung pada Wails/HTTP — UI apa pun (desktop IPC atau server HP) memakainya lewat Emitter.
type Engine struct {
	Cfg     *Config
	Store   *Store
	Proc    *Processor
	Fotoyu  *fotoyu.Client
	Queue   *UploadQueue
	Scanner *Scanner
	Hires   *HiresManager
	Ranking *RankingManager
	emit    *MultiEmitter
}

func NewEngine(dataDir string, emitters ...Emitter) (*Engine, error) {
	cfg, err := LoadConfig(dataDir)
	if err != nil {
		return nil, err
	}
	store := LoadStore(dataDir)
	proc := NewProcessor(dataDir)
	fy := fotoyu.New(dataDir)
	me := NewMultiEmitter(emitters...)

	uploaders := map[string]Uploader{
		"local":  LocalUploader{},
		"fotoyu": NewFotoyuUploader(fy),
	}
	q := NewQueue(cfg, store, proc, uploaders, me, fy.ActiveUsername)
	sc := NewScanner(ScannerOpts{
		Config:    cfg,
		IsKnown:   q.isKnownPath,
		OnReady:   q.AddFile,
		SkipDir:   func(d string) bool { return InArchive(cfg.Get(), d) },
		IntervalS: func() int { return cfg.Get().ScanIntervalSec },
	})

	e := &Engine{Cfg: cfg, Store: store, Proc: proc, Fotoyu: fy, Queue: q, Scanner: sc, emit: me}
	e.Hires = NewHiresManager(cfg, store, fy, me)
	e.Ranking = NewRankingManager(dataDir, fy, me)
	return e, nil
}

// AddEmitter menyambungkan tujuan event baru (mis. HP yang mulai memantau).
func (e *Engine) AddEmitter(em Emitter) { e.emit.Add(em) }

// Log meneruskan catatan dari komponen luar core (mis. server FTP) ke semua UI.
func (e *Engine) Log(level, msg string) { e.emit.Log(level, msg) }

func (e *Engine) Start() {
	e.Queue.Start()
	e.Scanner.Start()
	e.Hires.StartAutoLoop()
	e.emit.Emit("status", e.Status())
}

func (e *Engine) Stop() {
	e.Scanner.Stop()
	e.Queue.Stop()
	e.Hires.StopAutoLoop()
	e.Store.Flush()
}

// Status: ringkasan lengkap untuk UI (desktop & HP memakai bentuk yang sama).
type Status struct {
	App         string             `json:"app"`
	Paused      bool               `json:"paused"`
	PauseReason string             `json:"pauseReason"`
	PauseMsg    string             `json:"pauseMsg"`
	Counts      Counts             `json:"counts"`
	Scan        ScanSummary        `json:"scan"`
	Account     fotoyu.SessionInfo `json:"account"`
	PerMinute   int                `json:"perMinute"`
	Destination string             `json:"destination"`
	Event       Event              `json:"event"`
	Performance string             `json:"performance"`
	Speed       Speed              `json:"speed"`
	Folders     []FolderStat       `json:"folders"` // progres per folder sumber (cuma berarti saat >1 folder aktif)
}

func (e *Engine) Status() Status {
	cfg := e.Cfg.Get()
	paused, reason, msg := e.Queue.Paused()
	counts := e.Store.Counts()
	return Status{
		App:         "milesshoot",
		Paused:      paused,
		PauseReason: reason,
		PauseMsg:    msg,
		Counts:      counts,
		Scan:        e.Scanner.SummarySnapshot(),
		Account:     e.Fotoyu.Session(),
		PerMinute:   e.Queue.SpeedPerMinute(),
		Destination: cfg.Destination,
		Event:       cfg.Event,
		Performance: cfg.Performance,
		Speed:       e.Queue.Speed(counts.Pending + counts.Uploading),
		Folders:     e.Queue.FolderProgress(),
	}
}

// BroadcastStatus dipanggil berkala / setelah perubahan untuk mendorong status ke semua UI.
func (e *Engine) BroadcastStatus() { e.emit.Emit("status", e.Status()) }
