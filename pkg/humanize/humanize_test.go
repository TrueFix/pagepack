package humanize

import (
	"testing"
	"time"
)

func TestBytes(t *testing.T) {
	for in, want := range map[int64]string{0: "0 B", 512: "512 B", 1536: "1.5 KB", 104857600: "100 MB", 13002342: "12.4 MB", 5 << 30: "5 GB", -2048: "-2 KB"} {
		if got := Bytes(in); got != want {
			t.Errorf("Bytes(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestRatesAndPercent(t *testing.T) {
	if got := Rate(PerSecond(3<<20, 2*time.Second)); got != "1.5 MB/s" {
		t.Errorf("rate = %q", got)
	}
	if PerSecond(10, 0) != 0 || Percent(1, 0) != 0 || Percent(150, 100) != 100 || Percent(1, 3) != 33.3 {
		t.Error("unexpected percent/rate edge case")
	}
}
