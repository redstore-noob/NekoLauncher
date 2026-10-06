package bindings

// SystemAPI 扩展：RSS 小组件的订阅源抓取与解析。
// 放后端而不是 WebView fetch：任意外站的 RSS 没有 CORS 头，前端拿不到；
// Go 侧顺带做限长与超时，坏源不至于拖垮界面。

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// RssFeedItem 订阅源里的一条内容。
type RssFeedItem struct {
	// Title 条目标题（已去空白）。
	Title string `json:"Title"`
	// Link 条目链接（点开跳浏览器）。
	Link string `json:"Link"`
	// Published 发布时间的本地化文本（解析不出时为空串，前端按"无日期"展示）。
	Published string `json:"Published"`
}

// rssMaximumItems 单次返回的条目上限——小组件卡片一屏也就放得下这么多。
const rssMaximumItems = 10

// FetchRssFeed 抓取并解析一个 RSS 2.0 / Atom 订阅源，返回最近的条目（新→旧）。
// URL 非法、抓取失败、内容不是合法 RSS/Atom 时返回错误，由前端展示"加载失败"。
func (a *SystemAPI) FetchRssFeed(rawURL string) ([]RssFeedItem, error) {
	url := strings.TrimSpace(rawURL)
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return nil, fmt.Errorf("订阅源地址必须以 http:// 或 https:// 开头")
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("订阅源抓取失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("订阅源返回 HTTP %d", resp.StatusCode)
	}

	// 4MB 上限：正常 RSS 都在几百 KB 内，超大响应多半是被指向了别的什么
	const maximumRSSBytes = 4 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maximumRSSBytes))
	if err != nil {
		return nil, fmt.Errorf("订阅源读取失败：%w", err)
	}

	items, err := parseRssFeed(data)
	if err != nil {
		return nil, err
	}
	if len(items) > rssMaximumItems {
		items = items[:rssMaximumItems]
	}
	return items, nil
}

// ---- 解析：RSS 2.0 与 Atom 两种格式各一套结构体，谁的根元素匹配用谁 ----

type rssChannelXML struct {
	Channel struct {
		Items []struct {
			Title   string `xml:"title"`
			Link    string `xml:"link"`
			PubDate string `xml:"pubDate"`
		} `xml:"item"`
	} `xml:"channel"`
}

type atomFeedXML struct {
	Entries []struct {
		Title string `xml:"title"`
		Links []struct {
			Href string `xml:"href,attr"`
			Rel  string `xml:"rel,attr"`
		} `xml:"link"`
		Updated string `xml:"updated"`
	} `xml:"entry"`
}

// parseRssFeed 解析 RSS 2.0 / Atom 文本；识别不出时返回错误。
func parseRssFeed(data []byte) ([]RssFeedItem, error) {
	var rss rssChannelXML
	if err := xml.Unmarshal(data, &rss); err == nil && len(rss.Channel.Items) > 0 {
		items := make([]RssFeedItem, 0, len(rss.Channel.Items))
		for _, item := range rss.Channel.Items {
			if strings.TrimSpace(item.Title) == "" {
				continue
			}
			items = append(items, RssFeedItem{
				Title:     strings.TrimSpace(item.Title),
				Link:      strings.TrimSpace(item.Link),
				Published: formatRssTime(item.PubDate),
			})
		}
		if len(items) > 0 {
			return items, nil
		}
	}

	var atom atomFeedXML
	if err := xml.Unmarshal(data, &atom); err == nil && len(atom.Entries) > 0 {
		items := make([]RssFeedItem, 0, len(atom.Entries))
		for _, entry := range atom.Entries {
			if strings.TrimSpace(entry.Title) == "" {
				continue
			}
			// Atom 的 link 是元素列表：优先 rel=alternate（正文页），否则第一条
			link := ""
			for _, l := range entry.Links {
				if l.Rel == "" || l.Rel == "alternate" {
					link = strings.TrimSpace(l.Href)
					if l.Rel == "alternate" {
						break
					}
				}
			}
			items = append(items, RssFeedItem{
				Title:     strings.TrimSpace(entry.Title),
				Link:      link,
				Published: formatRssTime(entry.Updated),
			})
		}
		if len(items) > 0 {
			return items, nil
		}
	}

	return nil, fmt.Errorf("内容不是可识别的 RSS / Atom 订阅源")
}

// formatRssTime 把 RFC 1123 / RFC 3339 两种常见时间格式转成本地可读文本。
func formatRssTime(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	for _, layout := range []string{time.RFC1123Z, time.RFC1123, time.RFC3339} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed.Local().Format("2006-01-02 15:04")
		}
	}
	return raw
}
