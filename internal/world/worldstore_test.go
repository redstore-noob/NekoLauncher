package world

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"nekolauncher/internal/config"
	"nekolauncher/internal/instance"
)

// useTempStorage 把启动器存储目录指到临时目录，并显式关闭"全局默认版本隔离"。
// 三件事都必须做：
//  1. GetRecentWorlds 内部经 config.Get / config.DefaultVersionIsolation 读实例配置，
//     不重定向就会读（并有风险写）用户真实的 launcher.yaml；
//  2. 该配置缺省为 true（见 config.DefaultVersionIsolation），不显式写 false 的话
//     实例会被解析成"版本隔离"布局，游戏目录落到 versions/<版本>/，共享布局用例全部假失败；
//  3. 写配置只落在 t.TempDir() 里，用户数据零接触。
func useTempStorage(t *testing.T) {
	t.Helper()
	original := config.StorageDirectory()
	if err := config.SetStorageDirectory(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = config.SetStorageDirectory(original) })
	if !config.SetValue("defaultVersionIsolation", "false") {
		t.Fatal("写入临时配置失败（defaultVersionIsolation）")
	}
}

// useIsolatedStorage 临时存储 + 全局默认版本隔离开启：用于验证隔离布局下
// 每个实例各自扫描 versions/<版本>/saves。
func useIsolatedStorage(t *testing.T) {
	t.Helper()
	original := config.StorageDirectory()
	if err := config.SetStorageDirectory(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = config.SetStorageDirectory(original) })
	if !config.SetValue("defaultVersionIsolation", "true") {
		t.Fatal("写入临时配置失败（defaultVersionIsolation）")
	}
}

// sharedSnapshot 造一个"共享目录布局"的实例快照：游戏目录就是 minecraftDirectory 本身。
// 前提是 useTempStorage 已把全局默认版本隔离写成 false：
// 临时目录下没有任何隔离证据，布局解析会落到共享分支
// （GameVersionIsolationGetGameDirectory 返回空串 → 沿用根目录）。
func sharedSnapshot(minecraftDirectory string, versionIds ...string) instance.GameInstanceSnapshot {
	selected := ""
	if len(versionIds) > 0 {
		selected = versionIds[len(versionIds)-1]
	}
	return instance.GameInstanceSnapshot{
		MinecraftDirectory: minecraftDirectory,
		VersionIds:         versionIds,
		SelectedVersionId:  selected,
	}
}

// writeWorld 在 savesDirectory 下造一个世界目录，返回世界目录路径。
// withLevelDat=false 时只建目录，用于验证"level.dat 缺失就取目录时间"。
func writeWorld(t *testing.T, savesDirectory, name string, withLevelDat bool) string {
	t.Helper()
	worldPath := filepath.Join(savesDirectory, name)
	if err := os.MkdirAll(worldPath, 0o755); err != nil {
		t.Fatalf("建世界目录失败：%v", err)
	}
	if withLevelDat {
		if err := os.WriteFile(filepath.Join(worldPath, "level.dat"), []byte("level"), 0o644); err != nil {
			t.Fatalf("写 level.dat 失败：%v", err)
		}
	}
	return worldPath
}

// writeSavesDirectory 在 gameDirectory 下建 saves 目录。
func writeSavesDirectory(t *testing.T, gameDirectory string) string {
	t.Helper()
	saves := filepath.Join(gameDirectory, "saves")
	if err := os.MkdirAll(saves, 0o755); err != nil {
		t.Fatalf("建 saves 目录失败：%v", err)
	}
	return saves
}

// setModTime 设置路径的修改时间（排序依据）。
func setModTime(t *testing.T, path string, modTime time.Time) {
	t.Helper()
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("设置修改时间失败：%v", err)
	}
}

// worldNames 取出世界名列表，便于比较顺序。
func worldNames(worlds []WorldInfo) []string {
	names := make([]string, len(worlds))
	for i, world := range worlds {
		names[i] = world.Name
	}
	return names
}

// findWorld 按名字取世界信息。
func findWorld(t *testing.T, worlds []WorldInfo, name string) WorldInfo {
	t.Helper()
	for _, world := range worlds {
		if world.Name == name {
			return world
		}
	}
	t.Fatalf("结果里没有世界 %s：%v", name, worldNames(worlds))
	return WorldInfo{}
}

// TestGetRecentWorldsListsWorldsWithMetadata 扫描共享目录布局的 saves 并填充世界信息。
// 防的回归：世界列表整体扫不出来（主页"最近游玩"永远空白），
// 以及 OwnerVersionId / DirectoryPath 缺字段导致一键启动切错实例、打开错目录。
func TestGetRecentWorldsListsWorldsWithMetadata(t *testing.T) {
	useTempStorage(t)
	root := t.TempDir()
	saves := writeSavesDirectory(t, root)
	newWorld := writeWorld(t, saves, "新世界", true)
	oldWorld := writeWorld(t, saves, "Old World", true)

	base := time.Now().Add(-48 * time.Hour)
	setModTime(t, filepath.Join(oldWorld, "level.dat"), base)
	setModTime(t, filepath.Join(newWorld, "level.dat"), base.Add(time.Hour))

	worlds := GetRecentWorlds(sharedSnapshot(root, "1.20.1"), 50)
	if len(worlds) != 2 {
		t.Fatalf("应扫到 2 个世界，得到 %d：%v", len(worlds), worldNames(worlds))
	}

	newest := findWorld(t, worlds, "新世界")
	if newest.OwnerVersionId != "1.20.1" {
		t.Fatalf("世界应归属实例 1.20.1，得到 %q", newest.OwnerVersionId)
	}
	if newest.DirectoryPath != newWorld {
		t.Fatalf("世界目录不符：%q，期望 %q", newest.DirectoryPath, newWorld)
	}
	if !newest.LastPlayed.Equal(base.Add(time.Hour)) {
		t.Fatalf("LastPlayed 应取 level.dat 的修改时间，得到 %v", newest.LastPlayed)
	}
	if newest.IconPath != "" {
		t.Fatalf("没有 icon.png 时 IconPath 应为空串，得到 %q", newest.IconPath)
	}
}

// TestGetRecentWorldsSortsByLastPlayedDescending 结果按最后游玩时间降序。
// 防的回归：排序方向写反，"最近游玩"列表把最久没玩的世界顶在最前面。
func TestGetRecentWorldsSortsByLastPlayedDescending(t *testing.T) {
	useTempStorage(t)
	root := t.TempDir()
	saves := writeSavesDirectory(t, root)
	middle := writeWorld(t, saves, "middle", true)
	oldest := writeWorld(t, saves, "oldest", true)
	newest := writeWorld(t, saves, "newest", true)

	now := time.Now().Add(-time.Hour)
	setModTime(t, filepath.Join(oldest, "level.dat"), now.Add(-10*time.Hour))
	setModTime(t, filepath.Join(middle, "level.dat"), now.Add(-5*time.Hour))
	setModTime(t, filepath.Join(newest, "level.dat"), now)

	worlds := GetRecentWorlds(sharedSnapshot(root, "1.20.1"), 10)
	if names := worldNames(worlds); len(names) != 3 ||
		names[0] != "newest" || names[1] != "middle" || names[2] != "oldest" {
		t.Fatalf("排序应为 newest → middle → oldest，得到 %v", names)
	}
}

// TestGetRecentWorldsAppliesLimit 上限截断只保留最近的前 max 个，max<=0 时返回空。
// 防的回归：max 被忽略导致主页一次渲染上千个世界（滚动卡死），
// 或 max<=0 时切片越界 panic。
func TestGetRecentWorldsAppliesLimit(t *testing.T) {
	useTempStorage(t)
	root := t.TempDir()
	saves := writeSavesDirectory(t, root)

	now := time.Now().Add(-time.Hour)
	names := []string{"w1", "w2", "w3", "w4", "w5"}
	for index, name := range names {
		worldPath := writeWorld(t, saves, name, true)
		// w5 最新，w1 最旧
		setModTime(t, filepath.Join(worldPath, "level.dat"), now.Add(time.Duration(index)*time.Hour))
	}
	snapshot := sharedSnapshot(root, "1.20.1")

	limited := GetRecentWorlds(snapshot, 2)
	if got := worldNames(limited); len(got) != 2 || got[0] != "w5" || got[1] != "w4" {
		t.Fatalf("上限 2 应保留最近的 w5、w4，得到 %v", got)
	}
	if limited == nil {
		t.Fatal("截断结果不应是 nil 切片（前端 JSON 化会变成 null）")
	}

	if got := GetRecentWorlds(snapshot, 0); got == nil || len(got) != 0 {
		t.Fatalf("max=0 应返回空数组，得到 %#v", got)
	}
	if got := GetRecentWorlds(snapshot, -3); got == nil || len(got) != 0 {
		t.Fatalf("max<0 应返回空数组（且不 panic），得到 %#v", got)
	}
	if got := GetRecentWorlds(snapshot, 99); len(got) != len(names) {
		t.Fatalf("上限大于世界数时应全部返回，得到 %v", worldNames(got))
	}
}

// TestGetRecentWorldsSkipsDotAndNonDirectoryEntries saves 下的点目录与普通文件不算世界。
// 防的回归：启动器的临时目录（存档导入 .nya-import-*、回滚恢复 .nya-restore-*）
// 与世界目录同级，一旦被当成世界列出来，用户就能在列表里看到并"打开"半成品存档。
func TestGetRecentWorldsSkipsDotAndNonDirectoryEntries(t *testing.T) {
	useTempStorage(t)
	root := t.TempDir()
	saves := writeSavesDirectory(t, root)
	writeWorld(t, saves, "Real World", true)
	// 临时目录：可能带 level.dat，但名字以点开头
	importTemp := writeWorld(t, saves, ".nya-import-1712", true)
	restoreTemp := writeWorld(t, saves, ".nya-restore-backup", true)
	// 点目录里的内容再新，也不该挤进最近游玩列表
	newest := time.Now()
	setModTime(t, filepath.Join(importTemp, "level.dat"), newest)
	setModTime(t, filepath.Join(restoreTemp, "level.dat"), newest)
	// saves 根下的普通文件
	if err := os.WriteFile(filepath.Join(saves, "session.lock"), []byte("x"), 0o644); err != nil {
		t.Fatalf("写文件失败：%v", err)
	}

	worlds := GetRecentWorlds(sharedSnapshot(root, "1.20.1"), 10)
	if got := worldNames(worlds); len(got) != 1 || got[0] != "Real World" {
		t.Fatalf("只应列出 Real World（点目录必须跳过），得到 %v", got)
	}

	// 单独断言点目录确实存在且带 level.dat，避免用例因为"目录没造出来"而假通过
	if _, err := os.Stat(filepath.Join(importTemp, "level.dat")); err != nil {
		t.Fatalf("用例前置条件不成立：导入临时目录应当存在：%v", err)
	}
}

// TestGetRecentWorldsFallsBackToDirectoryTime level.dat 缺失时用世界目录时间，而不是零值时间。
// 防的回归：没进过游戏的新建世界 LastPlayed 变成 0001-01-01，
// 被排序甩到列表最后甚至被截断掉，用户新建的世界在启动器里"消失"。
func TestGetRecentWorldsFallsBackToDirectoryTime(t *testing.T) {
	useTempStorage(t)
	root := t.TempDir()
	saves := writeSavesDirectory(t, root)
	noLevel := writeWorld(t, saves, "Broken", false)
	withLevel := writeWorld(t, saves, "WithLevel", true)

	older := time.Now().Add(-72 * time.Hour)
	recent := time.Now().Add(-time.Hour)
	setModTime(t, noLevel, older)
	setModTime(t, filepath.Join(withLevel, "level.dat"), recent)

	worlds := GetRecentWorlds(sharedSnapshot(root, "1.20.1"), 10)
	if len(worlds) != 2 {
		t.Fatalf("两个世界都应被列出（缺 level.dat 不代表不是世界），得到 %v", worldNames(worlds))
	}

	broken := findWorld(t, worlds, "Broken")
	if broken.LastPlayed.IsZero() {
		t.Fatal("缺 level.dat 时应回落到目录修改时间，不能是零值时间")
	}
	if !broken.LastPlayed.Equal(older) {
		t.Fatalf("应取世界目录修改时间 %v，得到 %v", older, broken.LastPlayed)
	}
	// 排序仍按时间降序
	if names := worldNames(worlds); names[0] != "WithLevel" || names[1] != "Broken" {
		t.Fatalf("排序应为 WithLevel → Broken，得到 %v", names)
	}
}

// TestGetRecentWorldsReadsIconWhenPresent 只有真实存在的 icon.png 才填 IconPath。
// 防的回归：不论有没有图标都返回路径，前端加载到 404 破图；
// 或反过来永远返回空串，世界图标功能整体失效。
func TestGetRecentWorldsReadsIconWhenPresent(t *testing.T) {
	useTempStorage(t)
	root := t.TempDir()
	saves := writeSavesDirectory(t, root)
	withIcon := writeWorld(t, saves, "WithIcon", true)
	writeWorld(t, saves, "WithoutIcon", true)
	iconPath := filepath.Join(withIcon, "icon.png")
	if err := os.WriteFile(iconPath, []byte("fake-png"), 0o644); err != nil {
		t.Fatalf("写图标失败：%v", err)
	}

	worlds := GetRecentWorlds(sharedSnapshot(root, "1.20.1"), 10)
	if icon := findWorld(t, worlds, "WithIcon").IconPath; icon != iconPath {
		t.Fatalf("IconPath 应为 %q，得到 %q", iconPath, icon)
	}
	if icon := findWorld(t, worlds, "WithoutIcon").IconPath; icon != "" {
		t.Fatalf("没有图标时应返回空串，得到 %q", icon)
	}

	// icon.png 是目录（异常文件系统状态）时不能当作图标
	broken := writeWorld(t, saves, "BrokenIcon", true)
	if err := os.Mkdir(filepath.Join(broken, "icon.png"), 0o755); err != nil {
		t.Fatalf("建目录失败：%v", err)
	}
	worlds = GetRecentWorlds(sharedSnapshot(root, "1.20.1"), 10)
	if icon := findWorld(t, worlds, "BrokenIcon").IconPath; icon != "" {
		t.Fatalf("icon.png 是目录时不应填 IconPath，得到 %q", icon)
	}
}

// TestGetRecentWorldsHandlesMissingSavesDirectoryAndFiles 目录缺失 / 不是目录时不 panic，
// 且总是返回非 nil 的空数组。
// 防的回归：全新安装（还没进过游戏，没有 saves 目录）时主页直接 panic 或返回 null，
// 前端对 null 做 .map 崩掉整个页面。
func TestGetRecentWorldsHandlesMissingSavesDirectoryAndFiles(t *testing.T) {
	useTempStorage(t)

	cases := []struct {
		name string
		root func(t *testing.T) string
	}{
		{
			"minecraft 目录本身不存在",
			func(t *testing.T) string { return filepath.Join(t.TempDir(), "gone") },
		},
		{
			"没有 saves 子目录",
			func(t *testing.T) string { return t.TempDir() },
		},
		{
			"saves 是普通文件而不是目录",
			func(t *testing.T) string {
				root := t.TempDir()
				if err := os.WriteFile(filepath.Join(root, "saves"), []byte("x"), 0o644); err != nil {
					t.Fatalf("写文件失败：%v", err)
				}
				return root
			},
		},
		{
			"saves 目录为空",
			func(t *testing.T) string {
				root := t.TempDir()
				writeSavesDirectory(t, root)
				return root
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			worlds := GetRecentWorlds(sharedSnapshot(testCase.root(t), "1.20.1"), 10)
			if worlds == nil {
				t.Fatal("应返回空数组而不是 nil（前端会按数组处理）")
			}
			if len(worlds) != 0 {
				t.Fatalf("不应列出任何世界，得到 %v", worldNames(worlds))
			}
		})
	}
}

// TestGetRecentWorldsReturnsEmptyForInvalidSnapshot 未就绪/出错的实例快照直接返回空。
// 防的回归：启动器还在扫描实例（IsLoading）时就去读磁盘，
// 在世界列表里闪出一批随后又被清掉的条目；以及报错快照被拿来拼路径扫到用户目录。
func TestGetRecentWorldsReturnsEmptyForInvalidSnapshot(t *testing.T) {
	useTempStorage(t)

	// 造一份"本该能扫出世界"的目录，确保返回空是快照校验拦下的，而不是目录恰好为空
	root := t.TempDir()
	saves := writeSavesDirectory(t, root)
	writeWorld(t, saves, "ShouldNotAppear", true)

	cases := []struct {
		name     string
		snapshot instance.GameInstanceSnapshot
	}{
		{
			"仍在加载",
			instance.GameInstanceSnapshot{
				MinecraftDirectory: root,
				VersionIds:         []string{"1.20.1"},
				IsLoading:          true,
			},
		},
		{
			"实例扫描报错",
			instance.GameInstanceSnapshot{
				MinecraftDirectory: root,
				VersionIds:         []string{"1.20.1"},
				ErrorMessage:       "读取 versions 目录失败",
			},
		},
		{
			"Minecraft 目录为空",
			instance.GameInstanceSnapshot{VersionIds: []string{"1.20.1"}},
		},
		{
			"Minecraft 目录只有空白",
			instance.GameInstanceSnapshot{MinecraftDirectory: "   ", VersionIds: []string{"1.20.1"}},
		},
		{
			"没有实例",
			sharedSnapshot(root),
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			worlds := GetRecentWorlds(testCase.snapshot, 10)
			if worlds == nil {
				t.Fatal("应返回空数组而不是 nil")
			}
			if len(worlds) != 0 {
				t.Fatalf("非法快照不应扫出世界，得到 %v", worldNames(worlds))
			}
		})
	}

	// 对照：同样的目录在合法快照下必须能扫出来，否则上面的用例是假通过
	if worlds := GetRecentWorlds(sharedSnapshot(root, "1.20.1"), 10); len(worlds) != 1 {
		t.Fatalf("对照组应扫到 1 个世界，得到 %v", worldNames(worlds))
	}
}

// TestGetRecentWorldsDeduplicatesSharedDirectory 多实例共享同一游戏目录时只扫一次，
// 世界归属优先当前选中实例。
// 防的回归：共享目录下每个实例各扫一遍，同一个世界在列表里出现 N 次（N = 实例数）；
// 以及归属写成列表第一个实例，一键启动时把用户切到没在用的版本。
func TestGetRecentWorldsDeduplicatesSharedDirectory(t *testing.T) {
	useTempStorage(t)
	root := t.TempDir()
	saves := writeSavesDirectory(t, root)
	writeWorld(t, saves, "Shared", true)

	// 两个实例都解析到同一个共享目录（GameVersionIsolationGetGameDirectory 返回空串）
	worlds := GetRecentWorlds(sharedSnapshot(root, "1.20.1", "1.21"), 10)
	if len(worlds) != 1 {
		t.Fatalf("共享目录只能扫出一份世界列表，得到 %v（重复扫描会让世界出现多次）", worldNames(worlds))
	}
	if owner := worlds[0].OwnerVersionId; owner != "1.21" {
		t.Fatalf("世界应归属当前选中实例 1.21，得到 %q", owner)
	}

	// 未选中任何实例时退回第一个实例
	noSelection := instance.GameInstanceSnapshot{
		MinecraftDirectory: root,
		VersionIds:         []string{"1.20.1", "1.21"},
	}
	worlds = GetRecentWorlds(noSelection, 10)
	if len(worlds) != 1 {
		t.Fatalf("共享目录只能扫出一份世界列表，得到 %v", worldNames(worlds))
	}
	if owner := worlds[0].OwnerVersionId; owner != "1.20.1" {
		t.Fatalf("未选中实例时应归属第一个实例，得到 %q", owner)
	}

	// 选中的实例不属于该目录的拥有者时，同样退回第一个拥有者
	foreign := instance.GameInstanceSnapshot{
		MinecraftDirectory: root,
		VersionIds:         []string{"1.20.1", "1.21"},
		SelectedVersionId:  "1.19",
	}
	worlds = GetRecentWorlds(foreign, 10)
	if owner := worlds[0].OwnerVersionId; owner != "1.20.1" {
		t.Fatalf("选中实例不在拥有者列表中时应归属第一个，得到 %q", owner)
	}
}

// TestGetRecentWorldsScansEachVersionDirectory 每个实例的游戏目录都要扫到。
// 防的回归：目录去重时把不同目录当成同一个（normalize 键算错），
// 版本隔离布局下只有一个实例的存档能被列出。
func TestGetRecentWorldsScansEachVersionDirectory(t *testing.T) {
	useIsolatedStorage(t)
	root := t.TempDir()

	// 两个版本隔离实例：版本目录下各有独立 saves（触发"独立版本内容"自动检测）
	firstVersion := filepath.Join(root, "versions", "1.20.1")
	secondVersion := filepath.Join(root, "versions", "1.21")
	writeWorld(t, writeSavesDirectory(t, firstVersion), "WorldA", true)
	writeWorld(t, writeSavesDirectory(t, secondVersion), "WorldB", true)

	snapshot := instance.GameInstanceSnapshot{
		MinecraftDirectory: root,
		VersionIds:         []string{"1.20.1", "1.21"},
		SelectedVersionId:  "1.21",
	}

	worlds := GetRecentWorlds(snapshot, 10)
	if len(worlds) != 2 {
		t.Fatalf("两个隔离实例各有一个世界，应扫到 2 个，得到 %v", worldNames(worlds))
	}
	if owner := findWorld(t, worlds, "WorldA").OwnerVersionId; owner != "1.20.1" {
		t.Fatalf("WorldA 应归属 1.20.1，得到 %q", owner)
	}
	if owner := findWorld(t, worlds, "WorldB").OwnerVersionId; owner != "1.21" {
		t.Fatalf("WorldB 应归属 1.21，得到 %q", owner)
	}
	if world := findWorld(t, worlds, "WorldB"); world.DirectoryPath != filepath.Join(secondVersion, "saves", "WorldB") {
		t.Fatalf("隔离实例的世界目录不符：%q", world.DirectoryPath)
	}
}
