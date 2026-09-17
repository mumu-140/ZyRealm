package balancer

import (
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
	skippedProviders map[int]struct{}

	// 内嵌追踪
	attempts         []model.ChannelAttempt
	count            int
	decisionEvents   []model.RoutingDecisionEvent
	decisionSequence int
}

// NewIterator 创建负载均衡迭代器
// 自动处理：运行态准入 + 策略排序 + 粘性通道提前
func NewIterator(group model.Group, apiKeyID int, requestModel string) *Iterator {
	return NewIteratorWithPreference(group, apiKeyID, requestModel, nil)
}

// NewIteratorWithPreference 创建带优先通道偏好的迭代器。
// runtime eligibility 在所有 GroupMode 之前统一应用；sticky 只能在当前
// AVAILABLE 候选中生效，不能把 SUSPECT/HALF_OPEN/COOLDOWN 通道提到首位。
func NewIteratorWithPreference(group model.Group, apiKeyID int, requestModel string, preferred *SessionEntry) *Iterator {
	now := time.Now()
	order := runtimeOrderedCandidatesWithDecisions(group, requestModel, now)
	candidates := order.Candidates

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

	iterator := &Iterator{
		candidates:       candidates,
		index:            -1,
		stickyIdx:        stickyIdx,
		stickyKeyID:      stickyKeyID,
		skippedProviders: make(map[int]struct{}),
	}
	for _, decision := range order.Decisions {
		iterator.RecordDecision(decision)
	}
	return iterator
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

// RecordDecision stores observational routing metadata without participating in
// candidate selection or attempt accounting. Sequence is request-local and is
// assigned here so callers cannot accidentally create unstable ordering.
func (it *Iterator) RecordDecision(event model.RoutingDecisionEvent) {
	if it == nil {
		return
	}
	it.decisionSequence++
	event.Sequence = it.decisionSequence
	it.decisionEvents = append(it.decisionEvents, event)
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

func cloneDecisionEvents(events []model.RoutingDecisionEvent) []model.RoutingDecisionEvent {
	if len(events) == 0 {
		return nil
	}
	return append([]model.RoutingDecisionEvent(nil), events...)
}

// Attempts returns a detached persistence snapshot. Decision events are
// materialized only here, so explanation data cannot feed back into scheduling,
// attempt numbering, fairness, health, retry, or failover control flow.
func (it *Iterator) Attempts() []model.ChannelAttempt {
	if it == nil {
		return nil
	}
	out := append([]model.ChannelAttempt(nil), it.attempts...)
	for i := range out {
		out[i].DecisionTraceVersion = model.RoutingDecisionTraceVersion
		out[i].DecisionEvents = cloneDecisionEvents(out[i].DecisionEvents)
	}
	if len(it.decisionEvents) == 0 {
		return out
	}
	events := cloneDecisionEvents(it.decisionEvents)
	if len(out) > 0 {
		// One deterministic envelope keeps top-level attempt cardinality and
		// AttemptNum semantics unchanged while preserving request-wide ordering.
		out[0].DecisionEvents = append(events, out[0].DecisionEvents...)
		return out
	}

	first := events[0]
	return []model.ChannelAttempt{{
		ChannelID:    first.ChannelID,
		ChannelKeyID: first.ChannelKeyID,
		ChannelName:  first.ChannelName,
		ModelName:    first.ModelName,
		AttemptNum:   0,
		Status:       model.AttemptSkipped,
		AttemptKind:  "decision_only",
		AttemptRoutingTrace: model.AttemptRoutingTrace{
			DecisionTraceVersion: model.RoutingDecisionTraceVersion,
			DecisionEvents:       events,
		},
	}}
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

// SetRoutingTrace attaches the immutable classification/directive snapshot.
// It is normally called before End, but updateAttempt also supports a late call.
func (s *AttemptSpan) SetRoutingTrace(trace model.AttemptRoutingTrace) {
	if s == nil {
		return
	}
	s.updateAttempt(func(attempt *model.ChannelAttempt) {
		attempt.AttemptRoutingTrace = trace
	})
}

// SetRoutingRuntime records the concrete shared-runtime state after the handler
// applies the decision. This deliberately happens after End for failed attempts.
func (s *AttemptSpan) SetRoutingRuntime(effect, state string, cooldownUntil int64) {
	if s == nil {
		return
	}
	s.updateAttempt(func(attempt *model.ChannelAttempt) {
		attempt.RuntimeEffect = effect
		attempt.RuntimeState = state
		attempt.CooldownUntil = cooldownUntil
	})
}

// SetFailoverStopReason records a late control-flow gate that intentionally
// prevented a routing decision from advancing to another candidate. The first
// concrete stop reason wins so a later generic exhaustion marker cannot hide it.
func (s *AttemptSpan) SetFailoverStopReason(reason string) {
	if s == nil || reason == "" {
		return
	}
	s.updateAttempt(func(attempt *model.ChannelAttempt) {
		if attempt.FailoverStopReason == "" {
			attempt.FailoverStopReason = reason
		}
	})
}

func (s *AttemptSpan) updateAttempt(update func(*model.ChannelAttempt)) {
	if s == nil || update == nil {
		return
	}
	update(&s.attempt)
	if !s.ended || s.iter == nil {
		return
	}
	for i := len(s.iter.attempts) - 1; i >= 0; i-- {
		if s.iter.attempts[i].AttemptNum == s.attempt.AttemptNum {
			update(&s.iter.attempts[i])
			return
		}
	}
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
