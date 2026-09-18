package relay

import (
    "errors"
    "os"
    "path/filepath"
    "runtime"
    "strings"
    "testing"
)

func TestP4C3LegacyCircuitResidueIsPhysicallyRemoved(t *testing.T) {
    _, thisFile, _, ok := runtime.Caller(0)
    if !ok {
        t.Fatal("resolve P4C3 source path")
    }
    relayDir := filepath.Dir(thisFile)

    circuitPath := filepath.Join(relayDir, "balancer", "circuit.go")
    if _, err := os.Stat(circuitPath); err == nil {
        t.Errorf("legacy circuit implementation still exists: %s", circuitPath)
    } else if !errors.Is(err, os.ErrNotExist) {
        t.Fatalf("stat legacy circuit implementation: %v", err)
    }

    forbidden := map[string][]string{
        filepath.Join(relayDir, "balancer", "state.go"): {
            "resetCircuitBreakerByChannel",
        },
        filepath.Join(relayDir, "balancer", "balancer.go"): {
            "globalBreaker",
            "legacy circuit",
        },
        filepath.Join(relayDir, "routing_decision.go"): {
            "CircuitEffect",
        },
        filepath.Join(relayDir, "..", "model", "attempt_routing_trace.go"): {
            "CircuitEffect",
            "circuit_effect",
        },
        filepath.Join(relayDir, "..", "routinginspect", "explanation.go"): {
            "CircuitEffect",
            "circuit_effect",
        },
        filepath.Join(relayDir, "..", "..", "web", "src", "api", "endpoints", "routing-inspector.ts"): {
            "circuit_effect",
        },
        filepath.Join(relayDir, "..", "model", "setting.go"): {
            "SettingKeyCircuitBreaker",
        },
        filepath.Join(relayDir, "..", "model", "routing_decision_event.go"): {
            "DecisionReasonCircuitBreak",
        },
        filepath.Join(relayDir, "..", "model", "log.go"): {
            "AttemptCircuitBreak",
        },
    }

    for path, tokens := range forbidden {
        data, err := os.ReadFile(path)
        if err != nil {
            t.Fatalf("read %s: %v", path, err)
        }
        source := string(data)
        for _, token := range tokens {
            if strings.Contains(source, token) {
                t.Errorf("%s still contains retired circuit residue %q", filepath.Base(path), token)
            }
        }
    }
}
