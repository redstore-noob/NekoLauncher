package bindings

// 场景载荷的 puppet 注入：把对象引用的 .mdl（puppet warp 网格）解析后
// 以 JSON 形式并入 objects 数组对应条目，前端据此用真实网格 + 骨骼动画
// 替代整图 quad 渲染（格式见 wallpaper_engine_mdl.go）。
//
// 解析结果按 (pkg 修改时间 + 条目路径 + 条目长度) 缓存：背景层以
// WALLPAPER_POLL_INTERVAL_MS 轮询重取载荷，不缓存的话每轮都重新解包
// 全部 .mdl（单个 30 万字节 × 11 个对象）。

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

// wePuppetPayload 注入对象 JSON 的 Puppet 字段（与前端 WEScenePuppet 对齐）。
type wePuppetPayload struct {
	Positions []float32 `json:"Positions"`
	Uvs       []float32 `json:"Uvs"`
	Indices   []uint16  `json:"Indices"`
	// BlendIndex/BlendWeight 每顶点 4 组（蒙皮权重）
	BlendIndex  []float32 `json:"BlendIndex"`
	BlendWeight []float32 `json:"BlendWeight"`
	// Bones rest 矩阵为列主序 4x4（m[12..14] 平移，设计坐标系）
	Bones []weMdlBone         `json:"Bones"`
	Anim  *weMdlAnimationJSON `json:"Animation,omitempty"`
}

// weMdlAnimationJSON 动画的 JSON 友好形态（相对 weMdlAnimation 展平关键帧）。
type weMdlAnimationJSON struct {
	Duration float32 `json:"Duration"`
	// Tracks 与骨骼一一对应；每帧 9 个 float（pos3 + euler3 + scale3）摊平存储，
	// 比嵌套对象省一个数量级的序列化开销。
	Tracks [][]float32 `json:"Tracks"`
}

type wePuppetCacheEntry struct {
	modTime int64
	size    int64
	payload *wePuppetPayload
}

var (
	wePuppetCacheGate sync.Mutex
	wePuppetCache     = map[string]wePuppetCacheEntry{}
)

// wePuppetForModelPath 解析对象引用的 puppet 模型；无 puppet 字段或解析
// 失败返回 nil（调用方回落整图 quad）。modelPath 是包内模型 JSON 路径。
func wePuppetForModelPath(reader *wePkgReader, modelPath string) *wePuppetPayload {
	clean := strings.ReplaceAll(modelPath, "\\", "/")
	modelEntry := reader.lookup(clean)
	if modelEntry == nil {
		return nil
	}
	modelData, err := reader.readEntry(modelEntry)
	if err != nil {
		return nil
	}
	var model struct {
		Puppet string `json:"puppet"`
	}
	if err := json.Unmarshal(modelData, &model); err != nil || strings.TrimSpace(model.Puppet) == "" {
		return nil
	}

	// puppet 路径与模型 JSON 同目录（models/xxx.json → models/xxx_puppet.mdl）
	puppetPath := path.Join(path.Dir(clean), path.Base(strings.ReplaceAll(model.Puppet, "\\", "/")))
	puppetEntry := reader.lookup(puppetPath)
	if puppetEntry == nil {
		return nil
	}

	info, err := os.Stat(reader.path)
	if err != nil {
		return nil
	}
	cacheKey := filepath.Clean(reader.path) + "|" + strings.ToLower(puppetPath)
	wePuppetCacheGate.Lock()
	cached, cachedOk := wePuppetCache[cacheKey]
	wePuppetCacheGate.Unlock()
	if cachedOk && cached.modTime == info.ModTime().UnixNano() && cached.size == info.Size() {
		return cached.payload
	}

	data, err := reader.readEntry(puppetEntry)
	if err != nil {
		return nil
	}
	parsed, err := ParseWeMdl(data)
	if err != nil {
		// 解析失败不致命：该对象回落整图 quad，记一条日志便于排障
		fmt.Printf("[WE] puppet 解析失败 %s: %v\n", puppetPath, err)
		parsed = nil
	}

	var payload *wePuppetPayload
	if parsed != nil {
		payload = &wePuppetPayload{
			Positions:   parsed.Mesh.Positions,
			Uvs:         parsed.Mesh.Uvs,
			Indices:     parsed.Mesh.Indices,
			BlendIndex:  parsed.Mesh.BlendIndex,
			BlendWeight: parsed.Mesh.BlendWeight,
			Bones:       parsed.Bones,
		}
		if parsed.Animation != nil {
			tracks := make([][]float32, len(parsed.Animation.Tracks))
			for i, track := range parsed.Animation.Tracks {
				flat := make([]float32, len(track)*9)
				for f, frame := range track {
					copy(flat[f*9:], frame.Pos[:])
					copy(flat[f*9+3:], frame.Rot[:])
					copy(flat[f*9+6:], frame.Scale[:])
				}
				tracks[i] = flat
			}
			payload.Anim = &weMdlAnimationJSON{
				Duration: parsed.Animation.Duration,
				Tracks:   tracks,
			}
		}
	}

	wePuppetCacheGate.Lock()
	wePuppetCache[cacheKey] = wePuppetCacheEntry{
		modTime: info.ModTime().UnixNano(), size: info.Size(), payload: payload,
	}
	wePuppetCacheGate.Unlock()
	return payload
}

// weInjectPuppets 遍历 objects 数组，为带 image 的对象注入 Puppet 字段。
// 输入输出都是原始 JSON（保持其余字段原样透传）。
func weInjectPuppets(reader *wePkgReader, objects json.RawMessage) json.RawMessage {
	var items []map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(objects))
	if err := decoder.Decode(&items); err != nil {
		return objects
	}

	changed := false
	for _, item := range items {
		var image string
		if err := json.Unmarshal(item["image"], &image); err != nil || image == "" {
			continue
		}
		puppet := wePuppetForModelPath(reader, image)
		if puppet == nil {
			continue
		}
		encoded, err := json.Marshal(puppet)
		if err != nil {
			continue
		}
		item["Puppet"] = encoded
		changed = true
	}
	if !changed {
		return objects
	}

	encoded, err := json.Marshal(items)
	if err != nil {
		return objects
	}
	return encoded
}
