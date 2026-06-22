package snapshot

// SumContext returns the total context tokens for a turn or totals bucket:
// the new (non-cached) input plus cache-read plus cache-creation. This is what
// users intuitively mean by "输入量" — input_tokens alone excludes cache, which
// is ~95% of the real context for a long session.
func SumContext(in, cacheRead, cacheCreate int64) int64 {
	return in + cacheRead + cacheCreate
}

// Downsample returns at most max evenly-spaced points (first and last always
// kept) so chart/sparkline rendering stays cheap on busy windows. A slice
// already <= max (or max < 2) is returned unchanged.
func Downsample(points []SeriesPoint, max int) []SeriesPoint {
	if max < 2 || len(points) <= max {
		return points
	}
	step := float64(len(points)-1) / float64(max-1)
	out := make([]SeriesPoint, 0, max)
	for i := 0; i < max; i++ {
		idx := int(float64(i)*step + 0.5)
		if idx >= len(points) {
			idx = len(points) - 1
		}
		out = append(out, points[idx])
	}
	return out
}
