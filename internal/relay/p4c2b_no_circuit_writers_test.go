package relay

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// P4C2B makes the unified availability runtime the only live health writer.
// The legacy circuit breaker remains compiled for historical tests/settings/reset
// compatibility until P4C3, but production relay paths must not mutate it.
func TestP4C2BLiveRelayHasNoLegacyCircuitWriters(t *testing.T) {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	dir := filepath.Dir(thisFile)

	forbidden := map[string][]string{
		"relay_attempt.go": {
			"balancer.RecordSuccess(",
			"func circuitFailureKind(",
			"func circuitFailureKindForDecision(",
		},
		"relay_handler.go": {
			"balancer.RecordFailure(",
			"circuitFailureKindForDecision(",
		},
		"images.go": {
			"balancer.RecordSuccess(",
			"balancer.RecordFailure(",
			"circuitFailureKindForDecision(",
		},
		"compact.go": {
			"balancer.RecordSuccess(",
			"balancer.RecordFailure(",
			"circuitFailureKindForDecision(",
		},
		"ws_client.go": {
			"balancer.RecordFailure(",
			"circuitFailureKindForDecision(",
			"balancer.FailureHard",
		},
	}

	for name, tokens := range forbidden {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		source := string(data)
		for _, token := range tokens {
			if strings.Contains(source, token) {
				t.Errorf("%s still contains legacy circuit writer dependency %q", name, token)
			}
		}
	}
}
