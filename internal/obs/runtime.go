package obs

import (
	"math"
	"runtime/metrics"
)

// runtime/metrics sample names used by [RuntimeCollector].
const (
	sampleGoroutines = "/sched/goroutines:goroutines"
	sampleHeapAlloc  = "/memory/classes/heap/objects:bytes"
	sampleGCPauses   = "/sched/pauses/total/gc:seconds"
)

// RuntimeCollector returns a collector, suitable for [Memory.AddCollector],
// that samples Go runtime health via runtime/metrics (no stop-the-world) and
// publishes it as gauges:
//
//   - go_goroutines: live goroutines.
//   - go_memstats_heap_alloc_bytes: bytes of allocated heap objects.
//   - go_gc_pause_seconds: CUMULATIVE time spent in GC stop-the-world pauses
//     since process start. runtime/metrics exposes pauses only as a histogram,
//     so this is an estimate: each bucket's count is weighted by the bucket
//     midpoint (the finite bound for open-ended buckets). It is monotonically
//     non-decreasing; use rate() or a delta to get recent pause time.
//
// It also sets console_build_info{version} to 1. Samples of a kind the running
// Go version does not provide are skipped.
func RuntimeCollector(version string) func(Recorder) {
	return func(r Recorder) {
		samples := []metrics.Sample{
			{Name: sampleGoroutines},
			{Name: sampleHeapAlloc},
			{Name: sampleGCPauses},
		}
		metrics.Read(samples)

		if v := samples[0].Value; v.Kind() == metrics.KindUint64 {
			r.Gauge("go_goroutines").Set(float64(v.Uint64()))
		}
		if v := samples[1].Value; v.Kind() == metrics.KindUint64 {
			r.Gauge("go_memstats_heap_alloc_bytes").Set(float64(v.Uint64()))
		}
		if v := samples[2].Value; v.Kind() == metrics.KindFloat64Histogram {
			r.Gauge("go_gc_pause_seconds").Set(histogramTotal(v.Float64Histogram()))
		}
		r.Gauge("console_build_info", L("version", version)).Set(1)
	}
}

// histogramTotal estimates the sum of observations in h by weighting each
// bucket's count by its midpoint (or its finite bound when open-ended).
func histogramTotal(h *metrics.Float64Histogram) float64 {
	var total float64
	for i, c := range h.Counts {
		if c == 0 {
			continue
		}
		lo, hi := h.Buckets[i], h.Buckets[i+1]
		switch {
		case math.IsInf(lo, -1):
			total += float64(c) * hi
		case math.IsInf(hi, 1):
			total += float64(c) * lo
		default:
			total += float64(c) * (lo + hi) / 2
		}
	}
	return total
}
