// 端到端真实测试：真实下载 Vanilla 服务端并完整走一遍
// 创建 → 启动 → Done 检测 → list 指令 → 停止 流程。需要网络与 Java。
package mcserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLiveE2EVanilla(t *testing.T) {
	requireNetwork(t)
	root := useTempRoot(t)

	id, err := CreateServer(context.Background(), CreateOptions{
		Name: "e2e-vanilla", Core: CoreVanilla, MCVersion: "1.21.4",
		Port: 25599, MaxPlayers: 5, AcceptEULA: true,
	})
	if err != nil {
		t.Fatalf("创建失败：%v", err)
	}

	// EULA 与初始配置落盘校验
	eula, err := os.ReadFile(filepath.Join(root, id, "eula.txt"))
	if err != nil || !strings.Contains(string(eula), "eula=true") {
		t.Fatalf("eula.txt 未正确写入：%v", err)
	}
	properties, err := os.ReadFile(filepath.Join(root, id, "server.properties"))
	if err != nil || !strings.Contains(string(properties), "server-port=25599") {
		t.Fatalf("server.properties 未正确写入：%v", err)
	}

	mgr := Default()
	if err := mgr.StartServer(context.Background(), id); err != nil {
		t.Fatalf("启动失败：%v", err)
	}

	// 等待启动完成（Done 标志 → running），上限 90 秒
	deadline := time.Now().Add(90 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("90 秒内服务器未进入 running 状态")
		}
		if mgr.Poll(id, 0).Status == StatusRunning {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	// 发送 list 指令，等待响应日志出现
	if err := mgr.SendServerCommand(id, "list"); err != nil {
		t.Fatalf("发送指令失败：%v", err)
	}
	deadline = time.Now().Add(15 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("15 秒内未收到 list 响应")
		}
		snapshot := mgr.Poll(id, 0)
		for _, line := range snapshot.LogLines {
			if strings.Contains(line, "players online") {
				goto done
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
done:

	// 停止并等待落回 stopped
	if err := mgr.StopServer(id, false); err != nil {
		t.Fatalf("停止失败：%v", err)
	}
	deadline = time.Now().Add(30 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("30 秒内服务器未停止")
		}
		if mgr.Poll(id, 0).Status == StatusStopped {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}

	// 已停止后可删除
	if err := DeleteServer(id); err != nil {
		t.Fatalf("删除失败：%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, id)); !os.IsNotExist(err) {
		t.Fatal("删除后目录仍存在")
	}
}
