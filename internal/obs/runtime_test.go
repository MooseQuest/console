package obs

import (
	"runtime"
	"strings"
	"testing"
)

func TestRuntimeCollector(t *testing.T) {
	m := NewMemory()
	m.AddCollector(RuntimeCollector("v1.2.3"))
	runtime.GC()

	var sb strings.Builder
	if err := m.WritePrometheus(&sb); err != nil {
		t.Fatal(err)
	}
	out := sb.String()
	for _, want := range []string{
		"go_goroutines ",
		"go_memstats_heap_alloc_bytes ",
		"go_gc_pause_seconds ",
		`console_build_info{version="v1.2.3"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("scrape missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "go_goroutines 0\n") {
		t.Error("go_goroutines should be positive")
	}
}
