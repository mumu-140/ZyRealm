package balancer

import (
	"fmt"
	"time"

	"github.com/bestruirui/octopus/internal/model"
)

// Iterator 统一的负载均衡迭代器
// 内部编排：运行态准入 + 策略排序 + 粘性优先 + 决策追踪
type Iterator struct {
	candidates       []model.GroupItem
	index            int
	stickyIdx        int // 粘性通道在 candidates 中的位置，-1 表示无
	stickyKeyID      int
	modelName        string // 请求模型名（用于熔断检查）
	skippedProviders map[int]struct{}

	// 内嵌追踪
	attempts []model.ChannelAttempt
	count    int
}

// NewIterator 创建负载均衡迭代器
// 自动处理：运行态准入 + 策略排序 + 粘性通道提前
func NewIterator(group model.Group, apiKeyID int, requestModel string) *Iterator {
	return NewIteratorWithPreference(group, apiKeyID, requestModel, nil)
}

// NewIteratorWithPreference 创建带优先通道偏好的负载均衡迭代器。
// runtime eligibility 在所有 GroupMode 之前统一应用；sticky 只能在当前
// AVAILABLE 候选中生效，不能把 SUSPECT/HALF_OPEN/COOLDOWN 通道提到首位。
func NewIteratorWithPreference(group model.Group, apiKeyID int, requestModel string, preferred *SessionEntry) *Iterator {
	now := time.Now()
	candidates := runtimeOrderedCandidates(group, requestModel, now)

	stickyIdx := -1
	stickyKeyID := 0
	if preferred != nil && preferred.ChannelID > 0 {
		for i, item := range candidates {
			if item.ChannelID == preferred.ChannelID && runtimeStickyEligible(item, requestModel, now) {
				if i > 0 {
					preferredItem := candidates[i]
					copy(candidates[1:i+1], candidates[0:i])
					candidates[0] = preferredItem
				}
				stickyIdx = 0
				stickyKeyID = preferred.ChannelKeyID
				break
			}
		}
	}
	if stickyIdx < 0 && group.SessionKeepTime > 0 {
		stickyTTL := time.Duration(group.SessionKeepTime) * time.Second
		if sticky := GetSticky(apiKeyID, requestModel, stickyTTL); sticky != nil {
			for i, item := range candidates {
				if item.ChannelID == sticky.ChannelID && runtimeStickyEligible(item, requestModel, now) {
					if i > 0 {
						// 将 AVAILABLE 粘性通道移到最前面。
						stickyItem := candidates[i]
						copy(candidates[1:i+1], candidates[0:i])
						candidates[0] = stickyItem
					}
					stickyIdx = 0
					stickyKeyID = sticky.ChannelKeyID
					break
				}
			}
		}
	}

	return &Iterator{
		candidates:       candidates,
		index:            -1,
		stickyIdx:        stickyIdx,
		stickyKeyID:      stickyKeyID,
		modelName:        requestModel,
		skippedProviders: make(map[int]struct{}),
	}
}

// Next 移动到下一个未被当前请求跳过的候选，返回 false 表示遍历完成。
func (it *Iterator) Next() bool {
	for {
		it.index++
		if it.index >= len(it.candidates) {
			return false
		}
		if _, skipped := it.skippedProviders[it.candidates[it.index].ChannelID]; skipped {
			continue
		}
		return true
	}
}

// SkipProvider marks a provider/channel unavailable for the remainder of the
// current request. This is request-local state only; it does not mutate shared
// health, circuit, or persistent channel configuration.
func (it *Iterator) SkipProvider(channelID int) {
	if it == nil || channelID <= 0 {
		return
	}
	if it.skippedProviders == nil {
		it.skippedProviders = make(map[int]struct{})
	}
	it.skippedProviders[channelID] = struct{}{}
}

// Item 返回当前候选的 GroupItem
func (it *Iterator) Item() model.GroupItem {
	return it.candidates[it.index]
}

// IsSticky 当前候选是否为粘性通道
func (it *Iterator) IsSticky() bool {
	return it.stickyIdx >= 0 && it.index == it.stickyIdx
}

func (it *Iterator) StickyKeyID() int {
	if !it.IsSticky() {
		return 0
	}
	return it.stickyKeyID
}

// Len 返回当前通过运行态准入的候选列表长度。
func (it *Iterator) Len() int {
	return len(it.candidates)
}

// Index 返回当前迭代位置（0-based）
func (it *Iterator) Index() int {
	return it.index
}

// Skip 记录当前通道被跳过（通道禁用、无Key、类型不兼容等）
func (it *Iterator) Skip(channelID, channelKeyID int, channelName, msg string) {
	it.count++
	it.attempts = append(it.attempts, model.ChannelAttempt{
		ChannelID:    channelID,
		ChannelKeyID: channelKeyID,
		ChannelName:  channelName,
		ModelName:    it.candidates[it.index].ModelName,
		AttemptNum:   it.count,
		Status:       model.AttemptSkipped,
		Sticky:       it.IsSticky(),
		Msg:          msg,
	})
}

func (it *Iterator) SkipCapacity(channelID, channelKeyID int, channelName, msg string) {
	it.count++
	it.attempts = append(it.attempts, model.ChannelAttempt{
		ChannelID: channelID, ChannelKeyID: channelKeyID, ChannelName: channelName,
		ModelName: it.candidates[it.index].ModelName, AttemptNum: it.count,
		Status: model.AttemptCapacity, Sticky: it.IsSticky(), Msg: msg,
	})
}

func (it *Iterator) SkipRateLimit(channelID, channelKeyID int, channelName, msg string) {
	it.count++
	it.attempts = append(it.attempts, model.ChannelAttempt{
		ChannelID: channelID, ChannelKeyID: channelKeyID, ChannelName: channelName,
		ModelName: it.candidates[it.index].ModelName, AttemptNum: it.count,
		Status: model.AttemptRateLimit, Sticky: it.IsSticky(), Msg: msg,
	})
}

// SkipCircuitBreak 检查熔断状态，若已熔断自动记录（含剩余冷却时间）并返回 true
func (it *Iterator) SkipCircuitBreak(channelID, channelKeyID int, channelName string) bool {
	modelName := it.candidates[it.index].ModelName
	tripped, remaining := IsTripped(channelID, channelKeyID, modelName)
	if !tripped {
		return false
	}
	msg := "circuit breaker tripped"
	if remaining > 0 {
		msg = fmt.Sprintf("circuit breaker tripped, remaining cooldown: %ds", int(remaining.Seconds()))
	}
	it.count++
	it.attempts = append(it.attempts, model.ChannelAttempt{
		ChannelID:    channelID,
		ChannelKeyID: channelKeyID,
		ChannelName:  channelName,
		ModelName:    modelName,
		AttemptNum:   it.count,
		Status:       model.AttemptCircuitBreak,
		Sticky:       it.IsSticky(),
		Msg:          msg,
	})
	return true
}

// StartAttempt 开始一次真实转发尝试，返回 Span 用于记录结果
func (it *Iterator) StartAttempt(channelID, channelKeyID int, channelName string) *AttemptSpan {
	it.count++
	return &AttemptSpan{
		attempt: model.ChannelAttempt{
			ChannelID:    channelID,
			ChannelKeyID: channelKeyID,
			ChannelName:  channelName,
			ModelName:    it.candidates[it.index].ModelName,
			AttemptNum:   it.count,
			Sticky:       it.IsSticky(),
		},
		startTime: time.Now(),
		iter:      it,
	}
}

// Attempts 返回所有决策记录（交给日志模块持久化）
func (it *Iterator) Attempts() []model.ChannelAttempt {
	return it.attempts
}

// AttemptSpan 管理单次通道尝试的生命周期（计时、状态、结果）
type AttemptSpan struct {
	attempt   model.ChannelAttempt
	startTime time.Time
	iter      *Iterator
	ended     bool
}

// SetProtocolDecision attaches protocol routing metadata before the attempt is
// ended and appended to the iterator log.
func (s *AttemptSpan) SetProtocolDecision(mode, ingress, selected, kind, fallbackReason string) {
	s.attempt.ProtocolMode = mode
	s.attempt.IngressProtocol = ingress
	s.attempt.SelectedProtocol = selected
	s.attempt.AttemptKind = kind
	s.attempt.FallbackReason = fallbackReason
}

// End 结束尝试：设置状态，自动计算耗时，追加到 Iterator
func (s *AttemptSpan) End(status model.AttemptStatus, statusCode int, msg string) {
	if s.ended {
		return
	}
	s.ended = true
	s.attempt.Status = status
	s.attempt.Duration = int(time.Since(s.startTime).Milliseconds())
	s.attempt.Msg = msg
	s.iter.attempts = append(s.iter.attempts, s.attempt)
}

// Duration 返回从开始到现在的耗时
func (s *AttemptSpan) Duration() time.Duration {
	return time.Since(s.startTime)
}
