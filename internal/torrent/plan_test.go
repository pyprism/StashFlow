package torrent

import (
	"testing"
	"time"

	atorrent "github.com/anacrolix/torrent"
	"stashflow/internal/storage"
)

// Exercise real attachment and metadata behavior without external discovery.
func isolatedManager(t *testing.T) *Manager {
	t.Helper()
	m := newBareManager(t)
	cfg := newClientConfig(m.storagePath)
	cfg.NoDHT = true
	cfg.DisableTrackers = true
	cfg.DisablePEX = true
	cfg.DisableWebseeds = true
	client, err := atorrent.NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	m.client = client
	t.Cleanup(func() { _ = m.Close() })
	return m
}

func TestMetadataDiscoveryKeepsSingleSlotAfterReorder(t *testing.T) {
	m := isolatedManager(t)
	first, err := m.AddMagnet("magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567")
	if err != nil {
		t.Fatal(err)
	}
	m.CheckAndStartNext()
	if m.State().MetadataID != first.ID {
		t.Fatal("metadata activity is not exposed")
	}
	second, err := m.AddTorrentFile("test.torrent", []byte(minimalTorrentData))
	if err != nil {
		t.Fatal(err)
	}
	m.Reorder([]string{second.ID, first.ID})
	for range 3 {
		m.CheckAndStartNext()
	}
	d := m.Diagnostics()
	if d.AttachedTorrents != 1 || d.Transitions.MetadataFetches != 1 || d.Transitions.Starts != 0 {
		t.Fatalf("metadata must retain the only slot: %+v", d)
	}
	if len(m.client.Torrents()) != 1 {
		t.Fatal("queued file contacted the client")
	}
}

func TestMagnetWaitsWhenStorageHasNoHeadroom(t *testing.T) {
	m := isolatedManager(t)
	m.SetMaxUsagePercent(0)
	if _, err := m.AddMagnet("magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"); err != nil {
		t.Fatal(err)
	}
	m.CheckAndStartNext()
	if len(m.client.Torrents()) != 0 {
		t.Fatal("metadata discovery started without storage headroom")
	}
}

func TestResolvedMagnetCachesMetadataAndDropsWhenTooLarge(t *testing.T) {
	m := isolatedManager(t)
	mi, _, err := parseTorrentMetadata([]byte(minimalTorrentData))
	if err != nil {
		t.Fatal(err)
	}
	item, err := m.AddMagnet("magnet:?xt=urn:btih:" + mi.HashInfoBytes().HexString())
	if err != nil {
		t.Fatal(err)
	}
	tor, err := m.client.AddTorrent(mi)
	if err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.torrents[item.ID] = tor
	m.maxUsagePercent = 0
	m.mu.Unlock()
	m.resolveMetadata(item.ID, tor)
	state := m.State().Items[0]
	if state.Status != StatusError || state.SizeBytes != 1024 || state.TorrentFile == "" {
		t.Fatalf("unexpected resolved state: %+v", state)
	}
	if len(m.client.Torrents()) != 0 {
		t.Fatal("oversized torrent remained attached")
	}
	if _, _, err := loadTorrentMetadataFromFile(state.TorrentFile); err != nil {
		t.Fatal(err)
	}
	// Simulate restart and prove attachment can use cached metadata without peers.
	restored := newBareManager(t)
	restored.statePath = m.statePath
	if err := restored.loadState(); err != nil {
		t.Fatal(err)
	}
	if restored.items[item.ID].TorrentFile != state.TorrentFile {
		t.Fatal("cache path was not persisted")
	}
	m.mu.Lock()
	attached, err := m.attachTorrentLocked(m.items[item.ID])
	m.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if attached.Info() == nil {
		t.Fatal("cached magnet fetched metadata again")
	}
}

func TestCloseStopsBackgroundWorkersAndDiscovery(t *testing.T) {
	m := isolatedManager(t)
	if _, err := m.AddMagnet("magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"); err != nil {
		t.Fatal(err)
	}
	m.StartBackground()
	m.StartBackground()
	m.CheckAndStartNext()
	done := make(chan struct{})
	go func() { _ = m.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown left workers running")
	}
	m.CheckAndStartNext()
	m.StartBackground()
	if len(m.client.Torrents()) != 0 {
		t.Fatal("closed manager reattached torrent")
	}
}

func TestResourceSnapshotAndResidentParsing(t *testing.T) {
	for _, invalid := range []string{"", "1", "1 nope", "1 -1", "1 18446744073709551615"} {
		if parseResidentBytes(invalid, 4096) != nil {
			t.Fatalf("accepted %q", invalid)
		}
	}
	if got := parseResidentBytes("100 20 3", 4096); got == nil || *got != 81920 {
		t.Fatalf("bad RSS: %v", got)
	}
	m := newBareManager(t)
	d := m.Diagnostics()
	if d.Resources.HeapAllocBytes == 0 || d.Resources.Goroutines == 0 || d.Storage == nil {
		t.Fatalf("missing resources: %+v", d)
	}
	m.storagePath = m.storagePath + "/missing"
	if m.Diagnostics().StorageError == "" {
		t.Fatal("missing storage error")
	}
}

func TestResolvedMagnetWaitsDetachedForTemporaryStorageShortage(t *testing.T) {
	m := isolatedManager(t)
	mi, _, err := parseTorrentMetadata([]byte(minimalTorrentData))
	if err != nil {
		t.Fatal(err)
	}
	item, err := m.AddMagnet("magnet:?xt=urn:btih:" + mi.HashInfoBytes().HexString())
	if err != nil {
		t.Fatal(err)
	}
	tor, err := m.client.AddTorrent(mi)
	if err != nil {
		t.Fatal(err)
	}
	stats, err := storage.Stat(m.storagePath, 1)
	if err != nil {
		t.Fatal(err)
	}
	// Keep the quota well below current usage to tolerate unrelated filesystem
	// writes, while leaving the total quota much larger than this torrent.
	if stats.UsedBytes < 4096 {
		t.Skip("filesystem usage too small to model temporary shortage")
	}
	m.mu.Lock()
	m.maxUsagePercent = float64(stats.UsedBytes/2) / float64(stats.TotalBytes)
	m.torrents[item.ID] = tor
	m.mu.Unlock()
	m.resolveMetadata(item.ID, tor)
	state := m.State()
	if state.Items[0].Status != StatusQueued || state.MetadataID != "" || state.Items[0].TorrentFile == "" {
		t.Fatalf("expected cached, passive waiting magnet: %+v", state.Items[0])
	}
	if len(m.client.Torrents()) != 0 {
		t.Fatal("waiting magnet remained attached")
	}
	for range 3 {
		m.CheckAndStartNext()
	}
	if m.Diagnostics().Transitions.MetadataFetches != 0 {
		t.Fatal("waiting magnet repeated discovery")
	}
}

func TestClientMemoryBudgets(t *testing.T) {
	cfg := newClientConfig(t.TempDir())
	if cfg.MaxUnverifiedBytes != 32<<20 || cfg.MaxAllocPeerRequestDataPerConn != 256<<10 ||
		cfg.EstablishedConnsPerTorrent != 40 || cfg.HalfOpenConnsPerTorrent != 16 ||
		cfg.TotalHalfOpenConns != 32 || cfg.PieceHashersPerTorrent != 1 {
		t.Fatal("SBC resource budgets changed; reevaluate the memory profile")
	}
}

func BenchmarkResourceSnapshot(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = resourceSnapshot()
	}
}
