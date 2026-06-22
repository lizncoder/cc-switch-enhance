package snapshot

import "testing"

func TestSumContext(t *testing.T) {
	cases := []struct{ in, cr, cc, want int64 }{
		{334, 81664, 0, 81998},
		{1304011, 24096000, 0, 25400011},
		{0, 0, 0, 0},
	}
	for _, c := range cases {
		if got := SumContext(c.in, c.cr, c.cc); got != c.want {
			t.Errorf("SumContext(%d,%d,%d)=%d want %d", c.in, c.cr, c.cc, got, c.want)
		}
	}
}

func TestDownsample(t *testing.T) {
	in := make([]SeriesPoint, 100)
	for i := range in {
		in[i] = SeriesPoint{T: int64(i)}
	}
	out := Downsample(in, 10)
	if len(out) != 10 {
		t.Fatalf("len=%d want 10", len(out))
	}
	if out[0].T != 0 {
		t.Errorf("first point not preserved: %d", out[0].T)
	}
	if out[len(out)-1].T != 99 {
		t.Errorf("last point not preserved: %d", out[len(out)-1].T)
	}
	// Already-small slice is returned unchanged.
	short := []SeriesPoint{{T: 1}, {T: 2}, {T: 3}}
	if got := Downsample(short, 10); len(got) != 3 {
		t.Errorf("short slice changed: len=%d", len(got))
	}
}
