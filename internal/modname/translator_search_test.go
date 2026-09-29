package modname

import "testing"

// 搜索结果标题解析用的是真实页面结构的代表性片段。
const sampleResultPage = `<div class="result">
  <h4><a href="https://www.mcmod.cn/class/3533.html" target="_blank">钠 (Sodium)</a></h4>
  <p>现代渲染优化模组……</p>
</div>
<div class="result">
  <h4><a href="https://www.mcmod.cn/class/22721.html" target="_blank">MCSR Sodium</a></h4>
</div>
<div class="result">
  <h4><a href="https://www.mcmod.cn/class/3701.html" target="_blank">钠 · 扩展 (Sodium Extra)</a></h4>
</div>
<div class="result">
  <h4><a href="https://www.mcmod.cn/class/99999.html" target="_blank">某个没有英文名词条</a></h4>
</div>`

func TestSearchSelectionPicksTightestEnglishMatch(t *testing.T) {
	entries := extractEntries(sampleResultPage)
	if len(entries) != 4 {
		t.Fatalf("期望解析出 4 条结果，得到 %d", len(entries))
	}
	best := pickBest(entries, "sodium")
	if best == nil {
		t.Fatal("期望命中")
	}
	// "MCSR Sodium"（整词包含 sodium）与 "Sodium"（紧前是 /）都可能命中，
	// 取英文名最短者 → 主词条 "钠 (Sodium)"。
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
