package balancer

import (
	"testing"
	"time"
)

// circuitSeed 熔断初态，字段与 circuitEntry 一致但不含锁——circuitEntry 内嵌 sync.Mutex，
// 按值传参会被 go vet 判为复制锁。
type circuitSeed struct {
	State               CircuitState
	ConsecutiveFailures int64
	LastFailureTime     time.Time
	TripCount           int
	HalfOpenSince       time.Time
}

// seedCircuitEntry 直接写入一个熔断条目，用于构造测试初态。
// 走 getOrCreateEntry 而不是直接操作 globalBreaker，保证测试与生产用同一套索引结构。
func seedCircuitEntry(channelID, keyID int, modelName string, seed circuitSeed) *circuitEntry {
	entry := getOrCreateEntry(channelID, keyID, modelName)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	entry.State = seed.State
	entry.ConsecutiveFailures = seed.ConsecutiveFailures
	entry.LastFailureTime = seed.LastFailureTime
	entry.TripCount = seed.TripCount
	entry.HalfOpenSince = seed.HalfOpenSince
	return entry
}

// circuitStateOf 只读取出状态，供断言使用。
func circuitStateOf(t *testing.T, channelID, keyID int, modelName string) (CircuitState, time.Time) {
	t.Helper()
	entry, ok := loadCircuitEntry(channelID, keyID, modelName)
	if !ok {
		t.Fatalf("circuit entry %s not found", circuitKey(channelID, keyID, modelName))
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	return entry.State, entry.HalfOpenSince
}

func TestResetCircuitBreakerByChannelRemovesOnlyTargetChannel(t *testing.T) {
	Reset()
	seedCircuitEntry(1, 10, "gpt-4o", circuitSeed{
		State:           StateOpen,
		LastFailureTime: time.Now(),
		TripCount:       1,
	})
	seedCircuitEntry(10, 10, "gpt-4o", circuitSeed{
		State:           StateOpen,
		LastFailureTime: time.Now(),
		TripCount:       1,
	})
	seedCircuitEntry(2, 20, "gpt-4o", circuitSeed{
		State:           StateOpen,
		LastFailureTime: time.Now(),
		TripCount:       1,
	})

	ResetStateByChannel(1)

	if tripped, _ := IsTripped(1, 10, "gpt-4o"); tripped {
		t.Fatal("expected target channel circuit breaker to be reset")
	}
	if tripped, _ := IsTripped(10, 10, "gpt-4o"); !tripped {
		t.Fatal("expected channel with similar prefix to remain tripped")
	}
	if tripped, _ := IsTripped(2, 20, "gpt-4o"); !tripped {
		t.Fatal("expected unrelated channel circuit breaker to remain tripped")
	}
}

func TestResetStickyByChannelRemovesOnlyTargetChannel(t *testing.T) {
	Reset()
	SetSticky(1, "gpt-4o", 10, 100)
	SetSticky(2, "gpt-4o", 20, 200)
	SetSticky(3, "claude", 10, 300)

	ResetStateByChannel(10)

	if entry := GetSticky(1, "gpt-4o", time.Minute); entry != nil {
		t.Fatalf("expected target channel sticky session to be reset, got %#v", entry)
	}
	if entry := GetSticky(3, "claude", time.Minute); entry != nil {
		t.Fatalf("expected second target channel sticky session to be reset, got %#v", entry)
	}
	if entry := GetSticky(2, "gpt-4o", time.Minute); entry == nil || entry.ChannelID != 20 {
		t.Fatalf("expected unrelated sticky session to remain, got %#v", entry)
	}
}

// rewindCircuitFailure 把上次失败时间往前拨，用于在不等待真实冷却（默认 60s）的前提下
// 推进状态机。只改时间戳，不改状态：Open -> HalfOpen 的迁移仍由 IsTripped 自己完成。
func rewindCircuitFailure(t *testing.T, channelID, keyID int, modelName string, d time.Duration) {
	t.Helper()
	entry, ok := loadCircuitEntry(channelID, keyID, modelName)
	if !ok {
		t.Fatalf("circuit entry %s not found", circuitKey(channelID, keyID, modelName))
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	entry.LastFailureTime = entry.LastFailureTime.Add(-d)
}

// TestCircuitFullRecoveryCycle 完整恢复链路：
// Closed -> 连续失败达阈值 -> Open -> 冷却到期 -> HalfOpen -> 单个试探成功 -> Closed。
// 全程只用公开 API 驱动状态机，不预置状态，确保恢复路径真的能走通
// （只验证 Open 会拒绝请求不算验证恢复：熔断后永不恢复也能通过那种断言）。
func TestCircuitFullRecoveryCycle(t *testing.T) {
	Reset()
	const (
		ch  = 51
		key = 5
		mdl = "gpt-4o"
	)

	// 默认阈值 5：前 4 次失败必须仍然放行，否则阈值语义被破坏
	for i := 0; i < 4; i++ {
		RecordFailure(ch, key, mdl, FailureHard)
		if tripped, _ := IsTripped(ch, key, mdl); tripped {
			t.Fatalf("第 %d 次失败即熔断，默认阈值应为 5", i+1)
		}
	}
	RecordFailure(ch, key, mdl, FailureHard)

	tripped, remaining := IsTripped(ch, key, mdl)
	if !tripped {
		t.Fatal("连续失败达阈值后应进入 Open")
	}
	if remaining <= 0 || remaining > time.Minute {
		t.Fatalf("Open 剩余冷却 = %v, want (0, 60s]", remaining)
	}
	if state, _ := circuitStateOf(t, ch, key, mdl); state != StateOpen {
		t.Fatalf("达阈值后 state = %v, want Open", state)
	}

	// 冷却到期：恰好放行一个试探请求
	rewindCircuitFailure(t, ch, key, mdl, 61*time.Second)
	if tripped, _ := IsTripped(ch, key, mdl); tripped {
		t.Fatal("冷却到期后应放行试探请求")
	}
	state, halfOpenSince := circuitStateOf(t, ch, key, mdl)
	if state != StateHalfOpen {
		t.Fatalf("试探期 state = %v, want HalfOpen", state)
	}
	if halfOpenSince.IsZero() {
		t.Fatal("HalfOpenSince 未记录，试探超时将无法判定")
	}
	// 试探期间并发请求必须被拒绝：一次只放一个探针
	if tripped, _ := IsTripped(ch, key, mdl); !tripped {
		t.Fatal("试探进行中应拒绝其他请求")
	}

	// 试探成功 -> Closed，退避计数归零
	RecordSuccess(ch, key, mdl)
	if tripped, _ := IsTripped(ch, key, mdl); tripped {
		t.Fatal("试探成功后应恢复放行")
	}
	entry, ok := loadCircuitEntry(ch, key, mdl)
	if !ok {
		t.Fatal("恢复后熔断条目不应被删除")
	}
	entry.mu.Lock()
	st, consecutive, trips := entry.State, entry.ConsecutiveFailures, entry.TripCount
	entry.mu.Unlock()
	if st != StateClosed || consecutive != 0 || trips != 0 {
		t.Fatalf("恢复后 state=%v consecutiveFailures=%d tripCount=%d, want Closed/0/0", st, consecutive, trips)
	}
}

// TestCircuitProbeFailureReopensWithBackoff 试探失败：回到 Open，且退避比上一轮更长。
// 与恢复成功路径成对，证明 HalfOpen 的两个出口都通。
func TestCircuitProbeFailureReopensWithBackoff(t *testing.T) {
	Reset()
	const (
		ch  = 52
		key = 6
		mdl = "gpt-4o"
	)
	for i := 0; i < 5; i++ {
		RecordFailure(ch, key, mdl, FailureHard)
	}
	_, firstCooldown := IsTripped(ch, key, mdl)

	rewindCircuitFailure(t, ch, key, mdl, 61*time.Second)
	if tripped, _ := IsTripped(ch, key, mdl); tripped {
		t.Fatal("冷却到期后应放行试探请求")
	}

	// 试探失败：TripCount++ 使下一轮冷却指数退避
	RecordFailure(ch, key, mdl, FailureHard)
	tripped, secondCooldown := IsTripped(ch, key, mdl)
	if !tripped {
		t.Fatal("试探失败后应重新熔断")
	}
	if state, _ := circuitStateOf(t, ch, key, mdl); state != StateOpen {
		t.Fatalf("试探失败后 state = %v, want Open", state)
	}
	if secondCooldown <= firstCooldown {
		t.Fatalf("退避未增长：first=%v second=%v", firstCooldown, secondCooldown)
	}
}

// TestHalfOpenSoftRateLimitReopensWithoutBackoffAmplification preserves the
// temporary compatibility contract used by Compact/WebSocket until P4C1b:
// a rate-limited half-open probe reopens the breaker, but must not count as a
// new hard outage or exponentially lengthen the next probe delay.
func TestHalfOpenSoftRateLimitReopensWithoutBackoffAmplification(t *testing.T) {
	Reset()
	const (
		ch  = 53
		key = 7
		mdl = "gpt-4o"
	)

	for i := 0; i < 5; i++ {
		RecordFailure(ch, key, mdl, FailureHard)
	}
	entry, ok := loadCircuitEntry(ch, key, mdl)
	if !ok {
		t.Fatal("expected hard failures to create a circuit entry")
	}
	entry.mu.Lock()
	initialTrips := entry.TripCount
	entry.mu.Unlock()
	initialCooldown := GetCooldown(initialTrips)

	rewindCircuitFailure(t, ch, key, mdl, initialCooldown+time.Second)
	if tripped, _ := IsTripped(ch, key, mdl); tripped {
		t.Fatal("cooldown expiry should admit exactly one half-open probe")
	}
	if state, _ := circuitStateOf(t, ch, key, mdl); state != StateHalfOpen {
		t.Fatalf("probe state = %v, want HalfOpen", state)
	}

	RecordFailure(ch, key, mdl, FailureSoftRateLimit)

	entry, ok = loadCircuitEntry(ch, key, mdl)
	if !ok {
		t.Fatal("expected soft rate limit to retain the circuit entry")
	}
	entry.mu.Lock()
	state, trips := entry.State, entry.TripCount
	entry.mu.Unlock()
	if state != StateOpen {
		t.Fatalf("soft-rate-limited probe state = %v, want Open", state)
	}
	if trips != initialTrips {
		t.Fatalf("soft rate limit amplified trip count: before=%d after=%d", initialTrips, trips)
	}
	if cooldown := GetCooldown(trips); cooldown != initialCooldown {
		t.Fatalf("soft rate limit amplified cooldown: before=%v after=%v", initialCooldown, cooldown)
	}
}

func TestHalfOpenDoesNotRemainTrippedForeverWithoutResult(t *testing.T) {
	Reset()
	seedCircuitEntry(7, 8, "gpt-4o", circuitSeed{
		State:         StateHalfOpen,
		TripCount:     1,
		HalfOpenSince: time.Now().Add(-61 * time.Second),
	})

	tripped, remaining := IsTripped(7, 8, "gpt-4o")
	if !tripped {
		t.Fatal("expected expired half-open probe to be tripped again")
	}
	if remaining <= 0 {
		t.Fatalf("expected expired half-open probe to return cooldown, got %v", remaining)
	}

	if _, ok := loadCircuitEntry(7, 8, "gpt-4o"); !ok {
		t.Fatal("expected circuit entry to remain after half-open timeout")
	}
	state, halfOpenSince := circuitStateOf(t, 7, 8, "gpt-4o")
	if state != StateOpen {
		t.Fatalf("expected expired half-open entry to return to open, got %v", state)
	}
	if !halfOpenSince.IsZero() {
		t.Fatalf("expected half-open timestamp to be cleared, got %v", halfOpenSince)
	}
}
