package bindings

import (
	"bytes"
	"image/png"
	"fmt"
	"image"
	"path/filepath"
	"testing"
)

// 临时排障探针:解 3229648591 的光斑纹理,统计 alpha 分布与头部格式。
func TestProbeFlareTex(t *testing.T) {
	pkg := filepath.Join(`D:\SteamLibrary\steamapps\workshop\content\431960\3229648591`, "scene.pkg")
	reader, err := wePkgOpenCached(pkg)
	if err != nil {
		t.Skip("open fail:", err)
	}
	for _, name := range []string{
		"materials/workshop/2188505192/c7884e6807cf62bb85f8d8b67942cec4.tex",
		"materials/workshop/2188505192/60-604156_flare17-rainbow-lens-flare-png.tex",
	} {
		entry := reader.lookup(name)
		if entry == nil {
			t.Log("entry not found:", name)
			continue
		}
		data, err := reader.readEntry(entry)
		if err != nil {
			t.Fatal(err)
		}
		img, err := weDecodeTex(data)
		if err != nil {
			t.Fatal("decode:", err)
		}
		decoded, err := pngDecodeBytes(img.ImageData)
		if err != nil {
			t.Fatal("png:", err)
		}
		bounds := decoded.Bounds()
		opaque, semi, transparent := 0, 0, 0
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				_, _, _, a := decoded.At(x, y).RGBA()
				switch {
				case a == 0xFFFF:
					opaque++
				case a == 0:
					transparent++
				default:
					semi++
				}
			}
		}
		total := bounds.Dx() * bounds.Dy()
		fmt.Printf("%s\n  mime=%s %dx%d opaque=%d semi=%d transparent=%d (total %d)\n",
			name, img.Mime, bounds.Dx(), bounds.Dy(), opaque, semi, transparent, total)
	}
}

func pngDecodeBytes(b []byte) (image.Image, error) {
	decoded, err := pngDecode(b)
	if err != nil {
		return nil, err
	}
	return decoded, nil
}

func pngDecode(b []byte) (image.Image, error) {
	return png.Decode(bytes.NewReader(b))
}
