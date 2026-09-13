package balancer

import (
	"fmt"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/utils/log"
)

// CircuitState 熔断器状态
type CircuitState int

type FailureKind int

const (
	StateClosed   CircuitState = iota // 正常通行
	StateOpen                         // 熔断中，拒绝所有请求
	StateHalfOpen                     // 半开，仅允许单个试探请求
)

// FailureKind 区分硬失败、软失败（限流）与不应进入熔断器的语义失败。
// 独立 const 块，避免与 CircuitState 共用 iota 计数器导致 FailureHard 从 3 开始。
const (
	FailureHard          FailureKind = iota // 上游硬失败，计入连续失败并可触发熔断
	FailureSoftRateLimit                    // 上游限流，按 Retry-After 冷却，不累计硬失败
	FailureIgnore                           // 请求/凭据/能力语义错误，不作为上游健康证据
)

// circuitEntry 单个熔断器条目
type circuitEntry struct {
	State               CircuitState
	ConsecutiveFailures int64
	LastFailureTime     time.Time
	TripCount           int // 累计熔断触发次数（用于指数退避）
	HalfOpenSince       time.Time
	mu                  sync.Mutex
}

// circuitScope 熔断存储的一级键：渠道-模型。
// 二级键是 channelKeyID，业务粒度仍是 channelID+keyID+model（与原字符串键一致）。
// 分两级只为让排序阶段的「该渠道-模型是否有任一 Key 熔断」变成一次直接定位，
// 不再需要遍历全表 + 字符串前后缀匹配。
type circuitScope struct {
	ChannelID int
	Model     string
}

// circuitGroup 同一渠道-模型下的各 Key 熔断条目。
type circuitGroup struct {
	mu      sync.RWMutex
	entries map[int]*circuitEntry // channelKeyID -> entry
}

// 全局熔断器存储
var globalBreaker sync.Map // key: circuitScope -> value: *circuitGroup

// circuitKey 熔断器日志标签：channelID:channelKeyID:modelName
func circuitKey(channelID, keyID int, modelName string) string {
	return fmt.Sprintf("%d:%d:%s", channelID, keyID, modelName)
}

func resetCircuitBreakerByChannel(channelID int) {
	globalBreaker.Range(func(key, _ any) bool {
		if scope, ok := key.(circuitScope); ok && scope.ChannelID == channelID {
			globalBreaker.Delete(scope)
		}
		return true
	})
}

// loadCircuitEntry 只读定位一个熔断条目，不创建。
func loadCircuitEntry(channelID, keyID int, modelName string) (*circuitEntry, bool) {
	v, ok := globalBreaker.Load(circuitScope{ChannelID: channelID, Model: modelName})
	if !ok {
		return nil, false
	}
	group := v.(*circuitGroup)
	group.mu.RLock()
	entry, ok := group.entries[keyID]
	group.mu.RUnlock()
	return entry, ok
}

// getOrCreateEntry 获取或创建熔断器条目
func getOrCreateEntry(channelID, keyID int, modelName string) *circuitEntry {
	scope := circuitScope{ChannelID: channelID, Model: modelName}
	v, ok := globalBreaker.Load(scope)
	if !ok {
		v, _ = globalBreaker.LoadOrStore(scope, &circuitGroup{entries: make(map[int]*circuitEntry)})
	}
	group := v.(*circuitGroup)

	group.mu.RLock()
	entry, ok := group.entries[keyID]
	group.mu.RUnlock()
	if ok {
		return entry
	}

	group.mu.Lock()
	defer group.mu.Unlock()
	if entry, ok := group.entries[keyID]; ok {
		return entry
	}
	entry = &circuitEntry{State: StateClosed}
	group.entries[keyID] = entry
	return entry
}

// getThreshold 获取熔断阈值配置
func getThreshold() int64 {
	v, err := op.SettingGetInt(model.SettingKeyCircuitBreakerThreshold)
	if err != nil || v <= 0 {
		return 5
	}
	return int64(v)
}

// GetCooldown 获取当前冷却时间（带指数退避）
func GetCooldown(tripCount int) time.Duration {
	base, err := op.SettingGetInt(model.SettingKeyCircuitBreakerCooldown)
	if err != nil || base <= 0 {
		base = 60
	}
	maxCooldown, err := op.SettingGetInt(model.SettingKeyCircuitBreakerMaxCooldown)
	if err != nil || maxCooldown <= 0 {
		maxCooldown = 600
	}

	// 指数退避：baseCooldown * 2^(tripCount-1)
	cooldown := base
	if tripCount > 1 {
		shift := tripCount - 1
		if shift > 20 { // 防止溢出
			shift = 20
		}
		cooldown = base << shift
	}
	if cooldown > maxCooldown {
		cooldown = maxCooldown
	}

	return time.Duration(cooldown) * time.Second
}

// IsTripped 检查通道是否处于熔断状态
// 返回 tripped=true 表示该通道应被跳过，remaining 为剩余冷却时间
func IsTripped(channelID, keyID int, modelName string) (tripped bool, remaining time.Duration) {
	entry, ok := loadCircuitEntry(channelID, keyID, modelName)
	if !ok {
		return false, 0 // 无记录，视为 Closed
	}
	key := circuitKey(channelID, keyID, modelName)

	entry.mu.Lock()
	defer entry.mu.Unlock()

	switch entry.State {
	case StateClosed:
		return false, 0

	case StateOpen:
		cooldown := GetCooldown(entry.TripCount)
		elapsed := time.Since(entry.LastFailureTime)
		if elapsed >= cooldown {
			now := time.Now()
			entry.State = StateHalfOpen
			entry.HalfOpenSince = now
			log.Infof("circuit breaker [%s] Open -> HalfOpen (cooldown %v elapsed)", key, cooldown)
			return false, 0
		}
		// 仍在冷却中
		return true, cooldown - elapsed

	case StateHalfOpen:
		cooldown := GetCooldown(entry.TripCount)
		if entry.HalfOpenSince.IsZero() {
			entry.HalfOpenSince = time.Now()
		}
		if time.Since(entry.HalfOpenSince) >= cooldown {
			entry.State = StateOpen
			entry.LastFailureTime = time.Now()
			entry.HalfOpenSince = time.Time{}
			log.Warnf("circuit breaker [%s] HalfOpen -> Open (probe timed out, cooldown=%v)", key, cooldown)
			return true, cooldown
		}
		// 已有试探请求在进行中，拒绝其他请求
		return true, 0

	default:
		return false, 0
	}
}

// PeekItemTripped 只读探测某个渠道-模型是否有任一 Key 处于熔断中。
// 与 IsTripped 的区别：绝不做状态迁移（IsTripped 会把冷却到期的 Open 转 HalfOpen、
// 把探测超时的 HalfOpen 转回 Open，并对 HalfOpen 返回 true 以拒绝并发探测），
// 因此只适合排序阶段读取熔断信号，不能替代请求路径上的准入判断。
// keyID 未知：该渠道-模型下任一 Key 处于熔断即返回 true（跨 Key 折叠是排序期的既有语义，
// 只要有 Key 在熔断就说明这个渠道-模型当前有问题，排序上应当压后）。
func PeekItemTripped(channelID int, modelName string) bool {
	v, ok := globalBreaker.Load(circuitScope{ChannelID: channelID, Model: modelName})
	if !ok {
		return false
	}
	group := v.(*circuitGroup)

	// 持 group 读锁期间再取 entry.mu：全局没有「先 entry.mu 再 group.mu」的路径
	// （getOrCreateEntry 只持 group.mu，Record*/IsTripped 取 entry.mu 前已释放 group.mu），
	// 因此不存在锁序反转，可以免掉每次调用的切片拷贝。
	group.mu.RLock()
	defer group.mu.RUnlock()

	for _, entry := range group.entries {
		entry.mu.Lock()
		state := entry.State
		lastFailure := entry.LastFailureTime
		tripCount := entry.TripCount
		entry.mu.Unlock()

		if state == StateOpen && time.Since(lastFailure) < GetCooldown(tripCount) {
			return true
		}
	}
	return false
}

// RecordSuccess 记录成功，重置熔断器状态
func RecordSuccess(channelID, keyID int, modelName string) {
	entry, ok := loadCircuitEntry(channelID, keyID, modelName)
	if !ok {
		return
	}
	key := circuitKey(channelID, keyID, modelName)

	entry.mu.Lock()
	defer entry.mu.Unlock()

	if entry.State == StateHalfOpen {
		log.Infof("circuit breaker [%s] HalfOpen -> Closed (probe succeeded)", key)
	}

	// 重置全部状态
	entry.State = StateClosed
	entry.ConsecutiveFailures = 0
	entry.TripCount = 0
	entry.HalfOpenSince = time.Time{}
}

// RecordFailure 记录失败，可能触发熔断。
// FailureSoftRateLimit 用于 429/503 这类软失败：Closed 状态下不累计阈值，
// HalfOpen 状态下重新进入 Open，但不放大 TripCount。FailureIgnore 完全不写入熔断状态。
func RecordFailure(channelID, keyID int, modelName string, kind FailureKind) {
	if kind == FailureIgnore {
		return
	}

	key := circuitKey(channelID, keyID, modelName)
	entry := getOrCreateEntry(channelID, keyID, modelName)

	entry.mu.Lock()
	defer entry.mu.Unlock()

	entry.LastFailureTime = time.Now()
	entry.HalfOpenSince = time.Time{}

	switch entry.State {
	case StateClosed:
		if kind == FailureSoftRateLimit {
			return
		}
		entry.ConsecutiveFailures++
		threshold := getThreshold()
		if entry.ConsecutiveFailures >= threshold {
			entry.State = StateOpen
			entry.TripCount++
			log.Warnf("circuit breaker [%s] Closed -> Open (failures=%d >= threshold=%d, tripCount=%d, cooldown=%v)",
				key, entry.ConsecutiveFailures, threshold, entry.TripCount, GetCooldown(entry.TripCount))
		}

	case StateHalfOpen:
		if kind == FailureSoftRateLimit {
			entry.State = StateOpen
			log.Warnf("circuit breaker [%s] HalfOpen -> Open (soft rate limit, tripCount=%d, cooldown=%v)",
				key, entry.TripCount, GetCooldown(entry.TripCount))
			return
		}
		// 试探失败，重新进入 Open 状态，TripCount 递增（冷却时间翻倍）
		entry.State = StateOpen
		entry.TripCount++
		entry.ConsecutiveFailures = 0 // 重新开始计数
		log.Warnf("circuit breaker [%s] HalfOpen -> Open (probe failed, tripCount=%d, cooldown=%v)",
			key, entry.TripCount, GetCooldown(entry.TripCount))

	case StateOpen:
		// 理论上不应该在 Open 状态下接收到失败记录（请求应被拒绝），
		// 但为安全起见仍更新失败时间
	}
}
