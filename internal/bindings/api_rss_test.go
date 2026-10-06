package bindings

import "testing"

func TestParseRssFeed(t *testing.T) {
	rss := []byte(`<?xml version="1.0"?>
<rss version="2.0"><channel><title>MC 新闻</title>
<item><title>快照发布</title><link>https://example.com/a</link><pubDate>Mon, 02 Jan 2006 15:04:05 -0700</pubDate></item>
<item><title>无链接条目</title></item>
<item><title></title><link>https://example.com/c</link></item>
</channel></rss>`)
	items, err := parseRssFeed(rss)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("应解析出 2 条（空标题跳过），得到 %d", len(items))
	}
	if items[0].Title != "快照发布" || items[0].Link != "https://example.com/a" {
		t.Fatalf("首条内容不对：%+v", items[0])
	}
	if items[0].Published == "" {
		t.Fatal("RSS pubDate 应被解析成可读时间")
	}
	if items[1].Title != "无链接条目" || items[1].Link != "" {
		t.Fatalf("无链接条目解析不对：%+v", items[1])
	}
}

func TestParseRssFeedAtom(t *testing.T) {
	atom := []byte(`<?xml version="1.0"?>
<feed xmlns="http://www.w3.org/2005/Atom">
<entry><title>文章一</title>
  <link rel="self" href="https://example.com/feed"/>
  <link rel="alternate" href="https://example.com/1"/>
  <updated>2026-10-01T08:00:00Z</updated></entry>
<entry><title>文章二</title>
  <link href="https://example.com/2"/>
  <updated>2026-09-30T08:00:00Z</updated></entry>
</feed>`)
	items, err := parseRssFeed(atom)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("应解析出 2 条，得到 %d", len(items))
	}
	// rel=alternate 优先于 self
	if items[0].Link != "https://example.com/1" {
		t.Fatalf("Atom 应优先 rel=alternate 链接，得到 %q", items[0].Link)
	}
	// 无 rel 的 link 作为正文链接
	if items[1].Link != "https://example.com/2" {
		t.Fatalf("无 rel 链接应被采用，得到 %q", items[1].Link)
	}
}

func TestParseRssFeedRejectsNonFeed(t *testing.T) {
	if _, err := parseRssFeed([]byte("<html><body>不是订阅源</body></html>")); err == nil {
		t.Fatal("非 RSS/Atom 内容应返回错误")
	}
	if _, err := parseRssFeed([]byte("")); err == nil {
		t.Fatal("空内容应返回错误")
	}
}
