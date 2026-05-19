package torrent

import "time"

type Status string

const (
	StatusQueued      Status = "queued"
	StatusDownloading Status = "downloading"
	StatusPaused      Status = "paused"
	StatusCompleted   Status = "completed"
	StatusError       Status = "error"
)

type Item struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	SizeBytes    int64     `json:"size_bytes"`
	Status       Status    `json:"status"`
	Progress     float64   `json:"progress"`
	Downloaded   int64     `json:"downloaded"`
	AddedAt      time.Time `json:"added_at"`
	Magnet       string    `json:"magnet"`
	TorrentFile  string    `json:"torrent_file"`
	ErrorMessage string    `json:"error_message"`
}

type State struct {
	Items []*Item  `json:"items"`
	Order []string `json:"order"`
}

type StateView struct {
	Items    []*Item  `json:"items"`
	Order    []string `json:"order"`
	ActiveID string   `json:"active_id"`
}

type QueueCounts struct {
	Queued      int `json:"queued"`
	Downloading int `json:"downloading"`
	Paused      int `json:"paused"`
	Completed   int `json:"completed"`
	Error       int `json:"error"`
}
