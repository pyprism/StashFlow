package torrent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	atorrent "github.com/anacrolix/torrent"
	"github.com/google/uuid"

	"stashflow/internal/storage"
)

type Manager struct {
	mu              sync.Mutex
	client          *atorrent.Client
	items           map[string]*Item
	torrents        map[string]*atorrent.Torrent
	order           []string
	activeID        string
	statePath       string
	storagePath     string
	maxUsagePercent float64
	torrentDir      string
	onChange        func()
}

func NewManager(storagePath, torrentDir, statePath string, maxUsagePercent float64) (*Manager, error) {
	cfg := atorrent.NewDefaultClientConfig()
	cfg.DataDir = storagePath
	cfg.NoDefaultPortForwarding = true
	cfg.ListenPort = 0

	client, err := atorrent.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("torrent client: %w", err)
	}

	m := &Manager{
		client:          client,
		items:           map[string]*Item{},
		torrents:        map[string]*atorrent.Torrent{},
		order:           []string{},
		statePath:       statePath,
		storagePath:     storagePath,
		maxUsagePercent: maxUsagePercent,
		torrentDir:      torrentDir,
	}

	if err := m.loadState(); err != nil {
		return nil, err
	}

	return m, nil
}

func (m *Manager) SetOnChange(fn func()) {
	m.onChange = fn
}

func (m *Manager) SetMaxUsagePercent(pct float64) {
	m.mu.Lock()
	m.maxUsagePercent = pct
	m.mu.Unlock()
}

func (m *Manager) Close() error {
	errs := m.client.Close()
	if len(errs) == 0 {
		return nil
	}
	return errs[0]
}

func (m *Manager) StartBackground() {
	go m.progressLoop()
	go m.storageLoop()
}

func (m *Manager) progressLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		m.mu.Lock()
		for id, item := range m.items {
			if item.Status != StatusDownloading {
				continue
			}
			t := m.torrents[id]
			if t == nil {
				continue
			}
			if t.Info() != nil {
				total := t.Info().TotalLength()
				item.SizeBytes = total
				item.Downloaded = t.BytesCompleted()
				if total > 0 {
					item.Progress = float64(item.Downloaded) / float64(total)
				}
				if total > 0 && item.Downloaded >= total {
					item.Status = StatusCompleted
					item.Progress = 1
					item.Downloaded = total
					m.activeID = ""
					t.Drop()
					delete(m.torrents, id)
				}
			}
		}
		m.saveStateLocked()
		changed := m.onChange
		m.mu.Unlock()

		if changed != nil {
			changed()
		}
		m.CheckAndStartNext()
	}
}

func (m *Manager) storageLoop() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		m.CheckAndStartNext()
	}
}

func (m *Manager) State() *StateView {
	m.mu.Lock()
	defer m.mu.Unlock()
	items := make([]*Item, 0, len(m.order))
	for _, id := range m.order {
		if it, ok := m.items[id]; ok {
			items = append(items, it)
		}
	}
	return &StateView{Items: items, Order: append([]string{}, m.order...), ActiveID: m.activeID}
}

func (m *Manager) AddMagnet(magnet string) (*Item, error) {
	m.mu.Lock()

	id := uuid.NewString()
	t, err := m.client.AddMagnet(magnet)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}

	item := &Item{
		ID:       id,
		Status:   StatusQueued,
		Progress: 0,
		AddedAt:  time.Now(),
		Magnet:   magnet,
	}

	m.items[id] = item
	m.torrents[id] = t
	m.order = append(m.order, id)
	m.saveStateLocked()

	go m.populateInfo(id, t)
	result := *item
	changed := m.onChange
	m.mu.Unlock()

	if changed != nil {
		changed()
	}

	return &result, nil
}

func (m *Manager) AddTorrentFile(filename string, data []byte) (*Item, error) {
	m.mu.Lock()

	id := uuid.NewString()
	target := filepath.Join(m.torrentDir, id+".torrent")
	if err := os.WriteFile(target, data, 0o644); err != nil {
		m.mu.Unlock()
		return nil, err
	}

	t, err := m.client.AddTorrentFromFile(target)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}

	item := &Item{
		ID:          id,
		Status:      StatusQueued,
		Progress:    0,
		AddedAt:     time.Now(),
		TorrentFile: target,
	}

	m.items[id] = item
	m.torrents[id] = t
	m.order = append(m.order, id)
	m.saveStateLocked()

	go m.populateInfo(id, t)
	result := *item
	changed := m.onChange
	m.mu.Unlock()

	if changed != nil {
		changed()
	}

	return &result, nil
}

func (m *Manager) populateInfo(id string, t *atorrent.Torrent) {
	<-t.GotInfo()

	m.mu.Lock()
	item, ok := m.items[id]
	if !ok {
		m.mu.Unlock()
		return
	}
	if t.Info() == nil {
		item.Status = StatusError
		item.ErrorMessage = "failed to read torrent metadata"
		m.saveStateLocked()
		changed := m.onChange
		m.mu.Unlock()
		if changed != nil {
			changed()
		}
		return
	}
	total := t.Info().TotalLength()
	item.Name = t.Name()
	item.SizeBytes = total

	stats, err := storage.Stat(m.storagePath, m.maxUsagePercent)
	if err != nil {
		item.Status = StatusError
		item.ErrorMessage = "failed to read storage stats"
		m.saveStateLocked()
		changed := m.onChange
		m.mu.Unlock()
		if changed != nil {
			changed()
		}
		return
	}
	canFit, reason := storage.CanFit(stats, uint64(total))
	if !canFit {
		if uint64(total) > stats.MaxUsageBytes {
			// Permanent: torrent is larger than max allowed — will never fit.
			item.Status = StatusError
			item.ErrorMessage = reason
			t.Drop()
			delete(m.torrents, id)
			m.saveStateLocked()
			changed := m.onChange
			m.mu.Unlock()
			if changed != nil {
				changed()
			}
			return
		}
		// Temporary: not enough space right now.
		// Leave as StatusQueued — CheckAndStartNext will retry
		// when storage is freed.
	}

	m.saveStateLocked()
	changed := m.onChange
	m.mu.Unlock()

	if changed != nil {
		changed()
	}
	m.CheckAndStartNext()

	// If the item is still queued after CheckAndStartNext, drop the torrent
	// from the client to prevent tracker announces and peer connections while idle.
	m.mu.Lock()
	if it, ok := m.items[id]; ok && it.Status == StatusQueued {
		if ct := m.torrents[id]; ct != nil {
			ct.Drop()
			delete(m.torrents, id)
		}
	}
	m.mu.Unlock()
}

func (m *Manager) Remove(id string) error {
	m.mu.Lock()
	_, ok := m.items[id]
	if !ok {
		m.mu.Unlock()
		return errors.New("not found")
	}
	if t := m.torrents[id]; t != nil {
		t.Drop()
	}
	if m.activeID == id {
		m.activeID = ""
	}
	delete(m.torrents, id)
	delete(m.items, id)
	newOrder := make([]string, 0, len(m.order))
	for _, oid := range m.order {
		if oid != id {
			newOrder = append(newOrder, oid)
		}
	}
	m.order = newOrder
	m.saveStateLocked()
	changed := m.onChange
	m.mu.Unlock()

	if changed != nil {
		changed()
	}
	return nil
}

func (m *Manager) Reorder(order []string) {
	m.mu.Lock()
	seen := map[string]bool{}
	newOrder := make([]string, 0, len(m.order))
	for _, id := range order {
		if _, ok := m.items[id]; ok {
			newOrder = append(newOrder, id)
			seen[id] = true
		}
	}
	for _, id := range m.order {
		if !seen[id] {
			newOrder = append(newOrder, id)
		}
	}
	m.order = newOrder
	m.saveStateLocked()
	changed := m.onChange
	m.mu.Unlock()

	if changed != nil {
		changed()
	}
}

func (m *Manager) CheckAndStartNext() {
	m.mu.Lock()

	if m.activeID != "" {
		if item, ok := m.items[m.activeID]; ok && item.Status == StatusDownloading {
			m.mu.Unlock()
			return
		}
		m.activeID = ""
	}

	stats, err := storage.Stat(m.storagePath, m.maxUsagePercent)
	if err != nil {
		m.mu.Unlock()
		return
	}

	for _, id := range m.order {
		item := m.items[id]
		if item.Status != StatusQueued {
			continue
		}
		if item.SizeBytes <= 0 {
			continue
		}
		canFit, _ := storage.CanFit(stats, uint64(item.SizeBytes))
		if !canFit {
			continue
		}

		t := m.torrents[id]
		if t == nil {
			// Torrent was dropped while queued to prevent tracker hits.
			// Re-add it now that we're ready to download.
			var err error
			if item.Magnet != "" {
				t, err = m.client.AddMagnet(item.Magnet)
			} else if item.TorrentFile != "" {
				t, err = m.client.AddTorrentFromFile(item.TorrentFile)
			}
			if err != nil || t == nil {
				continue
			}
			m.torrents[id] = t
		}

		// Mark as downloading and set active before releasing the lock
		// to prevent another goroutine from starting a second download.
		item.Status = StatusDownloading
		item.ErrorMessage = ""
		m.activeID = id
		m.saveStateLocked()
		changed := m.onChange
		m.mu.Unlock()
		if changed != nil {
			changed()
		}

		// Wait for info and start download in background.
		// For .torrent files info is available immediately;
		// for magnets it requires a tracker round-trip.
		go m.awaitInfoAndDownload(id, t)
		return
	}
	m.mu.Unlock()
}

func (m *Manager) awaitInfoAndDownload(id string, t *atorrent.Torrent) {
	<-t.GotInfo()

	m.mu.Lock()
	item, ok := m.items[id]
	if !ok {
		// Item was removed while waiting for info
		t.Drop()
		delete(m.torrents, id)
		m.mu.Unlock()
		return
	}
	if t.Info() == nil {
		item.Status = StatusError
		item.ErrorMessage = "failed to read torrent metadata"
		m.activeID = ""
		m.saveStateLocked()
		changed := m.onChange
		m.mu.Unlock()
		if changed != nil {
			changed()
		}
		return
	}
	t.DownloadAll()
	m.saveStateLocked()
	changed := m.onChange
	m.mu.Unlock()
	if changed != nil {
		changed()
	}
}

func (m *Manager) loadState() error {
	data, err := os.ReadFile(m.statePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return err
	}
	m.items = map[string]*Item{}
	for _, item := range state.Items {
		if item.Status == StatusDownloading {
			item.Status = StatusQueued
			item.Progress = 0
			item.Downloaded = 0
		}
		// Recover items that failed due to temporary storage shortage.
		// They will be retried when CheckAndStartNext runs.
		if item.Status == StatusError && item.ErrorMessage == "not enough storage available" {
			item.Status = StatusQueued
			item.ErrorMessage = ""
		}
		m.items[item.ID] = item
	}
	m.order = state.Order

	// Reattach torrents for queued items that still need metadata.
	// Items that already have metadata (Name is set) are kept idle —
	// they will be re-added to the client by CheckAndStartNext when
	// it is time to download, avoiding unnecessary tracker hits.
	for _, item := range state.Items {
		if item.Status != StatusQueued {
			continue
		}
		if item.Name != "" {
			// Already have metadata; no need to add to client yet.
			continue
		}
		if item.Magnet != "" {
			t, err := m.client.AddMagnet(item.Magnet)
			if err == nil {
				m.torrents[item.ID] = t
				go m.populateInfo(item.ID, t)
			}
			continue
		}
		if item.TorrentFile != "" {
			if _, err := os.Stat(item.TorrentFile); err == nil {
				t, err := m.client.AddTorrentFromFile(item.TorrentFile)
				if err == nil {
					m.torrents[item.ID] = t
					go m.populateInfo(item.ID, t)
				}
			}
		}
	}

	return nil
}

func (m *Manager) saveStateLocked() {
	state := State{Items: []*Item{}, Order: m.order}
	for _, item := range m.items {
		state.Items = append(state.Items, item)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(m.statePath, data, 0o644)
}
