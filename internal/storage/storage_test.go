package storage
import "testing"
func TestCanFit(t *testing.T) {
    stats := &Stats{MaxUsageBytes: 900, AvailableForNew: 600}
    canFit, msg := CanFit(stats, 500)
    if !canFit || msg != "" {
        t.Errorf("expected fit=true, got fit=%v, msg=%q", canFit, msg)
    }
    canFit, msg = CanFit(stats, 700)
    if canFit || msg != "not enough storage available" {
        t.Errorf("expected fit=false, got fit=%v, msg=%q", canFit, msg)
    }
}
