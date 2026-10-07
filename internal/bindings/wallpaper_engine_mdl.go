package bindings

// Wallpaper Engine 的 puppet 模型（.mdl）解析：puppet warp 骨骼网格。
//
// 文件是三个前后相接的二进制块（自研逆向，布局经 linux-wallpaperengine
// 的 mesh 读取路径与真机样本交叉验证，见 wallpaper_engine_mdl_test.go）：
//
//	MDLV00xx\0  网格块：顶点（stride 80：pos 3f + blendindices 4u32 +
//	            blendweight 4f + 5f 未知 + uv 2f）+ u16 三角形索引。
//	            块内没有可靠的长度头（0019/0021/0023 版本头部细节不一），
//	            按 lwe 同款启发式扫描：从 magic 之后找"vertexBytes 对齐
//	            stride 且索引区完整落在 MDLS 之前"的候选。
//	MDLS00xx\0  骨骼块：u32(块长) + u32(骨骼数)，每骨一条定长记录：
//	            tmp u8 + type u32 + parent i32 + 矩阵长 u32 +
//	            4x4 列主序 rest 矩阵（f32，平移在第 4 列）+ 名字（\0 结尾）。
//	MDLA00xx\0  动画块（可选）：头部（总长 u32 + 动画数 u32 + 采样数 u32 +
//	            保留 u32 + 动画名 zstr + 播放模式 zstr + 时长 f32 + 帧率 u32
//	            + 16 字节保留）+ u32(轨道数) + 每骨骼一条轨道
//	            （u32 字节数 + N 帧 × 9 f32（pos3+euler3+scale3）+ u32 尾标）。
//	            关键帧在时长内均匀分布（WE 导出为等间隔采样）。
//
// 只读不写；解析失败返回错误由调用方回落"整图 quad"渲染，puppet 是增强
// 而非依赖。

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
)

// weMdlPuppet 解析后的 puppet 载荷：网格 + 骨骼 + 动画。
type weMdlPuppet struct {
	// Mesh 顶点/UV/索引；positions 为 xyz 三元组，uv 为 uv 对。
	Mesh      weMdlMesh
	Bones     []weMdlBone
	Animation *weMdlAnimation
}

type weMdlMesh struct {
	Positions []float32
	Uvs       []float32
	Indices   []uint16
	// BlendIndex 每顶点 4 个骨骼索引。WE 以 **f32 编码**存索引（0.0/1.0/2.0…，
	// 单骨骼对象的值观测为 1.0，疑似 1-based 或复用权重位模式），按 u32 读会
	// 得到 0x3F800000 这类位模式。这里保持 float 原样传递，消费端（前端
	// makePuppetState）负责钳位归一化到 [0, 骨骼数)。
	BlendIndex  []float32
	BlendWeight []float32
}

type weMdlBone struct {
	Name   string
	Parent int // -1 = 根骨骼
	// Rest 列主序 4x4（m[12..14] 为平移），模型设计坐标系
	Rest [16]float32
}

// weMdlKeyframe 单个关键帧：位置 + 欧拉角(xyz 弧度) + 缩放。
type weMdlKeyframe struct {
	Pos   [3]float32
	Rot   [3]float32
	Scale [3]float32
}

type weMdlAnimation struct {
	Name     string
	Loop     bool
	Duration float32 // 秒
	// Tracks 与骨骼一一对应，关键帧均匀分布在 [0, Duration]。
	Tracks [][]weMdlKeyframe
}

// weMdlVertexStride 顶点记录步长（pos 12 + indices 16 + weights 16 + 未知 20 + uv 8）。
const weMdlVertexStride = 80

// weMdlUVOffset 顶点内 UV 的字节偏移（stride 80 的最后 8 字节）。
const weMdlUVOffset = 72

// ParseWeMdl 解析 puppet .mdl 全部三个块；网格或骨骼任一缺失即报错。
func ParseWeMdl(data []byte) (*weMdlPuppet, error) {
	if len(data) < 9 || !bytes.HasPrefix(data, []byte("MDLV")) {
		return nil, fmt.Errorf("不是 MDL 文件（magic=%q）", string(data[:min(8, len(data))]))
	}

	mdlsOffset := bytes.Index(data, []byte("MDLS"))
	if mdlsOffset < 0 {
		return nil, fmt.Errorf("缺少 MDLS 骨骼块")
	}

	mesh, err := parseWeMdlMesh(data, mdlsOffset)
	if err != nil {
		return nil, fmt.Errorf("解析 MDLV 网格失败: %w", err)
	}

	bones, err := parseWeMdlBones(data[mdlsOffset:])
	if err != nil {
		return nil, fmt.Errorf("解析 MDLS 骨骼失败: %w", err)
	}

	puppet := &weMdlPuppet{Mesh: *mesh, Bones: bones}
	if mdlaOffset := bytes.Index(data, []byte("MDLA")); mdlaOffset >= 0 {
		if animation := parseWeMdlAnimation(data[mdlaOffset:]); animation != nil {
			puppet.Animation = animation
		}
	}

	return puppet, nil
}

// parseWeMdlMesh 按 lwe 的启发式定位网格块：候选头在 magic 之后、MDLS 之前，
// vertexBytes 对齐 stride，索引区完整落在 MDLS 之前。首版头（u32 vertexBytes）
// 前还有一个 u32（版本内布局差异），两个偏移都试。
func parseWeMdlMesh(data []byte, mdlsOffset int) (*weMdlMesh, error) {
	type meshBlock struct {
		headerOffset int
		vertexBytes  uint32
		indexBytes   uint32
	}
	find := func(startOffset int) *meshBlock {
		for offset := startOffset; offset+8+4 < mdlsOffset; offset++ {
			vertexBytes := binary.LittleEndian.Uint32(data[offset+4:])

			if vertexBytes == 0 || vertexBytes%weMdlVertexStride != 0 {
				continue
			}
			indexLengthOffset := offset + 8 + int(vertexBytes)
			if indexLengthOffset+4 > mdlsOffset {
				continue
			}
			indexBytes := binary.LittleEndian.Uint32(data[indexLengthOffset:])

			if indexBytes == 0 || indexBytes%6 != 0 || indexLengthOffset+4+int(indexBytes) > mdlsOffset {
				continue
			}
			return &meshBlock{headerOffset: offset, vertexBytes: vertexBytes, indexBytes: indexBytes}
		}
		return nil
	}

	// 主尝试：头紧跟 magic（9 字节处起扫）；备用：跳过 magic 后的一个 u32
	block := find(9)
	if block == nil {
		block = find(13)
	}
	if block == nil {
		return nil, fmt.Errorf("未找到可用的网格块（扫描到 MDLS@%d）", mdlsOffset)
	}

	verticesOffset := block.headerOffset + 8
	vertexCount := int(block.vertexBytes / weMdlVertexStride)
	indicesOffset := verticesOffset + int(block.vertexBytes) + 4
	indexCount := int(block.indexBytes / 2)

	mesh := &weMdlMesh{
		Positions:   make([]float32, 0, vertexCount*3),
		Uvs:         make([]float32, 0, vertexCount*2),
		Indices:     make([]uint16, 0, indexCount),
		BlendIndex:  make([]float32, 0, vertexCount*4),
		BlendWeight: make([]float32, 0, vertexCount*4),
	}
	for i := range vertexCount {
		v := verticesOffset + i*weMdlVertexStride
		pos := struct{ X, Y, Z float32 }{}
		if _, err := binary.Decode(data[v:v+12], binary.LittleEndian, &pos); err != nil {
			return nil, fmt.Errorf("顶点 %d 位置解码失败: %w", i, err)
		}
		mesh.Positions = append(mesh.Positions, pos.X, pos.Y, pos.Z)

		bi := make([]float32, 4)
		if _, err := binary.Decode(data[v+12:v+28], binary.LittleEndian, &bi); err != nil {
			return nil, fmt.Errorf("顶点 %d 骨骼索引解码失败: %w", i, err)
		}
		mesh.BlendIndex = append(mesh.BlendIndex, bi...)
		bw := make([]float32, 4)
		if _, err := binary.Decode(data[v+28:v+44], binary.LittleEndian, &bw); err == nil {
			mesh.BlendWeight = append(mesh.BlendWeight, bw...)
		} else {
			mesh.BlendWeight = append(mesh.BlendWeight, 1, 0, 0, 0)
		}

		uv := struct{ U, V float32 }{}
		if _, err := binary.Decode(data[v+weMdlUVOffset:v+weMdlUVOffset+8], binary.LittleEndian, &uv); err != nil {
			return nil, fmt.Errorf("顶点 %d UV 解码失败: %w", i, err)
		}
		mesh.Uvs = append(mesh.Uvs, uv.U, uv.V)
	}
	for i := range indexCount {
		value := binary.LittleEndian.Uint16(data[indicesOffset+i*2:])

		if int(value) >= vertexCount {
			return nil, fmt.Errorf("网格索引越界：%d ≥ %d", value, vertexCount)
		}
		mesh.Indices = append(mesh.Indices, value)
	}

	return mesh, nil
}

// parseWeMdlBones 解析 MDLS 骨骼表：magic(9) + u32 块长 + u32 骨骼数(@13)
// + N 条记录（tmp u8 + type u32 + parent i32 + 矩阵长 u32 + 4x4 f32 + 名字 zstr）。
func parseWeMdlBones(block []byte) ([]weMdlBone, error) {
	if len(block) < 17 {
		return nil, fmt.Errorf("MDLS 块过短：%d", len(block))
	}
	// 用块声明的长度截断传入切片：调用方给的是"MDLS 起点 → 文件尾"，
	// 不截断时畸形文件会把后续 MDLA 区字节当骨骼记录吃进去，产出
	// 垃圾骨骼名/矩阵（静默错误数据）；截断后越界会显式报错。
	declaredLength := int(binary.LittleEndian.Uint32(block[9:13]))
	if declaredLength >= 17 && declaredLength < len(block) {
		block = block[:declaredLength]
	}
	boneCount := binary.LittleEndian.Uint32(block[13:17])
	if boneCount == 0 || boneCount > 4096 {
		return nil, fmt.Errorf("骨骼数异常：%d", boneCount)
	}

	bones := make([]weMdlBone, 0, boneCount)
	p := 17
	for i := range int(boneCount) {
		if p+13 > len(block) {
			return nil, fmt.Errorf("骨骼 %d 记录越界", i)
		}
		parent := int(int32(binary.LittleEndian.Uint32(block[p+5 : p+9])))
		matrixBytes := binary.LittleEndian.Uint32(block[p+9 : p+13])

		if matrixBytes != 64 || p+13+int(matrixBytes) > len(block) {
			return nil, fmt.Errorf("骨骼 %d rest 矩阵长度异常：%d", i, matrixBytes)
		}
		bone := weMdlBone{Parent: parent}
		if _, err := binary.Decode(block[p+13:p+13+64], binary.LittleEndian, &bone.Rest); err != nil {
			return nil, fmt.Errorf("骨骼 %d rest 矩阵解码失败: %w", i, err)
		}

		nameStart := p + 13 + 64
		nameEnd := bytes.IndexByte(block[nameStart:], 0)
		if nameEnd < 0 {
			nameEnd = len(block) - nameStart
		}
		bone.Name = string(block[nameStart : nameStart+nameEnd])
		bones = append(bones, bone)
		p = nameStart + nameEnd + 1
	}

	return bones, nil
}

// parseWeMdlAnimation 解析 MDLA 动画块；结构不符（旧版/无动画）返回 nil。
// 头部字段经真机样本校验（见测试），轨道按 (u32 长度 + 36 字节/帧) 循环推进。
func parseWeMdlAnimation(block []byte) *weMdlAnimation {
	if len(block) < 60 {
		return nil
	}
	// 头：magic 9 + totalSize 4 + animCount 4 + frameSamples 4 + reserved 4
	animCount := binary.LittleEndian.Uint32(block[13:17])

	if animCount == 0 {
		return nil
	}
	p := 25
	readZString := func() string {
		end := bytes.IndexByte(block[p:], 0)
		if end < 0 {
			return ""
		}
		value := string(block[p : p+end])
		p += end + 1
		return value
	}

	animation := &weMdlAnimation{Name: readZString()}
	// name 与 mode 之间观察到一个空 zstr（两样本一致），mode 后直接是时长。
	// 版本间头部细节可能再变，这里不逐字段硬编码：从 name 之后扫描定位
	// duration —— 特征是 (0,3600] 的 f32 且紧跟 [24,240] 的帧率 u32，
	// 其后 8 字节保留 + 轨道数 u32 + 首轨道长度 u32（36 的倍数）逐项校验。
	scanStart := p
	found := false
	for i := scanStart; i+24 <= len(block) && i < scanStart+128; i++ {
		duration := math.Float32frombits(binary.LittleEndian.Uint32(block[i:]))
		if duration <= 0.01 || duration > 3600 {
			continue
		}
		fps := binary.LittleEndian.Uint32(block[i+4:])
		if fps < 24 || fps > 240 {
			continue
		}
		trackCount := binary.LittleEndian.Uint32(block[i+12:])
		if trackCount == 0 || trackCount > 4096 {
			continue
		}
		firstTrack := binary.LittleEndian.Uint32(block[i+20:])
		if firstTrack == 0 || firstTrack%36 != 0 || i+20+int(firstTrack) > len(block) {
			continue
		}
		animation.Duration = duration
		p = i + 12
		found = true
		break
	}
	if !found {
		return nil
	}
	animation.Loop = true
	trackCount := int(binary.LittleEndian.Uint32(block[p:]))
	p += 8 // 轨道数 u32 + 4 字节分隔，然后是首轨道长度

	if trackCount == 0 || trackCount > 4096 {
		return nil
	}
	for range trackCount {
		if p+4 > len(block) {
			break
		}
		trackBytes := int(binary.LittleEndian.Uint32(block[p:]))
		p += 4
		frameCount := trackBytes / 36

		if frameCount <= 0 || p+trackBytes > len(block) {
			break
		}
		track := make([]weMdlKeyframe, frameCount)
		for f := range frameCount {
			if _, err := binary.Decode(block[p+f*36:p+f*36+36], binary.LittleEndian, &track[f]); err != nil {
				break
			}
		}
		animation.Tracks = append(animation.Tracks, track)
		p += trackBytes + 4 // 轨道尾的 u32 分隔
	}

	if len(animation.Tracks) == 0 {
		return nil
	}
	return animation
}
