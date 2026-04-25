package bot

import (
	"log/slog"
	"os"
	"sync"
	"sync/atomic"

	tele "gopkg.in/telebot.v4"
)

// PhotoCache lazily uploads a local image to Telegram once and reuses
// the returned file_id for every subsequent send/edit. file_id is
// per-bot-token; restart of the bot re-acquires it on first send.
type PhotoCache struct {
	path   string
	fileID atomic.Pointer[string]
	mu     sync.Mutex
}

func NewPhotoCache(path string) *PhotoCache {
	if path != "" {
		if _, err := os.Stat(path); err != nil {
			slog.Warn("main menu image not found, falling back to text-only", "path", path, "err", err)
			path = ""
		}
	}
	return &PhotoCache{path: path}
}

// Build returns a tele.Photo ready to be sent. If we already captured
// a file_id, the photo references it (no upload). Otherwise it points
// at the local file (telebot will upload on first Send).
func (p *PhotoCache) Build(caption string) *tele.Photo {
	if p.path == "" {
		return nil
	}
	if id := p.fileID.Load(); id != nil && *id != "" {
		return &tele.Photo{File: tele.File{FileID: *id}, Caption: caption}
	}
	return &tele.Photo{File: tele.FromDisk(p.path), Caption: caption}
}

// Capture stores the file_id from a sent photo message so future sends
// skip the upload step. Safe to call repeatedly; first non-empty wins.
func (p *PhotoCache) Capture(m *tele.Message) {
	if m == nil || m.Photo == nil || m.Photo.FileID == "" {
		return
	}
	if p.fileID.Load() != nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.fileID.Load() != nil {
		return
	}
	id := m.Photo.FileID
	p.fileID.Store(&id)
}
