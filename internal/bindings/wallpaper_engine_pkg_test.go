package bindings

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pkgFixtureEntry 测试用的 pkg 条目：路径 + 数据（nil 表示目录）。
type pkgFixtureEntry struct {
	Path     string
	Data     []byte
	Children []pkgFixtureEntry
}

// encodePNG 生成指定尺寸的 PNG，内容不重要，只用于解码尺寸与选图逻辑。
func encodePNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, fixtureImage(width, height)); err != nil {
		t.Fatalf("编码 PNG 失败：%v", err)
	}
	return buffer.Bytes()
}

// mustEncodeJPEG 生成指定尺寸的 JPEG。
func mustEncodeJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, fixtureImage(width, height), nil); err != nil {
		t.Fatalf("编码 JPEG 失败：%v", err)
	}
	return buffer.Bytes()
}

// padEntry 在图片后补足填充字节，让条目长度越过最小条目阈值；真实的 .tex 条目同样
// 带有自己的头部与对齐填充，尾部多出的字节能否被正确剔除由截断测试单独覆盖。
func padEntry(data []byte) []byte {
	if len(data) > wePkgMinEntryBytes {
		return data
	}
	return append(data, make([]byte, wePkgMinEntryBytes+1-len(data))...)
}

// fixtureImage 生成一张纯色测试图。
func fixtureImage(width, height int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	img.Set(0, 0, color.RGBA{R: 0xFF, A: 0xFF})
	return img
}

func writePkgString(buffer *bytes.Buffer, value string) {
	binary.Write(buffer, binary.LittleEndian, int32(len(value)))
	buffer.WriteString(value)
}

// buildPkg 按 PKGV 布局拼出一个 pkg：magic + 条目总数 + 条目表 + 数据区。
// 条目偏移为**相对数据区**的偏移（真实包即如此，见 wallpaper_engine_pkg.go 的
// 格式说明），夹具先以 0 偏移排版量出表长，再回填真实偏移重排一次。
func buildPkg(t *testing.T, entries []pkgFixtureEntry) []byte {
	t.Helper()

	var count int32
	var flatten func(items []pkgFixtureEntry)
	flatten = func(items []pkgFixtureEntry) {
		for _, item := range items {
			count++
			flatten(item.Children)
		}
	}
	flatten(entries)

	serialize := func(offsets map[string]int64) []byte {
		var buffer bytes.Buffer
		writePkgString(&buffer, "PKGV0022")
		binary.Write(&buffer, binary.LittleEndian, count)
		var emit func(items []pkgFixtureEntry, prefix string)
		emit = func(items []pkgFixtureEntry, prefix string) {
			for _, item := range items {
				writePkgString(&buffer, prefix+item.Path)
				binary.Write(&buffer, binary.LittleEndian, int32(offsets[prefix+item.Path]))
				binary.Write(&buffer, binary.LittleEndian, int32(len(item.Data)))
				if item.Data == nil {
					binary.Write(&buffer, binary.LittleEndian, int32(len(item.Children)))
					emit(item.Children, prefix+item.Path+"/")
				}
			}
		}
		emit(entries, "")
		return buffer.Bytes()
	}

	toc := serialize(map[string]int64{})
	offsets := make(map[string]int64)
	dataOffset := int64(0) // 相对数据区，从 0 起算
	var assign func(items []pkgFixtureEntry, prefix string)
	assign = func(items []pkgFixtureEntry, prefix string) {
		for _, item := range items {
			full := prefix + item.Path
			if item.Data != nil {
				offsets[full] = dataOffset
				dataOffset += int64(len(item.Data))
			} else {
				assign(item.Children, full+"/")
			}
		}
	}
	assign(entries, "")

	toc = serialize(offsets)
	if int64(len(toc)) != int64(len(serialize(map[string]int64{}))) {
		t.Fatalf("条目表长度随偏移变化，夹具构造有误")
	}

	var out bytes.Buffer
	out.Write(toc)
	var emitData func(items []pkgFixtureEntry, prefix string)
	emitData = func(items []pkgFixtureEntry, prefix string) {
		for _, item := range items {
			full := prefix + item.Path
			if item.Data != nil {
				out.Write(item.Data)
			} else {
				emitData(item.Children, full+"/")
			}
		}
	}
	emitData(entries, "")
	return out.Bytes()
}

// TestReadWEPkgTOCParsesEntries 目录表应解析出全部条目（含目录下的子条目）。
func TestReadWEPkgTOCParsesEntries(t *testing.T) {
	pkg := buildPkg(t, []pkgFixtureEntry{
		{Path: "materials", Children: []pkgFixtureEntry{
			{Path: "art.tex", Data: encodePNG(t, 8, 8)},
			{Path: "masks", Children: []pkgFixtureEntry{
				{Path: "mask.tex", Data: encodePNG(t, 4, 4)},
			}},
		}},
		{Path: "preview.jpg", Data: encodePNG(t, 2, 2)},
	})

	entries, tocEnd, err := readWEPkgTOC(pkg)
	if err != nil {
		t.Fatalf("解析目录表失败：%v", err)
	}
	if tocEnd <= 0 || tocEnd > int64(len(pkg)) {
		t.Fatalf("目录表结束位置异常：%d", tocEnd)
	}

	paths := make(map[string]bool, len(entries))
	for _, entry := range entries {
		paths[entry.Path] = true
	}
	for _, want := range []string{"materials/art.tex", "materials/masks/mask.tex", "preview.jpg"} {
		if !paths[want] {
			t.Errorf("目录表缺少条目 %q，实际解析到 %v", want, paths)
		}
	}
}

// TestReadWEPkgTOCRejectsNonPkg 非 pkg 数据应报错而非返回垃圾条目。
func TestReadWEPkgTOCRejectsNonPkg(t *testing.T) {
	if _, _, err := readWEPkgTOC([]byte("not a wallpaper package at all")); err == nil {
		t.Fatal("非 pkg 数据应返回错误")
	}
}

// TestExtractWEPkgWallpaperImagePrefersLargestArtwork 选图应跳过 mask 类辅助贴图，
// 取像素面积最大的画面贴图。
func TestExtractWEPkgWallpaperImagePrefersLargestArtwork(t *testing.T) {
	pkg := buildPkg(t, []pkgFixtureEntry{
		{Path: "materials/masks/huge_mask.tex", Data: padEntry(encodePNG(t, 64, 64))},
		{Path: "materials/small.tex", Data: padEntry(encodePNG(t, 8, 8))},
		{Path: "materials/art.tex", Data: padEntry(encodePNG(t, 48, 32))},
		{Path: "sounds/bgm.mp3", Data: bytes.Repeat([]byte{0x11}, wePkgMinEntryBytes+16)},
	})

	dir := t.TempDir()
	pkgPath := filepath.Join(dir, "scene.pkg")
	if err := os.WriteFile(pkgPath, pkg, 0o644); err != nil {
		t.Fatalf("写入夹具失败：%v", err)
	}

	result, err := extractWEPkgWallpaperImage(pkgPath)
	if err != nil {
		t.Fatalf("提取失败：%v", err)
	}
	if result == nil {
		t.Fatal("应提取到 art.tex 中的贴图，实际为 nil")
	}
	// 64×64 的 mask 若未被排除会胜出，这条断言同时覆盖辅助贴图过滤
	if result.Width != 48 || result.Height != 32 {
		t.Errorf("应选中 48×32 的画面贴图，实际 %d×%d", result.Width, result.Height)
	}
	if result.Ext != ".png" {
		t.Errorf("扩展名应为 .png，实际 %q", result.Ext)
	}
	if width, height, ok := imageDimensions(result.Data); !ok || width != 48 || height != 32 {
		t.Errorf("提取出的字节不是完整可解码图片：%dx%d ok=%v", width, height, ok)
	}
}

// TestExtractWEPkgWallpaperImagePicksLargestLayer 分层场景里各层面积相当。
// 完整场景渲染落地后,静态提取只作加载期/失败兜底,此时取面积最大的一层
// 比空屏更好——占优判定(wePkgDominanceRatio)已按此语义停用,这里锁定该行为:
// 永远返回最大的一层,而不是放弃提取。
func TestExtractWEPkgWallpaperImagePicksLargestLayer(t *testing.T) {
	pkg := buildPkg(t, []pkgFixtureEntry{
		{Path: "materials/角色_本体.tex", Data: padEntry(encodePNG(t, 100, 100))},
		{Path: "materials/角色_左翅膀.tex", Data: padEntry(encodePNG(t, 98, 100))},
		{Path: "materials/角色_右翅膀.tex", Data: padEntry(encodePNG(t, 96, 100))},
		{Path: "materials/角色_头发.tex", Data: padEntry(encodePNG(t, 94, 100))},
	})

	dir := t.TempDir()
	pkgPath := filepath.Join(dir, "scene.pkg")
	if err := os.WriteFile(pkgPath, pkg, 0o644); err != nil {
		t.Fatalf("写入夹具失败：%v", err)
	}

	result, err := extractWEPkgWallpaperImage(pkgPath)
	if err != nil {
		t.Fatalf("提取失败：%v", err)
	}
	if result == nil {
		t.Fatal("兜底语义下应返回面积最大的一层,实际为 nil")
	}
	if result.Width != 100 || result.Height != 100 {
		t.Errorf("应选中 100×100 的本体层,实际 %d×%d", result.Width, result.Height)
	}
}

// TestExtractWEPkgWallpaperImageAcceptsNamedBackdrop 最大贴图明确叫"背景"时，
// 即使面积占比不到倍数门槛也应采用——名字已经说明它覆盖整块画布。
func TestExtractWEPkgWallpaperImageAcceptsNamedBackdrop(t *testing.T) {
	pkg := buildPkg(t, []pkgFixtureEntry{
		{Path: "materials/背景.tex", Data: padEntry(encodePNG(t, 80, 60))},
		{Path: "materials/图层 0 拷贝.tex", Data: padEntry(encodePNG(t, 76, 60))},
	})

	dir := t.TempDir()
	pkgPath := filepath.Join(dir, "scene.pkg")
	if err := os.WriteFile(pkgPath, pkg, 0o644); err != nil {
		t.Fatalf("写入夹具失败：%v", err)
	}

	result, err := extractWEPkgWallpaperImage(pkgPath)
	if err != nil {
		t.Fatalf("提取失败：%v", err)
	}
	if result == nil {
		t.Fatal("命名底图应被采用，实际为 nil")
	}
	if result.Width != 80 || result.Height != 60 {
		t.Errorf("应选中 80×60 的底图，实际 %d×%d", result.Width, result.Height)
	}
}

// TestExtractWEPkgWallpaperImageWithoutUsableArtwork 包内只有辅助贴图时不应乱选。
func TestExtractWEPkgWallpaperImageWithoutUsableArtwork(t *testing.T) {
	pkg := buildPkg(t, []pkgFixtureEntry{
		{Path: "materials/masks/big_mask.tex", Data: padEntry(encodePNG(t, 256, 256))},
		{Path: "models/thing.mdl", Data: bytes.Repeat([]byte{0x22}, wePkgMinEntryBytes+16)},
	})

	dir := t.TempDir()
	pkgPath := filepath.Join(dir, "scene.pkg")
	if err := os.WriteFile(pkgPath, pkg, 0o644); err != nil {
		t.Fatalf("写入夹具失败：%v", err)
	}

	result, err := extractWEPkgWallpaperImage(pkgPath)
	if err != nil {
		t.Fatalf("提取失败：%v", err)
	}
	if result != nil {
		t.Errorf("只有辅助贴图时不应选出图片，实际 %d×%d", result.Width, result.Height)
	}
}

// TestWEPkgIsAuxiliaryPath 辅助贴图/目录判定。
func TestWEPkgIsAuxiliaryPath(t *testing.T) {
	auxiliary := []string{
		"materials/masks/shake_mask_18b01ad5.tex",
		"materials/character_normal.tex",
		"models/左睫毛_puppet.mdl",
		"sounds/5月2日.mp3",
		"materials/effects/waterflowphase.tex",
	}
	for _, path := range auxiliary {
		if !wePkgIsAuxiliaryPath(path) {
			t.Errorf("%q 应判为辅助贴图", path)
		}
	}
	artwork := []string{
		"materials/背景 原生.tex",
		"materials/能天使_0023_图层 2.tex",
		"materials/art.png",
	}
	for _, path := range artwork {
		if wePkgIsAuxiliaryPath(path) {
			t.Errorf("%q 是画面贴图，不应被排除", path)
		}
	}
}

// TestFindEmbeddedImageTrimsToImageEnd 提取出的字节应在图片结束标记处截断，
// 不带入条目里的无关尾部数据。
func TestFindEmbeddedImageTrimsToImageEnd(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		prefix    []byte
		imageData []byte
		suffix    []byte
	}{
		{"PNG", []byte("tex header"), encodePNG(t, 12, 7), []byte("trailing junk")},
		{"JPEG", []byte("tex header"), mustEncodeJPEG(t, 12, 7), []byte("trailing junk")},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			buffer := append(append(append([]byte{}, testCase.prefix...), testCase.imageData...), testCase.suffix...)
			found, ok := findEmbeddedImage(buffer)
			if !ok {
				t.Fatal("应找到内嵌图片")
			}
			if found.Width != 12 || found.Height != 7 {
				t.Errorf("尺寸应为 12×7，实际 %d×%d", found.Width, found.Height)
			}
			if len(found.Data) != len(testCase.imageData) {
				t.Errorf("应在图片结束标记处截断：期望 %d 字节，实际 %d 字节",
					len(testCase.imageData), len(found.Data))
			}
			if strings.Contains(string(found.Data), "trailing junk") {
				t.Error("提取结果混入了尾部数据")
			}
		})
	}
}
