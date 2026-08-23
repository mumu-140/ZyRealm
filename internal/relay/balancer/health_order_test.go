package balancer

import (
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/outlierwindow"
)

func mkItem(id, priority int, channelID int) model.GroupItem {
	return model.GroupItem{ID: id, Priority: priority, ChannelID: channelID, ModelName: "m"}
}

// seedHealth 标记某个渠道-模型为健康（多次成功）或差（多次失败）。
func seedHealth(channelID int, successes, failures int, now time.Time) {
	for i := 0; i < successes; i++ {
		outlierwindow.Report(channelID, "m", true, 200, now)
	}
	for i := 0; i < failures; i++ {
		outlierwindow.Report(channelID, "m", false, 500, now)
	}
}

func TestHealthFirstOrdersHealthyFirst(t *testing.T) {
	Reset()
	now := time.Now()
	// ch1 健康（10 成功），ch2 差（10 失败），ch3 冷启动（无样本）
	seedHealth(101, 10, 0, now)
	seedHealth(102, 0, 10, now)

	b := &HealthFirst{}
	items := []model.GroupItem{
		mkItem(1, 1, 102), // 差
		mkItem(2, 1, 103), // 冷启动 → 中性 0.5 → 降级档
		mkItem(3, 1, 101), // 健康
	}
	got := b.Candidates(items)
	// 健康档(101) 必须在降级档(103) 之前，差档(102) 最后
	if got[0].ChannelID != 101 {
		t.Fatalf("first should be healthy ch101, got %d", got[0].ChannelID)
	}
	if got[2].ChannelID != 102 {
		t.Fatalf("last should be bad ch102, got %d", got[2].ChannelID)
	}
}

func TestHealthFirstTierRotation(t *testing.T) {
	Reset()
	now := time.Now()
	// 三个同健康通道在同一档内，连续多次调用应轮换起始通道（不盯死）
	seedHealth(201, 10, 0, now)
	seedHealth(202, 10, 0, now)
	seedHealth(203, 10, 0, now)

	b := &HealthFirst{}
	items := []model.GroupItem{
		mkItem(1, 1, 201),
		mkItem(2, 1, 202),
		mkItem(3, 1, 203),
	}
	seenFirst := map[int]struct{}{}
	for i := 0; i < 30; i++ {
		got := b.Candidates(items)
		seenFirst[got[0].ChannelID] = struct{}{}
		// 所有返回的应仍是这三个通道
		if len(got) != 3 {
			t.Fatalf("iter %d: got %d candidates, want 3", i, len(got))
		}
	}
	if len(seenFirst) < 2 {
		t.Fatalf("expected rotation across >=2 first channels, got %d distinct", len(seenFirst))
	}
}

func TestHealthFirstColdStartNeutral(t *testing.T) {
	Reset()
	b := &HealthFirst{}
	items := []model.GroupItem{
		mkItem(1, 1, 301),
		mkItem(2, 2, 302),
	}
	got := b.Candidates(items)
	// 双方均冷启动中性分 0.5，同档（降级档）会轮换；仅校验两通道都在结果中
	if len(got) != 2 {
		t.Fatalf("want 2 candidates, got %d", len(got))
	}
	seen := map[int]struct{}{}
	for _, it := range got {
		seen[it.ChannelID] = struct{}{}
	}
	if len(seen) != 2 {
		t.Fatalf("expected both cold-start channels, got %v", seen)
	}
}

// seedTier 把某渠道-模型喂到指定档位。
// 先失败后成功，让 ConsecutiveFails 归零：档位只由失败率决定，
// 否则连续失败罚分会把本该降级档的样本压进差档，测试的档位前提就不成立。
func seedTier(t *testing.T, channelID, tier int, now time.Time) {
	t.Helper()
	outlierwindow.ClearChannel(channelID)
	switch tier {
	case healthTierGood:
		for i := 0; i < 10; i++ {
			outlierwindow.Report(channelID, "m", true, 200, now)
		}
	case healthTierDeg:
		for i := 0; i < 5; i++ {
			outlierwindow.Report(channelID, "m", false, 500, now)
		}
		for i := 0; i < 5; i++ {
			outlierwindow.Report(channelID, "m", true, 200, now)
		}
	case healthTierBad:
		for i := 0; i < 10; i++ {
			outlierwindow.Report(channelID, "m", false, 500, now)
		}
	}
	// 前提自检：档位喂错时立即失败，而不是让后面的轮换断言给出误导性结果
	if got := healthTierOf(itemHealthScore(channelID, "m", now)); got != tier {
		t.Fatalf("seedTier(ch%d) 落在档位 %d, want %d (score=%.3f)",
			channelID, got, tier, itemHealthScore(channelID, "m", now))
	}
}

// blockOf 取结果中 [from,to) 段的渠道集合，用于断言档边界不串档。
func blockOf(got []model.GroupItem, from, to int) map[int]struct{} {
	s := map[int]struct{}{}
	for _, it := range got[from:to] {
		s[it.ChannelID] = struct{}{}
	}
	return s
}

func isSet(got map[int]struct{}, want ...int) bool {
	if len(got) != len(want) {
		return false
	}
	for _, w := range want {
		if _, ok := got[w]; !ok {
			return false
		}
	}
	return true
}

// TestHealthFirstSingleTierRotatesStrictly 单档三候选：连续请求严格轮换，
// 9 次恰好每个候选各领先 3 次（既不盯死也不跳号）。
func TestHealthFirstSingleTierRotatesStrictly(t *testing.T) {
	Reset()
	now := time.Now()
	ids := []int{801, 802, 803}
	for _, id := range ids {
		seedTier(t, id, healthTierGood, now)
	}
	items := []model.GroupItem{mkItem(1, 1, 801), mkItem(2, 1, 802), mkItem(3, 1, 803)}

	b := &HealthFirst{}
	var heads []int
	for i := 0; i < 9; i++ {
		got := b.Candidates(items)
		if len(got) != 3 {
			t.Fatalf("第 %d 次候选数 = %d, want 3", i, len(got))
		}
		heads = append(heads, got[0].ChannelID)
	}
	for i := 1; i < len(heads); i++ {
		if heads[i] == heads[i-1] {
			t.Fatalf("连续两次首位相同（盯死最优）：%v", heads)
		}
	}
	count := map[int]int{}
	for _, h := range heads {
		count[h]++
	}
	for _, id := range ids {
		if count[id] != 3 {
			t.Fatalf("ch%d 领先 %d 次, want 3；heads=%v", id, count[id], heads)
		}
	}
}

// TestHealthFirstTwoTiersBothRotate 两档各 2 候选——「每请求只推进一次 rotation」的关键回归。
// 旧实现在档循环内部各调一次 nextRotation：两档偏移按同一序列连续推进（+1/+2、+3/+4…），
// 奇偶相消，两档顺序在连续请求间完全冻结。改为档外只取一次 offset 后，两档都随请求轮换。
func TestHealthFirstTwoTiersBothRotate(t *testing.T) {
	Reset()
	now := time.Now()
	seedTier(t, 811, healthTierGood, now)
	seedTier(t, 812, healthTierGood, now)
	seedTier(t, 821, healthTierDeg, now)
	seedTier(t, 822, healthTierDeg, now)
	items := []model.GroupItem{
		mkItem(1, 1, 811), mkItem(2, 1, 812),
		mkItem(3, 1, 821), mkItem(4, 1, 822),
	}

	b := &HealthFirst{}
	var goodHeads, degHeads []int
	for i := 0; i < 4; i++ {
		got := b.Candidates(items)
		if len(got) != 4 {
			t.Fatalf("第 %d 次候选数 = %d, want 4", i, len(got))
		}
		// 档间顺序稳定：健康档恒占前两位，降级档恒占后两位
		if !isSet(blockOf(got, 0, 2), 811, 812) || !isSet(blockOf(got, 2, 4), 821, 822) {
			t.Fatalf("第 %d 次档边界被打破：%v", i, headChannels(got))
		}
		goodHeads = append(goodHeads, got[0].ChannelID)
		degHeads = append(degHeads, got[2].ChannelID)
	}
	for i := 1; i < 4; i++ {
		if goodHeads[i] == goodHeads[i-1] {
			t.Fatalf("健康档顺序冻结（rotation 未推进）：%v", goodHeads)
		}
		if degHeads[i] == degHeads[i-1] {
			t.Fatalf("降级档顺序冻结（rotation 未推进）：%v", degHeads)
		}
	}
}

// TestHealthFirstThreeTiersAllRotate 三档各 2 候选：档间顺序稳定，三档内部都轮换。
func TestHealthFirstThreeTiersAllRotate(t *testing.T) {
	Reset()
	now := time.Now()
	seedTier(t, 831, healthTierGood, now)
	seedTier(t, 832, healthTierGood, now)
	seedTier(t, 841, healthTierDeg, now)
	seedTier(t, 842, healthTierDeg, now)
	seedTier(t, 851, healthTierBad, now)
	seedTier(t, 852, healthTierBad, now)
	items := []model.GroupItem{
		mkItem(1, 1, 851), mkItem(2, 1, 841), mkItem(3, 1, 831),
		mkItem(4, 1, 852), mkItem(5, 1, 842), mkItem(6, 1, 832),
	}

	b := &HealthFirst{}
	heads := make([][3]int, 0, 4)
	for i := 0; i < 4; i++ {
		got := b.Candidates(items)
		if len(got) != 6 {
			t.Fatalf("第 %d 次候选数 = %d, want 6", i, len(got))
		}
		if !isSet(blockOf(got, 0, 2), 831, 832) ||
			!isSet(blockOf(got, 2, 4), 841, 842) ||
			!isSet(blockOf(got, 4, 6), 851, 852) {
			t.Fatalf("第 %d 次档边界被打破：%v", i, headChannels(got))
		}
		heads = append(heads, [3]int{got[0].ChannelID, got[2].ChannelID, got[4].ChannelID})
	}
	tierName := []string{"健康", "降级", "差"}
	for tier := 0; tier < 3; tier++ {
		for i := 1; i < len(heads); i++ {
			if heads[i][tier] == heads[i-1][tier] {
				t.Fatalf("%s档顺序冻结：%v", tierName[tier], heads)
			}
		}
	}
}

// TestHealthFirstEmptyTierSkipped 中间档为空（只有健康档与差档）时排序与轮换照常。
func TestHealthFirstEmptyTierSkipped(t *testing.T) {
	Reset()
	now := time.Now()
	seedTier(t, 861, healthTierGood, now)
	seedTier(t, 862, healthTierGood, now)
	seedTier(t, 871, healthTierBad, now)
	seedTier(t, 872, healthTierBad, now)
	items := []model.GroupItem{
		mkItem(1, 1, 871), mkItem(2, 1, 861), mkItem(3, 1, 872), mkItem(4, 1, 862),
	}

	b := &HealthFirst{}
	var goodHeads, badHeads []int
	for i := 0; i < 4; i++ {
		got := b.Candidates(items)
		if len(got) != 4 {
			t.Fatalf("第 %d 次候选数 = %d, want 4", i, len(got))
		}
		if !isSet(blockOf(got, 0, 2), 861, 862) || !isSet(blockOf(got, 2, 4), 871, 872) {
			t.Fatalf("空档导致排序错位：%v", headChannels(got))
		}
		goodHeads = append(goodHeads, got[0].ChannelID)
		badHeads = append(badHeads, got[2].ChannelID)
	}
	for i := 1; i < 4; i++ {
		if goodHeads[i] == goodHeads[i-1] || badHeads[i] == badHeads[i-1] {
			t.Fatalf("空档场景下轮换冻结：good=%v bad=%v", goodHeads, badHeads)
		}
	}
}

// TestHealthFirstSingleMemberTierNoPanic 每档只有 1 个候选：segLen==1 分支不轮换也不越界。
func TestHealthFirstSingleMemberTierNoPanic(t *testing.T) {
	Reset()
	now := time.Now()
	seedTier(t, 881, healthTierGood, now)
	seedTier(t, 882, healthTierDeg, now)
	seedTier(t, 883, healthTierBad, now)
	items := []model.GroupItem{mkItem(1, 1, 883), mkItem(2, 1, 881), mkItem(3, 1, 882)}

	b := &HealthFirst{}
	for i := 0; i < 5; i++ {
		got := b.Candidates(items)
		if len(got) != 3 {
			t.Fatalf("第 %d 次候选数 = %d, want 3", i, len(got))
		}
		if got[0].ChannelID != 881 || got[1].ChannelID != 882 || got[2].ChannelID != 883 {
			t.Fatalf("单成员档顺序应恒定为 881,882,883, got %v", headChannels(got))
		}
	}
}

// TestHealthFirstBucketsIsolatedAcrossItemSets 候选集合变化后各自独立轮换，
// 交替调用不互相推进游标（否则三候选集合会跳号退化）。
func TestHealthFirstBucketsIsolatedAcrossItemSets(t *testing.T) {
	Reset()
	now := time.Now()
	for _, id := range []int{891, 892, 893, 901, 902} {
		seedTier(t, id, healthTierGood, now)
	}
	setA := []model.GroupItem{mkItem(1, 1, 891), mkItem(2, 1, 892), mkItem(3, 1, 893)}
	setB := []model.GroupItem{mkItem(4, 1, 901), mkItem(5, 1, 902)}

	b := &HealthFirst{}
	var headsA, headsB []int
	for i := 0; i < 3; i++ {
		headsA = append(headsA, b.Candidates(setA)[0].ChannelID)
		headsB = append(headsB, b.Candidates(setB)[0].ChannelID)
	}
	distinct := map[int]struct{}{}
	for _, h := range headsA {
		distinct[h] = struct{}{}
	}
	if len(distinct) != 3 {
		t.Fatalf("setA 三次未覆盖三个候选（游标被 setB 推进）：%v", headsA)
	}
	for i := 1; i < 3; i++ {
		if headsB[i] == headsB[i-1] {
			t.Fatalf("setB 未严格轮换：%v", headsB)
		}
	}
}

// TestHealthFirstLongRunNoFreeze 长期行为：单档 4 候选跑 40 次，
// 每个候选领先次数必须严格均等，任一候选被冻结或饥饿都会失败。
func TestHealthFirstLongRunNoFreeze(t *testing.T) {
	Reset()
	now := time.Now()
	ids := []int{911, 912, 913, 914}
	items := make([]model.GroupItem, 0, len(ids))
	for i, id := range ids {
		seedTier(t, id, healthTierGood, now)
		items = append(items, mkItem(i+1, 1, id))
	}

	b := &HealthFirst{}
	count := map[int]int{}
	const runs = 40
	for i := 0; i < runs; i++ {
		count[b.Candidates(items)[0].ChannelID]++
	}
	for _, id := range ids {
		if count[id] != runs/len(ids) {
			t.Fatalf("ch%d 领先 %d 次, want %d；count=%v", id, count[id], runs/len(ids), count)
		}
	}
}

// TestHealthFirstColdStartNotStarved 冷启动（零样本）候选：
// 同档内必须参与轮换拿到领先位，否则「没有样本 → 永远不被选 → 永远没有样本」自锁。
func TestHealthFirstColdStartNotStarved(t *testing.T) {
	Reset()
	ids := []int{921, 922, 923}
	items := make([]model.GroupItem, 0, len(ids))
	for i, id := range ids {
		outlierwindow.ClearChannel(id)
		items = append(items, mkItem(i+1, 1, id))
	}

	b := &HealthFirst{}
	count := map[int]int{}
	for i := 0; i < 9; i++ {
		count[b.Candidates(items)[0].ChannelID]++
	}
	for _, id := range ids {
		if count[id] == 0 {
			t.Fatalf("冷启动候选 ch%d 从未获得领先位：%v", id, count)
		}
	}
}

// TestHealthFirstColdStartAheadOfBad 冷启动排在差档之前：无证据不等于有坏证据。
func TestHealthFirstColdStartAheadOfBad(t *testing.T) {
	Reset()
	now := time.Now()
	outlierwindow.ClearChannel(931)
	seedTier(t, 932, healthTierBad, now)
	items := []model.GroupItem{mkItem(1, 1, 932), mkItem(2, 1, 931)}

	b := &HealthFirst{}
	got := b.Candidates(items)
	if got[0].ChannelID != 931 {
		t.Fatalf("冷启动应排在差档之前，首位 = %d, want 931", got[0].ChannelID)
	}
}

// headChannels 便于失败信息里打印顺序。
func headChannels(got []model.GroupItem) []int {
	out := make([]int, len(got))
	for i, it := range got {
		out[i] = it.ChannelID
	}
	return out
}
