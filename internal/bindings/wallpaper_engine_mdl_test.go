package bindings

// ParseWeMdl 的真机样本测试：testdata 里的两个 .mdl 从本机 Wallpaper
// Engine 创意工坊提取（重剪裁角色壁纸的本体层 / 四骨骼轻量层）。
// 样本缺失（CI 无 WE）时跳过。

import (
	"os"
	"testing"
)

// TestParseWeMdlBody 重度 puppet 样本：网格/骨骼/动画完整性与运动断言。
func TestParseWeMdlBody(t *testing.T) {
	data, err := os.ReadFile("testdata/puppet_body.mdl")
	if err != nil {
		t.Skipf("本机无样本: %v", err)
	}
	puppet, err := ParseWeMdl(data)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(puppet.Mesh.Positions)/3 == 0 || len(puppet.Mesh.Indices)/3 == 0 {
		t.Fatal("网格为空")
	}
	if len(puppet.Bones) != 4 {
		t.Fatalf("期望 4 骨骼，得到 %d", len(puppet.Bones))
	}
	for i, bone := range puppet.Bones {
		// rest 平移必须是合理的设计坐标（千像素级，非矩阵噪声）
		if bone.Rest[15] != 1 {
			t.Fatalf("骨骼 %d rest 矩阵尾值异常: %v", i, bone.Rest[15])
		}
	}
	animation := puppet.Animation
	if animation == nil {
		t.Fatal("样本应带 MDLA 动画")
	}
	if animation.Name != "动画 1" || !animation.Loop || animation.Duration != 30 {
		t.Fatalf("动画头不符: %+v", animation)
	}
	if len(animation.Tracks) != 4 || len(animation.Tracks[0]) != 61 {
		t.Fatalf("轨道结构不符: tracks=%d frames=%d",
			len(animation.Tracks), len(animation.Tracks[0]))
	}
	// track[2] 在真机上实测有 ~4.6px 的水平漂移：动画数据确实在动
	minX, maxX := animation.Tracks[2][0].Pos[0], animation.Tracks[2][0].Pos[0]
	for _, frame := range animation.Tracks[2] {
		if frame.Pos[0] < minX {
			minX = frame.Pos[0]
		}
		if frame.Pos[0] > maxX {
			maxX = frame.Pos[0]
		}
	}
	if maxX-minX < 0.1 {
		t.Fatalf("track[2] 应有运动，实测静止: [%.2f..%.2f]", minX, maxX)
	}
}

// TestParseWeMdlSimple 四骨骼轻量样本：头部字段精确断言。
func TestParseWeMdlSimple(t *testing.T) {
	data, err := os.ReadFile("testdata/puppet_simple.mdl")
	if err != nil {
		t.Skipf("本机无样本: %v", err)
	}
	puppet, err := ParseWeMdl(data)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(puppet.Bones) != 4 {
		t.Fatalf("期望 4 骨骼，得到 %d", len(puppet.Bones))
	}
	// 首骨骼 rest 平移经十六进制手工逆向核对（-1636, -1737.434；
	// f32 存储有精度尾差，用 1e-3 容差）
	if diff := puppet.Bones[0].Rest[12] - (-1636.0); diff < -0.001 || diff > 0.001 {
		t.Fatalf("rest 平移 x 不符: %v", puppet.Bones[0].Rest[12])
	}
	if diff := puppet.Bones[0].Rest[13] - (-1737.434); diff < -0.001 || diff > 0.001 {
		t.Fatalf("rest 平移 y 不符: %v", puppet.Bones[0].Rest[13])
	}
	animation := puppet.Animation
	if animation == nil || animation.Duration != 7.5 || len(animation.Tracks) != 4 {
		t.Fatalf("动画头不符: %+v", animation)
	}
	if len(animation.Tracks[0]) != 61 {
		t.Fatalf("期望 61 关键帧，得到 %d", len(animation.Tracks[0]))
	}
	// 首帧位置必须与骨骼 rest 平移一致（静止动画，f32 容差 1e-3）
	frame := animation.Tracks[0][0]
	if diff := frame.Pos[0] - (-1636.0); diff < -0.001 || diff > 0.001 {
		t.Fatalf("轨道首帧 x 与 rest 不一致: %v", frame.Pos[0])
	}
	if diff := frame.Pos[1] - (-1737.434); diff < -0.001 || diff > 0.001 {
		t.Fatalf("轨道首帧 y 与 rest 不一致: %v", frame.Pos[1])
	}
}

// TestParseWeMdlRejects 非法输入直接报错不 panic。
func TestParseWeMdlRejects(t *testing.T) {
	if _, err := ParseWeMdl([]byte("not an mdl")); err == nil {
		t.Fatal("垃圾输入应报错")
	}
	if _, err := ParseWeMdl(nil); err == nil {
		t.Fatal("空输入应报错")
	}
}
