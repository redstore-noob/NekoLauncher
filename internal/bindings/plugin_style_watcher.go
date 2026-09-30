package bindings

// 插件样式热更新：轮询 plugins/ 下全部 .css 文件（modtime + 体积），发现变化
// 就向前端发 plugin:styles:changed 事件（载荷 pluginId + 变化文件列表），前端
// 自动重新拉取并重注入对应 <style>——作者改 CSS 存盘即生效，不用手点「重新加载」。
//
// 不引入 fsnotify：本地单用户场景下插件 CSS 就几个文件，1.5 秒一轮 stat 的
// 开销可以忽略，换来零新依赖与纯函数式的可测试扫描。
//
// 防抖：文件写入可能跨越一个轮询周期，刚检出的变化先进入 pending 挂一轮，
// 下一轮 modtime/体积都没再变才真正下发——作者连续保存也不会触发多次重注入。

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// pluginStyleWatchInterval 轮询周期。
const pluginStyleWatchInterval = 1500 * time.Millisecond

// styleStamp 样式文件的变化判据：modtime + 体积（体积兜底秒级精度时间戳的文件系统）。
type styleStamp struct {
	modTime int64
	size    int64
}

// styleChange 一个插件的一批变化样式文件（相对插件目录的 slash 路径）。
type styleChange struct {
	pluginID string
	files    []string
}

// styleChangedEvent 前端事件载荷（plugin:styles:changed）。
type styleChangedEvent struct {
	PluginID string   `json:"pluginId"`
	Files    []string `json:"files"`
}

// pluginStyleWatcher 样式监听状态。挂在 PluginAPI 上随 Startup 启动。
type pluginStyleWatcher struct {
	// snapshot 上一轮扫描结果：相对插件的 "id/文件" 键 → 指纹。
	snapshot map[string]styleStamp
	// pending 已检出但尚未下发（等下一轮确认稳定）的变化："id/文件" → 检出时指纹。
	pending map[string]styleStamp
}

func newPluginStyleWatcher() *pluginStyleWatcher {
	return &pluginStyleWatcher{
		snapshot: make(map[string]styleStamp),
		pending:  make(map[string]styleStamp),
	}
}

// poll 扫一轮，返回本轮确认稳定的变化。内部状态自更新——它只被
// watchStyles 的单 goroutine 调用。
//
// snapshot 只收录已稳定下发的文件：新文件先在 pending 挂一轮，指纹不再
// 变化才进入 snapshot 并下发；这样新增与修改走同一条"检出 → 稳定"路径。
func (w *pluginStyleWatcher) poll(root string) []styleChange {
	current, err := scanPluginStyles(root)
	if err != nil {
		// 目录暂时不可读（被占用/正在卸载）：跳过本轮，保留旧状态
		return nil
	}

	stable := make([]string, 0, 2)
	for key, stamp := range current {
		if old, ok := w.snapshot[key]; ok && old == stamp {
			continue
		}
		if pending, ok := w.pending[key]; ok && pending == stamp {
			// 本轮指纹与检出时一致：写入稳定，下发
			stable = append(stable, key)
		} else {
			// 首次检出或仍在变动：再挂一轮
			w.pending[key] = stamp
		}
	}

	// 快照同步：删掉已消失的文件，收录本轮确认稳定的
	for key := range w.snapshot {
		if _, ok := current[key]; !ok {
			delete(w.snapshot, key)
		}
	}
	for _, key := range stable {
		w.snapshot[key] = w.pending[key]
		delete(w.pending, key)
	}

	if len(stable) == 0 {
		return nil
	}
	return groupStyleChanges(stable)
}

// pluginStyleWatchMaxDepth 递归深度上限：CSS 不会埋得再深，挡住异常目录树。
const pluginStyleWatchMaxDepth = 8

// scanPluginStyles 扫描插件根目录下全部 .css 文件，返回 "插件id/相对路径" → 指纹。
// 根目录不存在返回空表（还没装过插件不算错误）。
//
// 用递归 ReadDir 而不是 filepath.Walk：ReadDir 在 Windows 上自带目录项元数据，
// 只对 .css 文件调 Info()，不必为每个无关文件付一次 stat；插件目录里可能
// 塞着几万个内容文件，这笔账不能省。
func scanPluginStyles(root string) (map[string]styleStamp, error) {
	stamps := make(map[string]styleStamp)
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return stamps, nil
		}
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pluginID := entry.Name()
		pluginDir := filepath.Join(root, pluginID)
		scanPluginStylesDir(pluginDir, pluginDir, pluginID, 0, stamps)
	}
	return stamps, nil
}

// scanPluginStylesDir 递归收录 directory 下的 .css 文件（relative 取相对插件目录的路径）。
func scanPluginStylesDir(directory, pluginDir, pluginID string, depth int, stamps map[string]styleStamp) {
	if depth > pluginStyleWatchMaxDepth {
		return
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return // 单个目录不可读不阻塞整轮
	}
	for _, entry := range entries {
		path := filepath.Join(directory, entry.Name())
		if entry.IsDir() {
			scanPluginStylesDir(path, pluginDir, pluginID, depth+1, stamps)
			continue
		}
		if !strings.EqualFold(filepath.Ext(entry.Name()), ".css") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue // 单个文件不可读不阻塞整轮
		}
		relative, err := filepath.Rel(pluginDir, path)
		if err != nil {
			continue
		}
		stamps[pluginID+"/"+filepath.ToSlash(relative)] = styleStamp{
			modTime: info.ModTime().UnixNano(),
			size:    info.Size(),
		}
	}
}

// groupStyleChanges 把 "id/文件" 键按插件分组（顺序保持传入序）。
func groupStyleChanges(keys []string) []styleChange {
	index := make(map[string]int)
	changes := make([]styleChange, 0, 2)
	for _, key := range keys {
		id, file, ok := strings.Cut(key, "/")
		if !ok || file == "" {
			continue
		}
		if at, exists := index[id]; exists {
			changes[at].files = append(changes[at].files, file)
			continue
		}
		index[id] = len(changes)
		changes = append(changes, styleChange{pluginID: id, files: []string{file}})
	}
	return changes
}

// watchStyles 轮询循环：Startup 起一个 goroutine，直到 ctx 取消。
func (a *PluginAPI) watchStyles(ctx context.Context) {
	watcher := newPluginStyleWatcher()
	ticker := time.NewTicker(pluginStyleWatchInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, change := range watcher.poll(a.directory()) {
				emit(ctx, "plugin:styles:changed", styleChangedEvent{
					PluginID: change.pluginID,
					Files:    change.files,
				})
			}
		}
	}
}
