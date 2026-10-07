package bindings

// WritePngFile 的行为：data URI / 裸 base64 / 非 PNG 数据 / 写路径收口。

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
	// 模拟一次"用户在对话框里选择了这个路径"
	api.approveWritePath(target)

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
	api.approveWritePath(bare)
	if err := api.WritePngFile(bare, base64.StdEncoding.EncodeToString(writePngFixture)); err != nil {
		t.Fatalf("写入裸 base64 失败：%v", err)
	}

	// 非 PNG 数据：拒绝（路径已批准，挡的是内容校验）
	notPng := filepath.Join(t.TempDir(), "fake.png")
	api.approveWritePath(notPng)
	junk := base64.StdEncoding.EncodeToString([]byte("hello, not a png"))
	if err := api.WritePngFile(notPng, junk); err == nil {
		t.Error("非 PNG 数据应被拒绝")
	}

	// 非法 base64：拒绝
	approvedX := filepath.Join(t.TempDir(), "x.png")
	api.approveWritePath(approvedX)
	if err := api.WritePngFile(approvedX, "!!!not-base64!!!"); err == nil {
		t.Error("非法 base64 应被拒绝")
	}

	// 空路径：拒绝
	if err := api.WritePngFile("", dataURI); err == nil {
		t.Error("空路径应被拒绝")
	}
}

// 写路径收口：未经对话框批准、也不在游戏/音乐目录内的写入必须被拒绝——
// 插件与宿主同 WebView，无限制的写入等于"改写任意文件"的能力。
func TestWriteGuardRejectsUnapprovedPath(t *testing.T) {
	api := &SystemAPI{}
	dataURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(writePngFixture)
	sneaky := filepath.Join(t.TempDir(), "sneaky.png")
	if err := api.WritePngFile(sneaky, dataURI); err == nil {
		t.Fatal("未批准路径的写入应被拒绝")
	}
	if _, err := os.Stat(sneaky); !os.IsNotExist(err) {
		t.Fatal("被拒绝的写入不应落盘")
	}
	// 批准的是精确路径：同目录的兄弟文件不在批准范围内
	api.approveWritePath(sneaky)
	brother := filepath.Join(filepath.Dir(sneaky), "brother.png")
	if err := api.WritePngFile(brother, dataURI); err == nil {
		t.Fatal("批准路径的兄弟文件不应连带放行")
	}
	if err := api.WritePngFile(sneaky, dataURI); err != nil {
		t.Fatalf("已批准路径的写入不应被拒绝：%v", err)
	}
}
