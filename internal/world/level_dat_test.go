package world

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// nbtName 编码一个 NBT 字符串（u16 长度 + UTF-8 字节）。
func nbtName(s string) []byte {
	out := make([]byte, 2+len(s))
	out[0] = byte(len(s) >> 8)
	out[1] = byte(len(s))
	copy(out[2:], s)

	return out
}

// nbtI32 / nbtI64 编码大端整数载荷。
func nbtI32(v int32) []byte {
	return []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
}

func nbtI64(v int64) []byte {
	return []byte{
		byte(v >> 56), byte(v >> 48), byte(v >> 40), byte(v >> 32),
		byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v),
	}
}

// buildLevelDat 造一份最小但结构完整的 level.dat（gzip + NBT）：
// 根 → Data（含若干噪音字段 + LastPlayed）→ 嵌套复合里埋同名的 LastPlayed 诱饵，
// 验证解析只认 Data 的直接子级。lastPlayed 传 0 表示不写该字段。
func buildLevelDat(t *testing.T, lastPlayed int64) []byte {
	t.Helper()

	var raw bytes.Buffer
	gz := gzip.NewWriter(&raw)

	// 根复合（根名为空串）
	raw2 := bytes.NewBuffer(nil)
	raw2.WriteByte(10)
	raw2.Write(nbtName(""))

	// Data 复合：噪音字段 + LastPlayed
	raw2.WriteByte(10)
	raw2.Write(nbtName("Data"))
	raw2.WriteByte(8) // TAG_String
	raw2.Write(nbtName("LevelName"))
	raw2.Write(nbtName("测试世界"))
	raw2.WriteByte(7) // TAG_ByteArray
	raw2.Write(nbtName("DataVersionBlob"))
	raw2.Write(nbtI32(4))
	raw2.Write([]byte{0xDE, 0xAD, 0xBE, 0xEF})
	raw2.WriteByte(9) // TAG_List（TAG_Int × 2）
	raw2.Write(nbtName("SpawnList"))
	raw2.WriteByte(3)
	raw2.Write(nbtI32(2))
	raw2.Write(nbtI32(16))
	raw2.Write(nbtI32(-3))
	if lastPlayed != 0 {
		raw2.WriteByte(4) // TAG_Long
		raw2.Write(nbtName("LastPlayed"))
		raw2.Write(nbtI64(lastPlayed))
	}
	// Data 的子复合：诱饵同名字段必须被整体跳过
	raw2.WriteByte(10)
	raw2.Write(nbtName("WorldGenSettings"))
	raw2.WriteByte(10)
	raw2.Write(nbtName("dimensions"))
	raw2.WriteByte(4)
	raw2.Write(nbtName("LastPlayed"))
	raw2.Write(nbtI64(111))
	raw2.WriteByte(0) // end dimensions
	raw2.WriteByte(0) // end WorldGenSettings
	raw2.WriteByte(0) // end Data
	raw2.WriteByte(0) // end root

	gz.Write(raw2.Bytes())
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip 收尾失败：%v", err)
	}

	return raw.Bytes()
}

// TestReadLevelDatLastPlayed 解析正常 level.dat、跳过嵌套诱饵、拒绝损坏输入。
func TestReadLevelDatLastPlayed(t *testing.T) {
	want := time.UnixMilli(1727654321000)
	got, ok := readLevelDatLastPlayedFrom(t, buildLevelDat(t, want.UnixMilli()))
	if !ok || !got.Equal(want) {
		t.Fatalf("应读到 %v，得到 %v / %v", want, got, ok)
	}

	// 没有 LastPlayed 字段 → 读不到
	if _, ok := readLevelDatLastPlayedFrom(t, buildLevelDat(t, 0)); ok {
		t.Fatal("缺 LastPlayed 字段时应返回 false")
	}

	// 损坏输入：非 gzip、截断、空数据
	for name, data := range map[string][]byte{
		"非gzip": []byte("level"),
		"截断":    buildLevelDat(t, 42)[:10],
		"空":     {},
	} {
		if _, ok := readLevelDatLastPlayedFrom(t, data); ok {
			t.Fatalf("损坏输入 %q 应返回 false", name)
		}
	}
}

// readLevelDatLastPlayedFrom 直接从字节解析（测试注入用）：写进临时文件后
// 走文件版入口，顺带覆盖 os.ReadFile / gzip 文件流路径。
func readLevelDatLastPlayedFrom(t *testing.T, raw []byte) (time.Time, bool) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "level.dat")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("写临时 level.dat 失败：%v", err)
	}

	return readLevelDatLastPlayed(path)
}

// TestGetRecentWorldsPrefersNBTLastPlayed 端到端：mtime 很旧、NBT LastPlayed
// 很新的世界，应按 NBT 时间参与排序（防"复制存档后排序乱跳"回归）。
func TestGetRecentWorldsPrefersNBTLastPlayed(t *testing.T) {
	useTempStorage(t)
	gameDirectory := t.TempDir()
	saves := writeSavesDirectory(t, gameDirectory)

	nbtTime := time.Now().Add(-time.Minute)
	oldTime := time.Now().Add(-30 * 24 * time.Hour)

	// nbtWorld：mtime 拨到 30 天前，但 NBT 里 LastPlayed 是 1 分钟前
	nbtWorld := writeWorld(t, saves, "nbtWorld", false)
	if err := os.WriteFile(filepath.Join(nbtWorld, "level.dat"), buildLevelDat(t, nbtTime.UnixMilli()), 0o644); err != nil {
		t.Fatalf("写 level.dat 失败：%v", err)
	}
	setModTime(t, filepath.Join(nbtWorld, "level.dat"), oldTime)

	// mtimeWorld：level.dat 是无效内容（NBT 读不到 → 回落 mtime），拨到 29 天前
	mtimeWorld := writeWorld(t, saves, "mtimeWorld", true)
	setModTime(t, filepath.Join(mtimeWorld, "level.dat"), oldTime.Add(24*time.Hour))

	worlds := GetRecentWorlds(sharedSnapshot(gameDirectory, "1.20.1"), 10)
	if len(worlds) != 2 {
		t.Fatalf("应扫到 2 个世界，得到 %d", len(worlds))
	}
	if worlds[0].Name != "nbtWorld" {
		t.Fatalf("NBT LastPlayed 应优先于 mtime：%v", worldNames(worlds))
	}
	// NBT 只存毫秒精度：期望值同样经 UnixMilli 往返后再比较
	if !worlds[0].LastPlayed.Equal(time.UnixMilli(nbtTime.UnixMilli())) {
		t.Fatalf("LastPlayed 应取 NBT 值 %v，得到 %v", nbtTime, worlds[0].LastPlayed)
	}
}
