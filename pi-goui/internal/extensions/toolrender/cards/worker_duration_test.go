package cards

import "testing"

func TestWorkerDurationWholeSeconds(t *testing.T) {
	for _, tc := range []struct {
		elapsed float64
		want    string
	}{
		{0, "0s"}, {400, "0s"}, {900, "1s"}, {10400, "10s"}, {11900, "12s"}, {59900, "1m0s"}, {62300, "1m2s"},
	} {
		if got := WorkerDuration(WorkerCard{Started: 1000, Finished: 1000 + tc.elapsed}, 0); got != tc.want {
			t.Fatalf("duration %v = %q, want %q", tc.elapsed, got, tc.want)
		}
	}
	if WorkerDuration(WorkerCard{}, 12000) != "" {
		t.Fatal("missing timestamps fabricated duration")
	}
}
