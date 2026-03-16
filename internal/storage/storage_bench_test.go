package storage

import (
	"testing"
)

func BenchmarkCanFit(b *testing.B) {
	stats := &Stats{MaxUsageBytes: 900, AvailableForNew: 600}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = CanFit(stats, 500)
	}
}

func BenchmarkCanFitLarge(b *testing.B) {
	stats := &Stats{MaxUsageBytes: 900, AvailableForNew: 600}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = CanFit(stats, 1000)
	}
}
