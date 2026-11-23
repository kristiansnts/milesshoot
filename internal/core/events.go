package core

// Emitter memisahkan core dari cara pesan dikirim ke UI.
// Jendela desktop (Wails IPC) dan server HP (SSE) sama-sama mengimplementasikan ini,
// jadi core tidak pernah tahu/peduli tampilan di depannya.
type Emitter interface {
	Emit(event string, data any) // push realtime: "status", "change", dll.
	Log(level, msg string)       // satu baris catatan aktivitas
}

// MultiEmitter menyiarkan ke beberapa tujuan sekaligus (mis. desktop + semua HP yang memantau).
type MultiEmitter struct {
	targets []Emitter
}

func NewMultiEmitter(targets ...Emitter) *MultiEmitter { return &MultiEmitter{targets: targets} }

func (m *MultiEmitter) Add(e Emitter) { m.targets = append(m.targets, e) }

func (m *MultiEmitter) Emit(event string, data any) {
	for _, t := range m.targets {
		if t != nil {
			t.Emit(event, data)
		}
	}
}

func (m *MultiEmitter) Log(level, msg string) {
	for _, t := range m.targets {
		if t != nil {
			t.Log(level, msg)
		}
	}
}

// NopEmitter: tujuan kosong (untuk tes / sebelum UI tersambung).
type NopEmitter struct{}

func (NopEmitter) Emit(string, any)   {}
func (NopEmitter) Log(string, string) {}
