package modname

import "testing"

// 搜索结果标题解析用的是真实页面结构的代表性片段。
// 真实页面里每条结果带两个同 id 的词条链接：标题链接 + "地址：
// www.mcmod.cn/class/…" 文本链接；主词条（"钠 (Sodium)"）常排在第 20 位以后。
const sampleResultPage = `<div class="result">
  <h4><a href="https://www.mcmod.cn/class/22721.html" target="_blank">MCSR <em>Sodium</em></a></h4>
  <p><a href="https://www.mcmod.cn/class/22721.html" target="_blank">www.mcmod.cn/class/22721.html</a></p>
</div>
<div class="result">
  <h4><a href="https://www.mcmod.cn/class/7276.html" target="_blank">Sodium 临时崩溃修复 (Sodium Crash Fix)</a></h4>
</div>
<div class="result">
  <h4><a href="https://www.mcmod.cn/class/22721.html" target="_blank">MCSR Sodium</a></h4>
</div>
<div class="result">
  <h4><a href="https://www.mcmod.cn/class/3533.html" target="_blank">钠 (Sodium)</a></h4>
  <p><a href="https://www.mcmod.cn/class/3533.html" target="_blank">www.mcmod.cn/class/3533.html</a></p>
</div>
<div class="result">
  <h4><a href="https://www.mcmod.cn/class/99999.html" target="_blank">某个没有英文名词条</a></h4>
</div>`

func TestSearchSelectionPicksTightestEnglishMatch(t *testing.T) {
	entries := extractEntries(sampleResultPage)
	// URL 文本链接不算标题：5 个词条（含重复 id 的 MCSR Sodium 两条被去重）
	if len(entries) != 4 {
		t.Fatalf("期望解析出 4 条结果，得到 %d", len(entries))
	}
	best := pickBest(entries, "sodium")
	if best == nil {
		t.Fatal("期望命中")
	}
	// "MCSR Sodium"（整词包含 sodium，无中文名）不该赢过
	// 英文与关键词等价且带中文名的主词条 "钠 (Sodium)"。
	if best.zh != "钠" || best.en != "Sodium" {
		t.Fatalf("期望 钠/Sodium，得到 %q/%q", best.zh, best.en)
	}
}

func TestSearchSelectionRejectsNonModEntries(t *testing.T) {
	page := `<a href="https://www.mcmod.cn/class/123.html">Sodium powered thing</a>`
	if pickBest(extractEntries(page), "vanilla") != nil {
		t.Fatal("关键词不含于任何标题时不应命中")
	}
}

// Modrinth 标题用空格分词，而文件名关键词被规范化成连字符连接
// （"Fabric API" → "fabric-api"）：分隔符不参与字面匹配才能命中。
func TestSearchSelectionMatchesAcrossSeparators(t *testing.T) {
	page := `<a href="https://www.mcmod.cn/class/3220.html">Fabric API（Fabric 支持库）</a>`
	best := pickBest(extractEntries(page), "fabric-api")
	if best == nil {
		t.Fatal("期望 fabric-api 命中标题 Fabric API")
	}
	if best.zh != "Fabric 支持库" || best.en != "Fabric API" {
		t.Fatalf("期望 Fabric 支持库/Fabric API，得到 %q/%q", best.zh, best.en)
	}
	if pickBest(extractEntries(page), "quilt-api") != nil {
		t.Fatal("词组不同时不应命中")
	}
}

// 词条英文名是粘连/带尾缀数字/复数形态时也要能命中：
// "MouseWheelie"、"AppliedEnergistics1"、"Iron Chests"。
func TestSearchSelectionMatchesGluedAndSuffixedTitles(t *testing.T) {
	cases := []struct {
		page, key, wantZh string
	}{
		{`<a href="https://www.mcmod.cn/class/3361.html">急速滚轮 (MouseWheelie)</a>`,
			"mouse-wheelie", "急速滚轮"},
		{`<a href="https://www.mcmod.cn/class/235.html">[AE] 应用能源 (AppliedEnergistics1)</a>`,
			"applied-energistics", "[AE] 应用能源"},
		{`<a href="https://www.mcmod.cn/class/3095.html">铁箱子 (Iron Chests)</a>`,
			"iron-chest", "铁箱子"},
	}
	for _, tc := range cases {
		best := pickBest(extractEntries(tc.page), tc.key)
		if best == nil {
			t.Fatalf("期望 %q 命中（%s）", tc.key, tc.page)
		}
		if best.zh != tc.wantZh {
			t.Fatalf("key %q 期望 %q，得到 %q", tc.key, tc.wantZh, best.zh)
		}
	}
}
