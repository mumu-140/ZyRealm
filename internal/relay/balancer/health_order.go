package balancer

import (
	"sort"
	"time"

	"github.com/bestruirui/octopus/internal/model"
)

// HealthFirst 健康优先：按渠道-模型健康分档（健康/降级/差），档间健康档先，
// 同档内轮换（连续请求不盯死同一候选），档间顺序稳定。
// 共享策略保留 legacy circuit 降档语义，供 Images 等尚未迁移的调用方使用；
// Core relay 复用同一排序实现，但关闭 legacy circuit 降档。
//
// 移植自 omniroute auto 策略的「健康分档 + 同档轮换」A 类本质：
//   - scoring.ts health 因子（这里退化为成败窗口，无 p95/cost）；
//   - engine.ts ScoreTierRotator 的「分 top/mid/rest 档 + 同档内轮换、不盯死最优」。
//
// 完整 13 维加权打分（含 p95/cost/quota）属 B 类，需新数据采集，不在本次范围。
type HealthFirst struct{}

// 健康档位：score 越高越健康
const (
	healthTierGood = 0 // score >= 0.6 且非熔断：健康档，最优先
	healthTierDeg  = 1 // 0.3 <= score < 0.6：降级档
	healthTierBad  = 2 // score < 0.3 或处于熔断：差档，兜底
)

func healthTierOf(score float64) int {
	switch {
	case score >= 0.6:
		return healthTierGood
	case score >= 0.3:
		return healthTierDeg
	default:
		return healthTierBad
	}
}

func (b *HealthFirst) Candidates(items []model.GroupItem) []model.GroupItem {
	return healthFirstCandidates(items, true, "hf:")
}

func healthFirstCandidates(items []model.GroupItem, includeLegacyCircuit bool, rotationBucket string) []model.GroupItem {
	n := len(items)
	if n == 0 {
		return nil
	}
	now := time.Now()

	type hfEntry struct {
		item  model.GroupItem
		score float64
		tier  int
	}
	es := make([]hfEntry, n)
	for i, item := range items {
		s := itemHealthScore(item.ChannelID, item.ModelName, now)
		tier := healthTierOf(s)
		// Legacy callers keep the historical circuit demotion. Core runtime
		// candidates already passed availability gating, so P4A disables it.
		if includeLegacyCircuit && tier != healthTierBad && PeekItemTripped(item.ChannelID, item.ModelName) {
			tier = healthTierBad
		}
		es[i] = hfEntry{item: item, score: s, tier: tier}
	}

	// 稳定排序：档位升序 → 同档按 score 降序 → 再按 Priority 升序
	sort.SliceStable(es, func(i, j int) bool {
		if es[i].tier != es[j].tier {
			return es[i].tier < es[j].tier
		}
		if es[i].score != es[j].score {
			return es[i].score > es[j].score
		}
		return es[i].item.Priority < es[j].item.Priority
	})

	// 同档内轮换：一次请求只推进一次 rotation，得到的 offset 供全部档位共用。
	// 每档取 offset % segLen 作为起始下标，使「同档内」的通道在连续请求间轮转
	// （不盯死最优），档间顺序仍稳定。
	// 计数器按用途+候选集合分桶，core 与 legacy 路径不会互相推进游标。
	offset := nextRotation(rotationBucket + itemSetKey(items))
	result := make([]model.GroupItem, n)
	tierStart := 0
	for k := 0; k < n; {
		tier := es[k].tier
		end := k
		for end < n && es[end].tier == tier {
			end++
		}
		segLen := end - k
		if segLen > 1 {
			off := int(offset % uint64(segLen))
			for idx := 0; idx < segLen; idx++ {
				result[tierStart+idx] = es[k+(off+idx)%segLen].item
			}
		} else {
			result[tierStart] = es[k].item
		}
		tierStart += segLen
		k = end
	}
	return result
}
