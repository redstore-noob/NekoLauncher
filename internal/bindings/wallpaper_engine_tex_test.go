package bindings

// .tex 解码与 LZ4 块解压的单元测试。
// 测试数据全部手工构造,不依赖任何安装了 Wallpaper Engine 的环境;
// 真实包的端到端校验见 TestDecodeRealScenePackages(按环境变量开关)。

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// ---- LZ4 块解压 ----

func TestLZ4AllLiterals(t *testing.T) {
	// 纯字面量块:token=0x60(6 字面量,低 4 位无意义)+ 6 字节,输入恰好耗尽
	src := []byte{0x60, 'a', 'b', 'c', 'd', 'e', 'f'}
	out, err := lz4UncompressBlock(src, 6)
	if err != nil {
		t.Fatalf("解压失败:%v", err)
	}
	if string(out) != "abcdef" {
		t.Fatalf("结果 %q != %q", out, "abcdef")
	}
}

func TestLZ4MatchCopy(t *testing.T) {
	// 3 字面量 "abc" + 匹配(offset=3, len=4) → "abca" → "abcabca",再尾字面量 "d"
	src := []byte{0x30, 'a', 'b', 'c', 0x03, 0x00, 0x10, 'd'}
	out, err := lz4UncompressBlock(src, 8)
	if err != nil {
		t.Fatalf("解压失败:%v", err)
	}
	if string(out) != "abcabcad" {
		t.Fatalf("结果 %q != %q", out, "abcabcad")
	}
}

func TestLZ4OverlappingRLE(t *testing.T) {
	// 1 字面量 x + offset=1 的 20 字节匹配(长度扩展:15 + 1)→ 21 个 x
	src := []byte{0x1F, 'x', 0x01, 0x00, 0x01}
	out, err := lz4UncompressBlock(src, 21)
	if err != nil {
		t.Fatalf("解压失败:%v", err)
	}
	if len(out) != 21 || bytes.Count(out, []byte{'x'}) != 21 {
		t.Fatalf("RLE 结果异常:%q", out)
	}
}

func TestLZ4LiteralLengthExtension(t *testing.T) {
	// 字面量长度 15+255+1 = 271:两级扩展
	literals := bytes.Repeat([]byte{'z'}, 271)
	src := append([]byte{0xFF, 0xFF, 0x01}, literals...)
	// LZ4 块没有显式结束符,输入在字面量后耗尽即结束
	out, err := lz4UncompressBlock(src, 271)
	if err != nil {
		t.Fatalf("解压失败:%v", err)
	}
	if len(out) != 271 {
		t.Fatalf("长度 %d != 271", len(out))
	}
}

func TestLZ4RejectsTruncated(t *testing.T) {
	if _, err := lz4UncompressBlock([]byte{0x40, 'a'}, 4); err == nil {
		t.Fatal("截断输入应当报错")
	}
	if _, err := lz4UncompressBlock([]byte{0x30, 'a', 'b', 'c', 0x00, 0x00}, 7); err == nil {
		t.Fatal("零偏移应当报错")
	}
}

// ---- DXT 解块 ----

func makeBC1Block(color0, color1 uint16, indices uint32) []byte {
	block := make([]byte, 8)
	binary.LittleEndian.PutUint16(block[0:2], color0)
	binary.LittleEndian.PutUint16(block[2:4], color1)
	binary.LittleEndian.PutUint32(block[4:8], indices)
	return block
}

func TestDXT1FourColor(t *testing.T) {
	// 红(0xF800) > 绿(0x07E0):四色模式;索引全 0 → 全红不透明
	out, err := decodeDXT(makeBC1Block(0xF800, 0x07E0, 0), 4, 4, weTexFormatDXT1)
	if err != nil {
		t.Fatalf("解码失败:%v", err)
	}
	for i := 0; i < 16; i++ {
		r, g, b, a := out.At(i%4, i/4).RGBA()
		if r>>8 != 0xFF || g != 0 || b != 0 || a>>8 != 0xFF {
			t.Fatalf("像素 %d 异常:(%d,%d,%d,%d)", i, r>>8, g>>8, b>>8, a>>8)
		}
	}
}

func TestDXT1PunchthroughAlpha(t *testing.T) {
	// color0 <= color1 → 三色模式;索引全 3 = 透明黑
	out, err := decodeDXT(makeBC1Block(0x0000, 0xFFFF, 0xFFFFFFFF), 4, 4, weTexFormatDXT1)
	if err != nil {
		t.Fatalf("解码失败:%v", err)
	}
	_, _, _, a := out.At(0, 0).RGBA()
	if a != 0 {
		t.Fatalf("索引 3 应为透明,实际 alpha=%d", a>>8)
	}
}

func TestDXT5AlphaEndpoints(t *testing.T) {
	// alpha 端点 255/0,索引全 0 → alpha=255;全 1 → alpha=0
	block := makeBC1Block(0xF800, 0x07E0, 0)
	dxt5 := append([]byte{0xFF, 0x00, 0, 0, 0, 0, 0, 0}, block...)
	out, err := decodeDXT(dxt5, 4, 4, weTexFormatDXT5)
	if err != nil {
		t.Fatalf("解码失败:%v", err)
	}
	if _, _, _, a := out.At(0, 0).RGBA(); a>>8 != 0xFF {
		t.Fatalf("alpha 应为 255")
	}
	// 索引全 1(3bit×16 = 48bit 全 1)
	indices := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	dxt5b := append(append([]byte{0xFF, 0x00}, indices...), block...)
	out2, err := decodeDXT(dxt5b, 4, 4, weTexFormatDXT5)
	if err != nil {
		t.Fatalf("解码失败:%v", err)
	}
	if _, _, _, a := out2.At(0, 0).RGBA(); a != 0 {
		t.Fatalf("alpha 应为 0")
	}
}

func TestCropRGBARowStride(t *testing.T) {
	// 纹理宽 8(填充),实际图像 6×2:验证按行距裁剪
	src := image.NewNRGBA(image.Rect(0, 0, 8, 2))
	for x := 0; x < 8; x++ {
		src.SetNRGBA(x, 0, color.NRGBA{R: uint8(x), A: 255})
	}
	cropped := cropRGBA(src, 8, 6, 2)
	if cropped.Bounds().Dx() != 6 || cropped.Bounds().Dy() != 2 {
		t.Fatalf("裁剪尺寸异常:%v", cropped.Bounds())
	}
	if r, _, _, _ := cropped.At(5, 0).RGBA(); r>>8 != 5 {
		t.Fatalf("行距裁剪取错列")
	}
}

// ---- .tex 容器解析 ----

func nstring(s string) []byte { return append([]byte(s), 0) }
func i32(v int) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, uint32(int32(v)))
	return b
}
func concat(parts ...[]byte) []byte { return bytes.Join(parts, nil) }

// buildTex 构造一个 TEXB0002 裸像素 .tex。
func buildTex(format, texW, texH, imgW, imgH int, payload []byte) []byte {
	return concat(
		nstring("TEXV0005"),
		nstring("TEXI0001"),
		i32(format), i32(0), i32(texW), i32(texH), i32(imgW), i32(imgH), i32(0),
		nstring("TEXB0002"),
		i32(1), // imageCount
		i32(1), // mipmapCount
		i32(texW), i32(texH),
		i32(0),            // isLZ4
		i32(len(payload)), // decompressedCount
		i32(len(payload)), // byteCount
		payload,
	)
}

func TestDecodeTexRGBA(t *testing.T) {
	// 2×2 RGBA,含半透明像素
	px := []byte{255, 0, 0, 255, 0, 255, 0, 128, 0, 0, 255, 255, 255, 255, 255, 0}
	decoded, err := weDecodeTex(buildTex(weTexFormatRGBA8888, 2, 2, 2, 2, px))
	if err != nil {
		t.Fatalf("解码失败:%v", err)
	}
	if decoded.Mime != "image/png" || decoded.Width != 2 || decoded.Height != 2 {
		t.Fatalf("元数据异常:%+v", decoded)
	}
	img, err := png.Decode(bytes.NewReader(decoded.ImageData))
	if err != nil {
		t.Fatalf("PNG 无法解码:%v", err)
	}
	if _, _, _, a := img.At(1, 0).RGBA(); a>>8 != 128 {
		t.Fatalf("半透明像素丢失:alpha=%d", a>>8)
	}
}

func TestDecodeTexPaddedRows(t *testing.T) {
	// 纹理 8 宽填充、实际 6×2:校验解码后裁剪正确
	px := make([]byte, 8*2*4)
	for row := 0; row < 2; row++ {
		for col := 0; col < 8; col++ {
			px[(row*8+col)*4] = uint8(col)
			px[(row*8+col)*4+3] = 255
		}
	}
	decoded, err := weDecodeTex(buildTex(weTexFormatRGBA8888, 8, 2, 6, 2, px))
	if err != nil {
		t.Fatalf("解码失败:%v", err)
	}
	img, err := png.Decode(bytes.NewReader(decoded.ImageData))
	if err != nil {
		t.Fatalf("PNG 无法解码:%v", err)
	}
	if img.Bounds().Dx() != 6 {
		t.Fatalf("宽度 %d != 6", img.Bounds().Dx())
	}
	if r, _, _, _ := img.At(5, 0).RGBA(); r>>8 != 5 {
		t.Fatalf("填充行距裁剪取错列")
	}
}

func TestDecodeTexEmbeddedPNG(t *testing.T) {
	// FIF=PNG 的 TEXB0003:体数据直接透传
	var pngBuffer bytes.Buffer
	small := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	if err := png.Encode(&pngBuffer, small); err != nil {
		t.Fatal(err)
	}
	payload := pngBuffer.Bytes()
	tex := concat(
		nstring("TEXV0005"),
		nstring("TEXI0001"),
		i32(weTexFormatRGBA8888), i32(0), i32(3), i32(2), i32(3), i32(2), i32(0),
		nstring("TEXB0003"),
		i32(1),         // imageCount
		i32(weFIFPNG),  // fif
		i32(1),         // mipmapCount
		i32(3), i32(2), // w h
		i32(0),            // isLZ4
		i32(len(payload)), // decompressedCount
		i32(len(payload)), // byteCount
		payload,
	)
	decoded, err := weDecodeTex(tex)
	if err != nil {
		t.Fatalf("解码失败:%v", err)
	}
	if decoded.Mime != "image/png" || !bytes.Equal(decoded.ImageData, payload) {
		t.Fatalf("内嵌 PNG 未透传:%s", decoded.Mime)
	}
}

func TestDecodeTexRejectsGarbage(t *testing.T) {
	if _, err := weDecodeTex([]byte("not a tex at all")); err == nil {
		t.Fatal("垃圾输入应当报错")
	}
}

// ---- 真实壁纸包端到端(可选) ----

// weTestPkgRoot 返回环境变量 WE_TEST_PKG_DIR 指定的真实创意工坊目录;
// 未设置时为空串,相关测试自动跳过。
func weTestPkgRoot(t *testing.T) string {
	t.Helper()
	return os.Getenv("WE_TEST_PKG_DIR")
}

// TestDecodeRealScenePackages 在设置了 WE_TEST_PKG_DIR(指向创意工坊 431960 目录)
// 时,把目录下所有 scene.pkg 的全部 .tex 解一遍,确保真实世界输入不炸。
// 本机开发时用它做回归;CI 没装 WE,自动跳过。
func TestDecodeRealScenePackages(t *testing.T) {
	root := weTestPkgRoot(t)
	if root == "" {
		t.Skip("未设置 WE_TEST_PKG_DIR,跳过真实包回归")
	}
	dirEntries, err := os.ReadDir(root)
	if err != nil || len(dirEntries) == 0 {
		t.Skip("没有可用的真实壁纸包")
	}
	decoded, failed, pngOut, jpegOut, videoOut := 0, 0, 0, 0, 0
	for _, dirEntry := range dirEntries {
		pkgPath := filepath.Join(root, dirEntry.Name(), "scene.pkg")
		reader, err := wePkgOpen(pkgPath)
		if err != nil {
			continue // 非 scene 包(视频/网页壁纸目录)
		}
		for _, entry := range reader.entries {
			if entry.Length == 0 || !bytes.HasSuffix([]byte(entry.Path), []byte(".tex")) {
				continue
			}
			payload, err := reader.readEntry(&entry)
			if err != nil {
				continue
			}
			decoded++
			image, err := weDecodeTex(payload)
			if err != nil {
				failed++
				t.Logf("解码失败 %s!%s:%v", dirEntry.Name(), entry.Path, err)
				continue
			}
			switch image.Mime {
			case "image/png":
				pngOut++
			case "image/jpeg":
				jpegOut++
			case "video/mp4":
				videoOut++
			}
			if image.Mime == "image/png" {
				if _, err := png.Decode(bytes.NewReader(image.ImageData)); err != nil {
					t.Errorf("产出非法 PNG %s!%s:%v", dirEntry.Name(), entry.Path, err)
				}
			}
		}
	}
	t.Logf("真实包统计:tex=%d 失败=%d png=%d jpeg=%d mp4=%d", decoded, failed, pngOut, jpegOut, videoOut)
	if decoded > 0 && failed > decoded/10 {
		t.Errorf("失败率过高:%d/%d", failed, decoded)
	}
}

func TestDXTInterpolatedColors(t *testing.T) {
	// 回归:插值色必须走 int 运算。曾经 byte 回绕把 2/3-1/3 插值洗成垃圾色,
	// 端点色(索引 0/1)不受影响,所以只有断言插值项才能抓住这个 bug。
	// 构造一个块:c0=白(0xFFFF) c1=暗红(0x8C1F?)——用会触发进位的端点:
	// 白(255,255,255) 与 蓝紫(0x7B1F → 121,31,242)插值
	c0 := uint16(0xFFFF)
	c1 := uint16(0x7B1F)
	block := makeBC1Block(c0, c1, 0)
	out, err := decodeDXT(block, 4, 4, weTexFormatDXT1)
	if err != nil {
		t.Fatal(err)
	}
	r0, g0, b0, _ := out.At(0, 0).RGBA()
	if uint8(r0>>8) != 255 || uint8(g0>>8) != 255 || uint8(b0>>8) != 255 {
		t.Fatalf("端点 0 应为白色,实际 %d,%d,%d", r0>>8, g0>>8, b0>>8)
	}
	// 索引 2 的期望值从 decode565 端点推导,避免手算 565 展开出错
	indices := uint32(0xAAAAAAAA) // 全部像素取索引 2(0b10)
	out2, err := decodeDXT(makeBC1Block(c0, c1, indices), 4, 4, weTexFormatDXT1)
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := out2.At(0, 0).RGBA()
	e0r, e0g, e0b := decode565(c0)
	e1r, e1g, e1b := decode565(c1)
	want := [3]byte{byte((2*int(e0r) + int(e1r)) / 3), byte((2*int(e0g) + int(e1g)) / 3), byte((2*int(e0b) + int(e1b)) / 3)}
	if uint8(r>>8) != want[0] || uint8(g>>8) != want[1] || uint8(b>>8) != want[2] {
		t.Fatalf("插值色错误: got %d,%d,%d want %d,%d,%d", r>>8, g>>8, b>>8, want[0], want[1], want[2])
	}
}

func TestDXT3AlphaLowNibble(t *testing.T) {
	// 回归:DXT3 偶数像素取高 nibble、奇数像素取低 nibble(<<4 归一到 8bit)。
	// 曾把低 nibble 写成 & 0xF0(等于重复高 nibble)。
	block := make([]byte, 8)
	block[0] = 0xF0 // 像素 0 = 0xF(240),像素 1 = 0x0(0)
	block[1] = 0x0F // 像素 2 = 0x0,像素 3 = 0xF(240)
	var alpha [16]byte
	decodeDXT3Alpha(block, &alpha)
	if alpha[0] != 240 || alpha[1] != 0 || alpha[2] != 0 || alpha[3] != 240 {
		t.Fatalf("DXT3 alpha nibble 顺序错误:%v", alpha[:4])
	}
}
