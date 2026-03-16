package torrent

import "testing"

func TestStatusConstants(t *testing.T) {
	if StatusQueued != "queued" {
		t.Error("wrong queued status")
	}
	if StatusDownloading != "downloading" {
		t.Error("wrong downloading status")
	}
	if StatusCompleted != "completed" {
		t.Error("wrong completed status")
	}
	if StatusError != "error" {
		t.Error("wrong error status")
	}
}
