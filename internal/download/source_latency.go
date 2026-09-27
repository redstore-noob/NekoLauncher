package download

// 下载源可用性/延迟探测：主页「网络状态」小组件使用。
// 复用 source_provider.go 的 MeasureLatency（HEAD 版本清单地址），并发测量全部内置源。

import (
	"context"
	"sync"
)

// SourceLatency 单个下载源的探测结果。
type SourceLatency struct {
	// Name 下载源名称（与 DownloadSource.Name 一致）。
	Name string `json:"Name"`
	// LatencyMs 往返延迟毫秒；失败为 -1。
	LatencyMs int `json:"LatencyMs"`
	// Available 是否可用（延迟 >= 0）。
	Available bool `json:"Available"`
}

// MeasureSourceLatencies 并发测量全部内置下载源的延迟。
// 每个源最多等待 5 秒（见 MeasureLatency），失败返回 LatencyMs=-1。
func MeasureSourceLatencies(ctx context.Context) []SourceLatency {
	sources := AllDownloadSources
	results := make([]SourceLatency, len(sources))

	var wg sync.WaitGroup
	for i, source := range sources {
		wg.Add(1)
		go func(index int, s DownloadSource) {
			defer wg.Done()
			ms := SourceProvider.MeasureLatency(ctx, s)
			results[index] = SourceLatency{Name: s.Name, LatencyMs: ms, Available: ms >= 0}
		}(i, source)
	}
	wg.Wait()

	return results
}
