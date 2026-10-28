package torrent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	atorrent "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/google/uuid"

	"stashflow/internal/storage"
)

const (
	errFailedMetadata     = "failed to read torrent metadata"
	errFailedStorageStats = "failed to read storage stats"
	errTemporaryStorage   = "not enough storage available"
	errDuplicateTorrent   = "torrent already queued"
)

type storageDecision int

const (
	storageDecisionFit storageDecision = iota
	storageDecisionTemporaryShortage
	storageDecisionPermanentShortage
)

type transitionStats struct {
	Attachments     int64 `json:"attachments"`
	Drops           int64 `json:"drops"`
	Starts          int64 `json:"starts"`
	MetadataFetches int64 `json:"metadata_fetches"`
	Pauses          int64 `json:"pauses"`
	Resumes         int64 `json:"resumes"`
}

type Diagnostics struct {
	ActiveID         string               `json:"active_id"`
	AttachedTorrents int                  `json:"attached_torrents"`
	Queue            QueueCounts          `json:"queue"`
	Transitions      transitionStats      `json:"transitions"`
	Resources        ResourceStats        `json:"resources"`
	Storage          *storage.Stats       `json:"storage"`
	StorageError     string               `json:"storage_error,omitempty"`
	ActivePeers      int                  `json:"active_peers"`
	ClientStats      atorrent.ClientStats `json:"client_stats"`
}

type Manager struct {
	mu                sync.Mutex
	client            *atorrent.Client
	items             map[string]*Item
	torrents          map[string]*atorrent.Torrent
	order             []string
	activeID          string
	statePath         string
	storagePath       string
	maxUsagePercent   float64
	torrentDir        string
	onChange          func()
	transitions       transitionStats
	closed            bool
	done              chan struct{}
	workers           sync.WaitGroup
	backgroundStarted bool
}

func NewManager(storagePath, torrentDir, statePath string, maxUsagePercent float64) (*Manager, error) {
	cfg := newClientConfig(storagePath)

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
		done:            make(chan struct{}),
	}

	if err := m.loadState(); err != nil {
		_ = client.Close()
		return nil, err
	}

	return m, nil
}

func newClientConfig(storagePath string) *atorrent.ClientConfig {
	cfg := atorrent.NewDefaultClientConfig()
	cfg.DataDir = storagePath
	cfg.NoDefaultPortForwarding = true
	cfg.ListenPort = 0
	cfg.NoDHT = false
	cfg.DisablePEX = false
	cfg.DisableWebtorrent = true
	cfg.DisableWebseeds = false
	cfg.NoUpload = true
	cfg.MaxUnverifiedBytes = 32 << 20
	cfg.EstablishedConnsPerTorrent = 40
	cfg.HalfOpenConnsPerTorrent = 16
	cfg.TotalHalfOpenConns = 32
	cfg.MaxAllocPeerRequestDataPerConn = 256 << 10
	cfg.PieceHashersPerTorrent = 1
	return cfg
}

func (m *Manager) SetOnChange(fn func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onChange = fn
}

func (m *Manager) SetMaxUsagePercent(pct float64) {
	m.mu.Lock()
	m.maxUsagePercent = pct
	m.mu.Unlock()
}

func (m *Manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	if m.done != nil {
		close(m.done)
	}
	for id := range m.torrents {
		m.dropTorrentLocked(id)
	}
	m.mu.Unlock()
	m.workers.Wait()
	if m.client == nil {
		return nil
	}
	errs := m.client.Close()
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func (m *Manager) StartBackground() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.backgroundStarted {
		return
	}
	if m.done == nil {
		m.done = make(chan struct{})
	}
	m.backgroundStarted = true
	m.workers.Add(2)
	go func() { defer m.workers.Done(); m.progressLoop() }()
	go func() { defer m.workers.Done(); m.storageLoop() }()
}

func (m *Manager) progressLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.done:
			return
		case <-ticker.C:
		}
		m.mu.Lock()
		for id, item := range m.items {
			if item.Status != StatusDownloading {
				continue
			}
			t := m.torrents[id]
			if t == nil || t.Info() == nil {
				continue
			}
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
				item.ErrorMessage = ""
				m.activeID = ""
				m.dropTorrentLocked(id)
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
	for {
		select {
		case <-m.done:
			return
		case <-ticker.C:
		}
		m.CheckAndStartNext()
	}
}

func (m *Manager) State() *StateView {
	m.mu.Lock()
	defer m.mu.Unlock()
	metadataID := ""
	for id := range m.torrents {
		if item := m.items[id]; item != nil && item.Status == StatusQueued {
			metadataID = id
		}
	}
	items := make([]*Item, 0, len(m.order))
	for _, id := range m.order {
		if it, ok := m.items[id]; ok {
			copyItem := *it
			items = append(items, &copyItem)
		}
	}
	return &StateView{
		Items:      items,
		Order:      append([]string{}, m.order...),
		ActiveID:   m.activeID,
		MetadataID: metadataID,
	}
}

func (m *Manager) Diagnostics() Diagnostics {
	m.mu.Lock()
	defer m.mu.Unlock()

	diag := Diagnostics{
		ActiveID:         m.activeID,
		AttachedTorrents: len(m.torrents),
		Transitions:      m.transitions,
		Queue:            countStatuses(m.items),
	}
	diag.Resources = resourceSnapshot()
	var err error
	diag.Storage, err = storage.Stat(m.storagePath, m.maxUsagePercent)
	if err != nil {
		diag.StorageError = err.Error()
	}
	for _, t := range m.torrents {
		diag.ActivePeers += t.Stats().ActivePeers
	}
	if m.client != nil {
		diag.ClientStats = m.client.Stats()
	}
	return diag
}

func (m *Manager) AddMagnet(magnetURI string) (*Item, error) {
	m.mu.Lock()
	magnet, err := metainfo.ParseMagnetUri(magnetURI)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	infoHash := magnet.InfoHash.HexString()
	if duplicate := m.findQueuedDuplicateLocked(infoHash, ""); duplicate != nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("%s: %s", errDuplicateTorrent, duplicate.NameOrID())
	}

	item := &Item{
		ID:       uuid.NewString(),
		Name:     magnet.DisplayName,
		InfoHash: infoHash,
		Status:   StatusQueued,
		Progress: 0,
		AddedAt:  time.Now(),
		Magnet:   magnetURI,
	}

	m.items[item.ID] = item
	m.order = append(m.order, item.ID)
	m.saveStateLocked()

	result := *item
	changed := m.onChange
	m.mu.Unlock()
	m.notifyChange(changed)
	return &result, nil
}

func (m *Manager) AddTorrentFile(filename string, data []byte) (*Item, error) {
	m.mu.Lock()
	mi, info, err := parseTorrentMetadata(data)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	infoHash := mi.HashInfoBytes().HexString()
	if duplicate := m.findQueuedDuplicateLocked(infoHash, ""); duplicate != nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("%s: %s", errDuplicateTorrent, duplicate.NameOrID())
	}

	id := uuid.NewString()
	target := filepath.Join(m.torrentDir, id+".torrent")
	if err := os.WriteFile(target, data, 0o644); err != nil {
		m.mu.Unlock()
		return nil, err
	}

	item := &Item{
		ID:          id,
		Name:        info.Name,
		InfoHash:    infoHash,
		SizeBytes:   info.TotalLength(),
		Status:      StatusQueued,
		Progress:    0,
		AddedAt:     time.Now(),
		TorrentFile: target,
	}
	if item.Name == "" {
		item.Name = filename
	}

	if _, err := m.applyStorageResultLocked(item); err != nil {
		m.mu.Unlock()
		return nil, err
	}

	m.items[id] = item
	m.order = append(m.order, id)
	m.saveStateLocked()

	result := *item
	changed := m.onChange
	m.mu.Unlock()
	m.notifyChange(changed)
	return &result, nil
}

func (m *Manager) Remove(id string) error {
	m.mu.Lock()
	_, ok := m.items[id]
	if !ok {
		m.mu.Unlock()
		return errors.New("not found")
	}

	m.dropTorrentLocked(id)
	if m.activeID == id {
		m.activeID = ""
	}

	item := m.items[id]
	delete(m.items, id)
	m.order = filterOrder(m.order, id)
	m.saveStateLocked()
	changed := m.onChange
	m.mu.Unlock()

	if item != nil && item.TorrentFile != "" {
		_ = os.Remove(item.TorrentFile)
	}
	if changed != nil {
		changed()
	}
	return nil
}

func (m *Manager) Pause(id string) error {
	m.mu.Lock()
	item, ok := m.items[id]
	if !ok {
		m.mu.Unlock()
		return errors.New("not found")
	}
	if item.Status == StatusCompleted || item.Status == StatusError {
		m.mu.Unlock()
		return errors.New("cannot pause completed or errored item")
	}
	if item.Status == StatusPaused {
		m.mu.Unlock()
		return nil
	}

	m.dropTorrentLocked(id)
	if m.activeID == id {
		m.activeID = ""
	}
	item.Status = StatusPaused
	item.ErrorMessage = ""
	m.transitions.Pauses++
	m.saveStateLocked()
	changed := m.onChange
	m.mu.Unlock()

	if changed != nil {
		changed()
	}
	m.CheckAndStartNext()
	return nil
}

func (m *Manager) Resume(id string) error {
	m.mu.Lock()
	item, ok := m.items[id]
	if !ok {
		m.mu.Unlock()
		return errors.New("not found")
	}
	if item.Status != StatusPaused {
		m.mu.Unlock()
		return errors.New("item is not paused")
	}

	item.Status = StatusQueued
	item.ErrorMessage = ""
	m.transitions.Resumes++
	m.saveStateLocked()
	changed := m.onChange
	m.mu.Unlock()

	if changed != nil {
		changed()
	}
	m.CheckAndStartNext()
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
	defer m.mu.Unlock()
	// Metadata discovery owns the same single slot as downloading, even after reordering.
	if m.closed || len(m.torrents) > 0 {
		return
	}

	if m.activeID != "" {
		if item, ok := m.items[m.activeID]; ok && item.Status == StatusDownloading {
			return
		}
		m.activeID = ""
	}

	stats, err := storage.Stat(m.storagePath, m.maxUsagePercent)
	if err != nil {
		return
	}

	for _, id := range m.order {
		item := m.items[id]
		if item == nil || item.Status != StatusQueued {
			continue
		}

		if item.SizeBytes <= 0 {
			if item.Magnet == "" || m.client == nil || stats.AvailableForNew == 0 {
				continue
			}
			if m.torrents[id] != nil {
				return
			}

			t, err := m.client.AddMagnet(item.Magnet)
			if err != nil {
				item.Status = StatusError
				item.ErrorMessage = err.Error()
				m.saveStateLocked()
				go m.notifyChange(m.onChange)
				continue
			}
			if item.Name != "" {
				t.SetDisplayName(item.Name)
			}
			m.torrents[id] = t
			m.transitions.Attachments++
			m.transitions.MetadataFetches++
			slog.Info("torrent metadata fetch", "id", id)
			m.saveStateLocked()
			go m.notifyChange(m.onChange)
			m.workers.Add(1)
			go func() { defer m.workers.Done(); m.resolveMetadata(id, t) }()
			return
		}

		canFit, _ := storage.CanFit(stats, uint64(item.SizeBytes))
		if !canFit || m.client == nil {
			continue
		}

		t := m.torrents[id]
		if t == nil {
			attached, err := m.attachTorrentLocked(item)
			if err != nil {
				item.Status = StatusError
				item.ErrorMessage = err.Error()
				m.saveStateLocked()
				go m.notifyChange(m.onChange)
				continue
			}
			t = attached
		}

		item.Status = StatusDownloading
		item.ErrorMessage = ""
		m.activeID = id
		m.transitions.Starts++
		slog.Info("torrent started", "id", id)
		m.saveStateLocked()
		go m.notifyChange(m.onChange)
		m.workers.Add(1)
		go func() { defer m.workers.Done(); m.awaitInfoAndDownload(id, t) }()
		return
	}
}

func (m *Manager) awaitInfoAndDownload(id string, t *atorrent.Torrent) {
	if !waitForTorrentInfo(t) {
		m.mu.Lock()
		item, ok := m.items[id]
		if ok && m.torrents[id] == t {
			if item.Status == StatusDownloading {
				item.Status = StatusError
				item.ErrorMessage = errFailedMetadata
				m.activeID = ""
			}
			m.dropTorrentLocked(id)
			m.saveStateLocked()
			changed := m.onChange
			m.mu.Unlock()
			m.notifyChange(changed)
			m.CheckAndStartNext()
			return
		}
		m.mu.Unlock()
		return
	}

	m.mu.Lock()
	item, ok := m.items[id]
	if !ok || m.torrents[id] != t {
		m.mu.Unlock()
		t.Drop()
		return
	}
	if item.Status != StatusDownloading {
		m.mu.Unlock()
		return
	}
	if t.Info() == nil {
		item.Status = StatusError
		item.ErrorMessage = errFailedMetadata
		m.activeID = ""
		m.dropTorrentLocked(id)
		m.saveStateLocked()
		changed := m.onChange
		m.mu.Unlock()
		m.notifyChange(changed)
		m.CheckAndStartNext()
		return
	}

	item.Name = chooseName(item.Name, t.Name())
	item.SizeBytes = t.Info().TotalLength()
	if err := m.cacheMetadataLocked(item, t); err != nil {
		item.Status = StatusError
		item.ErrorMessage = "failed to save torrent metadata: " + err.Error()
		m.activeID = ""
		m.dropTorrentLocked(id)
		m.saveStateLocked()
		changed := m.onChange
		m.mu.Unlock()
		m.notifyChange(changed)
		m.CheckAndStartNext()
		return
	}
	item.ErrorMessage = ""
	m.saveStateLocked()
	changed := m.onChange
	t.DownloadAll()
	m.mu.Unlock()
	m.notifyChange(changed)
}

func (m *Manager) resolveMetadata(id string, t *atorrent.Torrent) {
	if !waitForTorrentInfo(t) {
		m.mu.Lock()
		if m.torrents[id] == t {
			m.dropTorrentLocked(id)
			m.saveStateLocked()
			changed := m.onChange
			m.mu.Unlock()
			m.notifyChange(changed)
			m.CheckAndStartNext()
			return
		}
		m.mu.Unlock()
		return
	}

	m.mu.Lock()
	item, ok := m.items[id]
	if !ok || m.torrents[id] != t {
		m.mu.Unlock()
		t.Drop()
		return
	}
	if item.Status == StatusPaused {
		m.dropTorrentLocked(id)
		m.saveStateLocked()
		changed := m.onChange
		m.mu.Unlock()
		m.notifyChange(changed)
		return
	}
	if t.Info() == nil {
		item.Status = StatusError
		item.ErrorMessage = errFailedMetadata
		m.dropTorrentLocked(id)
		m.saveStateLocked()
		changed := m.onChange
		m.mu.Unlock()
		m.notifyChange(changed)
		m.CheckAndStartNext()
		return
	}

	item.Name = chooseName(item.Name, t.Name())
	item.SizeBytes = t.Info().TotalLength()
	if err := m.cacheMetadataLocked(item, t); err != nil {
		item.Status = StatusError
		item.ErrorMessage = "failed to save torrent metadata: " + err.Error()
		m.activeID = ""
		m.dropTorrentLocked(id)
		m.saveStateLocked()
		changed := m.onChange
		m.mu.Unlock()
		m.notifyChange(changed)
		m.CheckAndStartNext()
		return
	}
	decision, err := m.applyStorageResultLocked(item)
	if err != nil {
		m.dropTorrentLocked(id)
		m.saveStateLocked()
		changed := m.onChange
		m.mu.Unlock()
		m.notifyChange(changed)
		m.CheckAndStartNext()
		return
	}

	changed := m.onChange
	switch decision {
	case storageDecisionFit:
		item.Status = StatusDownloading
		item.ErrorMessage = ""
		m.activeID = id
		m.transitions.Starts++
		slog.Info("torrent started", "id", id)
		m.saveStateLocked()
		t.DownloadAll()
		m.mu.Unlock()
		m.notifyChange(changed)
	case storageDecisionTemporaryShortage:
		item.Status = StatusQueued
		item.ErrorMessage = ""
		m.dropTorrentLocked(id)
		m.saveStateLocked()
		m.mu.Unlock()
		m.notifyChange(changed)
		m.CheckAndStartNext()
	case storageDecisionPermanentShortage:
		item.Status = StatusError
		m.dropTorrentLocked(id)
		m.saveStateLocked()
		m.mu.Unlock()
		m.notifyChange(changed)
		m.CheckAndStartNext()
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
	m.order = append([]string{}, state.Order...)
	for _, item := range state.Items {
		if item.Status == StatusDownloading {
			item.Status = StatusQueued
			item.Progress = 0
			item.Downloaded = 0
		}
		if item.Status == StatusError && item.ErrorMessage == errTemporaryStorage {
			item.Status = StatusQueued
			item.ErrorMessage = ""
		}

		if item.Magnet != "" && (item.Name == "" || item.InfoHash == "") {
			if magnet, err := metainfo.ParseMagnetUri(item.Magnet); err == nil {
				item.Name = magnet.DisplayName
				if item.InfoHash == "" {
					item.InfoHash = magnet.InfoHash.HexString()
				}
			}
		}
		if item.TorrentFile != "" && (item.Name == "" || item.SizeBytes == 0 || item.InfoHash == "") {
			if mi, info, err := loadTorrentMetadataFromFile(item.TorrentFile); err == nil {
				item.Name = chooseName(item.Name, info.Name)
				if item.SizeBytes == 0 {
					item.SizeBytes = info.TotalLength()
				}
				if item.InfoHash == "" {
					item.InfoHash = mi.HashInfoBytes().HexString()
				}
			}
		}

		m.items[item.ID] = item
	}
	return nil
}

func (m *Manager) saveStateLocked() {
	state := State{
		Items: []*Item{},
		Order: append([]string{}, m.order...),
	}
	seen := map[string]bool{}
	for _, id := range m.order {
		if item, ok := m.items[id]; ok {
			copyItem := *item
			state.Items = append(state.Items, &copyItem)
			seen[id] = true
		}
	}
	for id, item := range m.items {
		if seen[id] {
			continue
		}
		copyItem := *item
		state.Items = append(state.Items, &copyItem)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(m.statePath, data, 0o644)
}

func (m *Manager) attachTorrentLocked(item *Item) (*atorrent.Torrent, error) {
	if m.client == nil {
		return nil, errors.New("torrent client unavailable")
	}

	var (
		t   *atorrent.Torrent
		err error
	)
	switch {
	case item.TorrentFile != "":
		t, err = m.client.AddTorrentFromFile(item.TorrentFile)
	case item.Magnet != "":
		t, err = m.client.AddMagnet(item.Magnet)
		if err == nil && item.Name != "" {
			t.SetDisplayName(item.Name)
		}
	default:
		return nil, errors.New("torrent source missing")
	}
	if err != nil {
		return nil, err
	}

	m.torrents[item.ID] = t
	m.transitions.Attachments++
	slog.Info("torrent attached", "info_hash", t.InfoHash().HexString())
	return t, nil
}

// Cache metadata so waiting magnets and restarts do not repeat peer discovery.
func (m *Manager) cacheMetadataLocked(item *Item, t *atorrent.Torrent) error {
	if item.TorrentFile != "" {
		return nil
	}
	mi := t.Metainfo()
	var buf bytes.Buffer
	if err := mi.Write(&buf); err != nil {
		return err
	}
	target := filepath.Join(m.torrentDir, item.ID+".torrent")
	if err := os.WriteFile(target, buf.Bytes(), 0o644); err != nil {
		return err
	}
	item.TorrentFile = target
	return nil
}

func (m *Manager) dropTorrentLocked(id string) {
	t := m.torrents[id]
	if t == nil {
		return
	}
	t.Drop()
	delete(m.torrents, id)
	m.transitions.Drops++
	slog.Info("torrent detached", "id", id)
}

func (m *Manager) applyStorageResultLocked(item *Item) (storageDecision, error) {
	stats, err := storage.Stat(m.storagePath, m.maxUsagePercent)
	if err != nil {
		item.Status = StatusError
		item.ErrorMessage = errFailedStorageStats
		return storageDecisionPermanentShortage, err
	}
	canFit, reason := storage.CanFit(stats, uint64(item.SizeBytes))
	if canFit {
		if item.Status != StatusPaused {
			item.Status = StatusQueued
		}
		item.ErrorMessage = ""
		return storageDecisionFit, nil
	}
	if uint64(item.SizeBytes) > stats.MaxUsageBytes {
		item.Status = StatusError
		item.ErrorMessage = reason
		return storageDecisionPermanentShortage, nil
	}
	item.Status = StatusQueued
	item.ErrorMessage = ""
	return storageDecisionTemporaryShortage, nil
}

func notifyChange(fn func()) {
	if fn != nil {
		fn()
	}
}

func (m *Manager) notifyChange(fn func()) {
	notifyChange(fn)
}

func waitForTorrentInfo(t *atorrent.Torrent) bool {
	select {
	case <-t.GotInfo():
		return t.Info() != nil
	case <-t.Closed():
		return false
	}
}

func parseTorrentMetadata(data []byte) (*metainfo.MetaInfo, metainfo.Info, error) {
	mi, err := metainfo.Load(bytes.NewReader(data))
	if err != nil {
		return nil, metainfo.Info{}, err
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		return nil, metainfo.Info{}, err
	}
	return mi, info, nil
}

func loadTorrentMetadataFromFile(path string) (*metainfo.MetaInfo, metainfo.Info, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, metainfo.Info{}, err
	}
	return parseTorrentMetadata(data)
}

func chooseName(current, fallback string) string {
	if current != "" {
		return current
	}
	return fallback
}

func (m *Manager) findQueuedDuplicateLocked(infoHash string, excludeID string) *Item {
	if infoHash == "" {
		return nil
	}
	for id, item := range m.items {
		if id == excludeID || item == nil {
			continue
		}
		if item.InfoHash != infoHash {
			continue
		}
		if item.Status == StatusQueued || item.Status == StatusDownloading || item.Status == StatusPaused {
			return item
		}
	}
	return nil
}

func filterOrder(order []string, removeID string) []string {
	filtered := make([]string, 0, len(order))
	for _, id := range order {
		if id != removeID {
			filtered = append(filtered, id)
		}
	}
	return filtered
}

func countStatuses(items map[string]*Item) QueueCounts {
	var counts QueueCounts
	for _, item := range items {
		switch item.Status {
		case StatusQueued:
			counts.Queued++
		case StatusDownloading:
			counts.Downloading++
		case StatusPaused:
			counts.Paused++
		case StatusCompleted:
			counts.Completed++
		case StatusError:
			counts.Error++
		}
	}
	return counts
}

func (i *Item) NameOrID() string {
	if i == nil {
		return ""
	}
	if i.Name != "" {
		return i.Name
	}
	return i.ID
}
