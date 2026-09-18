package balancer

import (
	"math"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/outlierwindow"
)

// mkWeighted 构造带权重的候选项。
func mkWeighted(id, channelID int, modelName string, weight int) model.GroupItem {
	return model.GroupItem{ID: id, ChannelID: channelID, ModelName: modelName, Priority: 1, Weight: weight}
}

// TestWeightedFirstPositionMatchesWeightRatio 加权策略首位命中概率必须等于 w_i/Σw。
// w=10:1 时理论值 0.909；旧公式 score=rand*w/total 降序实测约 0.95，落在断言区间外，
// 因此本测试对旧实现具备证伪能力。
func TestWeightedFirstPositionMatchesWeightRatio(t *testing.T) {
	Reset()
	b := &Weighted{}
	items := []model.GroupItem{
		mkWeighted(1, 601, "m", 10),
		mkWeighted(2, 602, "m", 1),
	}
	const runs = 200000
	hits := 0
	for i := 0; i < runs; i++ {
		if b.Candidates(items)[0].ChannelID == 601 {
			hits++
		}
	}
	got := float64(hits) / runs
	want := 10.0 / 11.0
	if math.Abs(got-want) > 0.01 {
		t.Fatalf("首位命中率 = %.4f, want %.4f±0.01（权重被系统性放大或压缩）", got, want)
	}
}

// TestWeightedZeroWeightTreatedAsOne 权重 <=0 按 1 处理，且不产生 +Inf 排序键。
func TestWeightedZeroWeightTreatedAsOne(t *testing.T) {
	Reset()
	b := &Weighted{}
	items := []model.GroupItem{
		mkWeighted(1, 611, "m", 0),
		mkWeighted(2, 612, "m", -5),
	}
	seen := map[int]int{}
	for i := 0; i < 4000; i++ {
		got := b.Candidates(items)
		if len(got) != 2 {
			t.Fatalf("want 2 candidates, got %d", len(got))
		}
		seen[got[0].ChannelID]++
	}
	// 等权：任一方首位占比应接近 0.5，至少不能出现一方永远不排首位
	for _, ch := range []int{611, 612} {
		ratio := float64(seen[ch]) / 4000
		if ratio < 0.4 || ratio > 0.6 {
			t.Fatalf("ch%d 首位占比 = %.3f, 等权应接近 0.5", ch, ratio)
		}
	}
}

// TestRoundRobinRotatesStrictly 单一候选集合内严格轮转（不跳号、不重复）。
func TestRoundRobinRotatesStrictly(t *testing.T) {
	Reset()
	b := &RoundRobin{}
	items := []model.GroupItem{
		mkWeighted(1, 621, "m", 1),
		mkWeighted(2, 622, "m", 1),
		mkWeighted(3, 623, "m", 1),
	}
	var heads []int
	for i := 0; i < 6; i++ {
		heads = append(heads, b.Candidates(items)[0].ChannelID)
	}
	for i := 1; i < len(heads); i++ {
		if heads[i] == heads[i-1] {
			t.Fatalf("连续两次首位相同：%v", heads)
		}
	}
	// 6 次应恰好覆盖 3 个候选各 2 次
	count := map[int]int{}
	for _, h := range heads {
		count[h]++
	}
	for _, ch := range []int{621, 622, 623} {
		if count[ch] != 2 {
			t.Fatalf("ch%d 出现 %d 次，want 2；heads=%v", ch, count[ch], heads)
		}
	}
}

// TestRoundRobinBucketsIsolatedAcrossItemSets 不同候选集合各自独立轮转。
// 原实现共用单个全局计数器，交替调用会让每个集合的游标每次 +2，
// 三候选集合退化为「621→623→622」跳号推进，本断言即可捕获。
func TestRoundRobinBucketsIsolatedAcrossItemSets(t *testing.T) {
	Reset()
	b := &RoundRobin{}
	setA := []model.GroupItem{
		mkWeighted(1, 631, "m", 1),
		mkWeighted(2, 632, "m", 1),
		mkWeighted(3, 633, "m", 1),
	}
	setB := []model.GroupItem{
		mkWeighted(4, 641, "m", 1),
		mkWeighted(5, 642, "m", 1),
	}
	var headsA, headsB []int
	for i := 0; i < 3; i++ {
		headsA = append(headsA, b.Candidates(setA)[0].ChannelID)
		headsB = append(headsB, b.Candidates(setB)[0].ChannelID)
	}
	// setA 三次应恰好遍历三个候选一遍
	distinct := map[int]struct{}{}
	for _, h := range headsA {
		distinct[h] = struct{}{}
	}
	if len(distinct) != 3 {
		t.Fatalf("setA 三次未覆盖三个候选（被 setB 推进游标）：%v", headsA)
	}
	if headsB[0] == headsB[1] || headsB[1] == headsB[2] {
		t.Fatalf("setB 未严格轮转：%v", headsB)
	}
}

// TestRoundRobinSameChannelDifferentModelsRotate 同渠道多模型是不同候选项，
// 必须各自参与轮转（候选项本身就是渠道-模型粒度）。
func TestRoundRobinSameChannelDifferentModelsRotate(t *testing.T) {
	Reset()
	b := &RoundRobin{}
	items := []model.GroupItem{
		mkWeighted(1, 651, "m1", 1),
		mkWeighted(2, 651, "m2", 1),
	}
	first := b.Candidates(items)[0]
	second := b.Candidates(items)[0]
	if first.ModelName == second.ModelName {
		t.Fatalf("同渠道两模型未轮转：%s / %s", first.ModelName, second.ModelName)
	}
}

// TestFailoverPrioritySurvivesHealthOrdering Priority 语义不能被健康度覆盖：
// 低 Priority（数值小=优先）即使不健康也必须排在高 Priority 之前。
func TestFailoverPrioritySurvivesHealthOrdering(t *testing.T) {
	Reset()
	now := time.Now()
	outlierwindow.ClearChannel(661)
	outlierwindow.ClearChannel(662)
	for i := 0; i < 10; i++ {
		outlierwindow.Report(661, "m", false, 500, now) // P1 但很差
		outlierwindow.Report(662, "m", true, 200, now)  // P2 且健康
	}
	b := &Failover{}
	items := []model.GroupItem{
		{ID: 2, Priority: 2, ChannelID: 662, ModelName: "m"},
		{ID: 1, Priority: 1, ChannelID: 661, ModelName: "m"},
	}
	got := b.Candidates(items)
	if got[0].ChannelID != 661 {
		t.Fatalf("Priority 优先级被破坏，首位 = %d, want 661", got[0].ChannelID)
	}
}

// TestFailoverSamePriorityPrefersHealthy 同 Priority 内健康项前移。
// 原实现只按 Priority 排序，坏项固定排前，每次请求都要先撞一次坏上游。
func TestFailoverSamePriorityPrefersHealthy(t *testing.T) {
	Reset()
	now := time.Now()
	outlierwindow.ClearChannel(671)
	outlierwindow.ClearChannel(672)
	for i := 0; i < 10; i++ {
		outlierwindow.Report(671, "m", false, 500, now) // 差
		outlierwindow.Report(672, "m", true, 200, now)  // 健康
	}
	b := &Failover{}
	items := []model.GroupItem{
		{ID: 1, Priority: 1, ChannelID: 671, ModelName: "m"},
		{ID: 2, Priority: 1, ChannelID: 672, ModelName: "m"},
	}
	got := b.Candidates(items)
	if got[0].ChannelID != 672 {
		t.Fatalf("同 Priority 未按健康度排序，首位 = %d, want 672", got[0].ChannelID)
	}
}

// TestFailoverModelHealthDoesNotLeakAcrossModels 同渠道两模型：
// 只有出错的模型该被后移，另一个模型不受牵连。
func TestFailoverModelHealthDoesNotLeakAcrossModels(t *testing.T) {
	Reset()
	now := time.Now()
	outlierwindow.ClearChannel(681)
	for i := 0; i < 10; i++ {
		outlierwindow.Report(681, "bad", false, 503, now)
		outlierwindow.Report(681, "good", true, 200, now)
	}
	b := &Failover{}
	items := []model.GroupItem{
		{ID: 1, Priority: 1, ChannelID: 681, ModelName: "bad"},
		{ID: 2, Priority: 1, ChannelID: 681, ModelName: "good"},
	}
	got := b.Candidates(items)
	if got[0].ModelName != "good" {
		t.Fatalf("同渠道模型级健康度未生效，首位 = %s, want good", got[0].ModelName)
	}
}

func TestHealthFirstModelLevelTiering(t *testing.T) {
	Reset()
	now := time.Now()
	outlierwindow.ClearChannel(711)
	for i := 0; i < 10; i++ {
		outlierwindow.Report(711, "bad", false, 503, now)
		outlierwindow.Report(711, "good", true, 200, now)
	}
	if s := itemHealthScore(711, "good", now); s < 0.6 {
		t.Fatalf("good 模型健康分 = %.3f, 应在健康档(>=0.6)", s)
	}
	if s := itemHealthScore(711, "bad", now); s >= 0.3 {
		t.Fatalf("bad 模型健康分 = %.3f, 应在差档(<0.3)", s)
	}
	b := &HealthFirst{}
	got := b.Candidates([]model.GroupItem{
		{ID: 1, Priority: 1, ChannelID: 711, ModelName: "bad"},
		{ID: 2, Priority: 1, ChannelID: 711, ModelName: "good"},
	})
	if got[0].ModelName != "good" {
		t.Fatalf("首位 = %s, want good", got[0].ModelName)
	}
}

// TestItemHealthScoreColdStartAndLowSamples 冷启动给中性分；样本不足向 0.5 回拉。
func TestItemHealthScoreColdStartAndLowSamples(t *testing.T) {
	Reset()
	now := time.Now()
	outlierwindow.ClearChannel(721)
	if s := itemHealthScore(721, "m", now); s != 0.5 {
		t.Fatalf("冷启动分 = %.3f, want 0.5", s)
	}
	// 3 条全失败：raw score 0（1-1.0 后再扣连续失败罚分），样本不足回拉到 0.25
	for i := 0; i < 3; i++ {
		outlierwindow.Report(721, "m", false, 500, now)
	}
	if s := itemHealthScore(721, "m", now); math.Abs(s-0.25) > 1e-9 {
		t.Fatalf("样本不足未向中性回拉，score = %.4f, want 0.25", s)
	}
}

