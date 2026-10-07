package launch

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestResolveSkipsNativesOnlyMainArtifact 旧版本（1.7.x/1.8.x）的 natives-only
// 库（如 lwjgl-platform）只发布 natives classifier jar，主 jar 从未存在；
// 解析时不得把主构件列为缺失，否则旧版本"装得上、启不来"。
func TestResolveSkipsNativesOnlyMainArtifact(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "libraries",
		"org", "lwjgl", "lwjgl", "lwjgl-platform", "2.9.4-beta-1"), 0o755); err != nil {
		t.Fatal(err)
	}
	nativesJar := filepath.Join(root, "libraries", "org", "lwjgl", "lwjgl",
		"lwjgl-platform", "2.9.4-beta-1", "lwjgl-platform-2.9.4-beta-1-natives-windows.jar")
	if err := os.WriteFile(nativesJar, []byte("jar"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "versions", "1.7.10"), 0o755); err != nil {
		t.Fatal(err)
	}
	clientJar := filepath.Join(root, "versions", "1.7.10", "1.7.10.jar")
	if err := os.WriteFile(clientJar, []byte("jar"), 0o644); err != nil {
		t.Fatal(err)
	}

	library := json.RawMessage(`{
		"name": "org.lwjgl.lwjgl:lwjgl-platform:2.9.4-beta-1",
		"natives": {"windows": "natives-windows"},
		"extract": {"exclude": ["META-INF/"]}
	}`)
	profile := &MinecraftVersionProfile{
		Id:                 "1.7.10",
		ClientJarVersionId: "1.7.10",
		Libraries:          []json.RawMessage{library},
	}

	resolved, err := (MinecraftLibraryResolver{}).Resolve(
		context.Background(), profile, root, map[string]bool{})
	if err != nil {
		t.Fatalf("natives-only 库不应导致解析失败（缺主构件是正常状态）：%v", err)
	}
	if len(resolved.Natives) != 1 {
		t.Fatalf("应解析出 1 个 natives 归档，实际 %d", len(resolved.Natives))
	}
	for _, entry := range resolved.Classpath {
		if filepath.Base(entry) == "lwjgl-platform-2.9.4-beta-1.jar" {
			t.Error("natives-only 库的主构件不应进入 classpath")
		}
	}
}

// TestResolveStillRequiresOrdinaryArtifact 普通库（有 downloads.artifact 声明）
// 缺文件时仍要报错，确认上面的跳过逻辑没有扩大范围。
func TestResolveStillRequiresOrdinaryArtifact(t *testing.T) {
	root := t.TempDir()
	library := json.RawMessage(`{
		"name": "com.mojang:brigadier:1.0.18",
		"downloads": {"artifact": {"path": "com/mojang/brigadier/1.0.18/brigadier-1.0.18.jar"}}
	}`)
	profile := &MinecraftVersionProfile{
		Id:                 "1.19",
		ClientJarVersionId: "1.19",
		Libraries:          []json.RawMessage{library},
	}
	if _, err := (MinecraftLibraryResolver{}).Resolve(
		context.Background(), profile, root, map[string]bool{}); err == nil {
		t.Fatal("普通库缺主构件仍应报“版本文件不完整”")
	}
}
