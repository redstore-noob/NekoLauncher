package bindings

// WritePngFile 的行为：data URI / 裸 base64 / 非 PNG 数据。

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

var writePngFixture = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x01}

func TestWritePngFile(t *testing.T) {
	api := &SystemAPI{}
	target := filepath.Join(t.TempDir(), "out.png")

	// data URI 形式
	dataURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(writePngFixture)
	if err := api.WritePngFile(target, dataURI); err != nil {
		t.Fatalf("写入 data URI 失败：%v", err)
	}
	if raw, err := os.ReadFile(target); err != nil || len(raw) < 4 || string(raw[:4]) != "\x89PNG" {
		t.Fatalf("文件内容不符：%v", err)
	}

	// 裸 base64 形式
	bare := filepath.Join(t.TempDir(), "bare.png")
	if err := api.WritePngFile(bare, base64.StdEncoding.EncodeToString(writePngFixture)); err != nil {
		t.Fatalf("写入裸 base64 失败：%v", err)
	}

	// 非 PNG 数据：拒绝
	notPng := filepath.Join(t.TempDir(), "fake.png")
	junk := base64.StdEncoding.EncodeToString([]byte("hello, not a png"))
	if err := api.WritePngFile(notPng, junk); err == nil {
		t.Error("非 PNG 数据应被拒绝")
	}

	// 非法 base64：拒绝
	if err := api.WritePngFile(filepath.Join(t.TempDir(), "x.png"), "!!!not-base64!!!"); err == nil {
		t.Error("非法 base64 应被拒绝")
	}

	// 空路径：拒绝
	if err := api.WritePngFile("", dataURI); err == nil {
		t.Error("空路径应被拒绝")
	}
}
