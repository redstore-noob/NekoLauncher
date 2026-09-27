package music

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// newTestLibrary 建一个只走内存配置的音乐库。
// 禁止触碰用户真实配置（config 包），测试一律自带 MemoryConfigStore。
func newTestLibrary() *MusicLibrary {
	return NewMusicLibrary(NewMemoryConfigStore())
}

// writeAudioFile 在 dir 下写一个假音频文件，返回完整路径。
// 只写字节骨架：本包不做真实解码，播放输出由前端 AudioPlayer 负责。
func writeAudioFile(t *testing.T, dir, name string, size int) string {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("建目录失败：%v", err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatalf("写文件失败：%v", err)
	}
	return path
}

// trackPaths 按当前顺序取出曲目路径，便于比较排序结果。
func trackPaths(tracks []MusicTrack) []string {
	paths := make([]string, len(tracks))
	for i, track := range tracks {
		paths[i] = track.FilePath
	}
	return paths
}

// TestScanKeepsOnlyWhitelistedExtensions 扫描只收扩展名白名单内的文件。
// 防的回归：C# 时代把 wma 等 WebView 解不了的容器收进列表，用户"能点却永远没声音"；
// 也别把 .txt / .mp3.bak / 扩展名缺失的普通文件混入播放列表。
func TestScanKeepsOnlyWhitelistedExtensions(t *testing.T) {
	root := t.TempDir()
	seen := []string{
		"track.mp3", "track.wav", "track.ogg", "track.flac",
		"track.aac", "track.m4a", "track.opus",
	}
	for _, name := range seen {
		writeAudioFile(t, root, name, 32)
	}
	for _, name := range []string{"readme.txt", "movie.mp4", "song.wma", "song.mp3.bak", "noext"} {
		writeAudioFile(t, root, name, 32)
	}

	library := newTestLibrary()
	if err := library.SetFolder(root); err != nil {
		t.Fatalf("设置音乐文件夹失败：%v", err)
	}

	got := map[string]bool{}
	for _, track := range library.Tracks() {
		got[filepath.Base(track.FilePath)] = true
	}
	if len(got) != len(seen) {
		t.Fatalf("扫描到 %d 个曲目，期望 %d 个：%v", len(got), len(seen), got)
	}
	for _, name := range seen {
		if !got[name] {
			t.Errorf("白名单文件 %s 未被收录，曲库会漏歌", name)
		}
	}
	for _, name := range []string{"readme.txt", "movie.mp4", "song.wma", "song.mp3.bak", "noext"} {
		if got[name] {
			t.Errorf("非白名单文件 %s 被收进曲库（会变成点了没声音的僵尸条目）", name)
		}
	}
}

// TestScanIsRecursiveAndCaseInsensitive 递归扫描子目录，并且大小写不同的扩展名照收。
// 防的回归：只扫一层目录导致按专辑分文件夹的用户曲库大面积漏歌；
// 以及 ".MP3" 被当成不支持格式（Windows 上文件名大小写不敏感，这里必须跟上）。
func TestScanIsRecursiveAndCaseInsensitive(t *testing.T) {
	root := t.TempDir()
	top := writeAudioFile(t, root, "top.mp3", 32)
	sub := writeAudioFile(t, root, "album/a.ogg", 32)
	deep := writeAudioFile(t, root, "album/disc2/deep.flac", 32)
	upper := writeAudioFile(t, root, "UPPER.MP3", 32)
	mixed := writeAudioFile(t, root, "album/Mixed.Flac", 32)

	library := newTestLibrary()
	if err := library.SetFolder(root); err != nil {
		t.Fatalf("设置音乐文件夹失败：%v", err)
	}

	got := trackPaths(library.Tracks())
	sort.Strings(got)
	want := []string{deep, mixed, sub, top, upper}
	sort.Strings(want)
	if !equalStringSlices(got, want) {
		t.Fatalf("递归扫描结果不符：\n得到 %v\n期望 %v", got, want)
	}

	// 枚举器本身也要能翻出各层文件（不让白名单掩盖递归逻辑的回归）
	if files := enumerateAudioFilesSafe(root); len(files) != 5 {
		t.Fatalf("enumerateAudioFilesSafe 递归到 %d 个文件，期望 5：%v", len(files), files)
	}
}

// TestScanSkipsUnreadableAndMissingFolder 目录不存在 / 路径为空 / 不是目录时清空曲库且不 panic。
// 防的回归：用户在设置里填错路径或拔掉移动硬盘后，界面继续显示已经播不出来的旧列表。
func TestScanSkipsUnreadableAndMissingFolder(t *testing.T) {
	root := t.TempDir()
	writeAudioFile(t, root, "a.mp3", 32)

	library := newTestLibrary()
	if err := library.SetFolder(root); err != nil {
		t.Fatalf("设置音乐文件夹失败：%v", err)
	}
	if len(library.Tracks()) != 1 {
		t.Fatalf("前置条件不成立：应扫到 1 首，得到 %d", len(library.Tracks()))
	}

	// 目录被删除
	missing := filepath.Join(t.TempDir(), "gone")
	if err := library.SetFolder(missing); err != nil {
		t.Fatalf("设置不存在的文件夹不该报错（只应扫描为空）：%v", err)
	}
	if tracks := library.Tracks(); len(tracks) != 0 {
		t.Fatalf("目录不存在时曲库应清空，仍剩 %d 首", len(tracks))
	}

	// 路径指向普通文件而非目录
	fileAsFolder := writeAudioFile(t, t.TempDir(), "not-a-dir.mp3", 8)
	if err := library.SetFolder(fileAsFolder); err != nil {
		t.Fatalf("设置普通文件路径不该报错：%v", err)
	}
	if tracks := library.Tracks(); len(tracks) != 0 {
		t.Fatalf("路径不是目录时曲库应清空，仍剩 %d 首", len(tracks))
	}

	// 空白路径：拒绝设置，保留上一次的文件夹配置
	if err := library.SetFolder("   "); err == nil {
		t.Fatal("空白文件夹路径应返回错误（否则会静默清空用户曲库）")
	} else if !strings.Contains(err.Error(), "不能为空") {
		t.Fatalf("空白路径的错误信息应说明原因，得到 %q", err.Error())
	}
	if library.FolderPath() != fileAsFolder {
		t.Fatalf("被拒绝的空白路径不应写入配置，当前 = %q", library.FolderPath())
	}
}

// TestScanTrimsFolderPathAndSkipsNonRegularFiles 扫描前去掉路径首尾空白，并跳过非普通文件。
// 防的回归：从剪贴板粘贴路径常带空格/制表符，直接拼目录会扫不到歌；
// 目录项里的子目录不能被当成曲目（否则列表出现点不开的条目）。
func TestScanTrimsFolderPathAndSkipsNonRegularFiles(t *testing.T) {
	root := t.TempDir()
	writeAudioFile(t, root, "song.mp3", 16)
	if err := os.MkdirAll(filepath.Join(root, "fake.mp3"), 0o755); err != nil {
		t.Fatalf("建目录失败：%v", err)
	}

	library := newTestLibrary()
	if err := library.SetFolder("  " + root + "\t"); err != nil {
		t.Fatalf("设置音乐文件夹失败：%v", err)
	}
	if library.FolderPath() != root {
		t.Fatalf("配置里的路径应去掉首尾空白：%q", library.FolderPath())
	}

	tracks := library.Tracks()
	if len(tracks) != 1 || filepath.Base(tracks[0].FilePath) != "song.mp3" {
		t.Fatalf("应只扫到 song.mp3（名为 fake.mp3 的目录不算曲目），得到 %v", trackPaths(tracks))
	}

	// 直接验证枚举器的两道过滤：同名目录与非普通文件都不算曲目
	dirAsFile := filepath.Join(root, "album.mp3")
	if err := os.MkdirAll(dirAsFile, 0o755); err != nil {
		t.Fatalf("建目录失败：%v", err)
	}
	if isRegularFile(dirAsFile) {
		t.Error("isRegularFile 把目录判成了普通文件")
	}
	for _, file := range enumerateAudioFilesSafe(root) {
		if file == dirAsFile {
			t.Errorf("枚举结果混入了目录：%s", file)
		}
	}
}

// TestSortTracksByAllModesAndKeepsSource 六种排序模式都要真的按对应字段排序，且不修改入参切片。
// 防的回归：排序回调写反（升/降混淆）、排序键选错字段，
// 以及"就地排序"把 library 内部未排序的原始列表一起打乱。
func TestSortTracksByAllModesAndKeepsSource(t *testing.T) {
	base := time.Date(2024, 5, 1, 12, 0, 0, 0, time.UTC)
	tracks := []MusicTrack{
		{FilePath: `C:\m\Bravo.mp3`, FileSize: 300, LastModified: base.Add(2 * time.Hour)},
		{FilePath: `C:\m\alpha.mp3`, FileSize: 100, LastModified: base.Add(3 * time.Hour)},
		{FilePath: `C:\m\charlie.mp3`, FileSize: 200, LastModified: base.Add(1 * time.Hour)},
	}

	cases := []struct {
		name string
		mode MusicSortMode
		want []string
	}{
		{"按文件名升序", SortFileName, []string{"alpha.mp3", "Bravo.mp3", "charlie.mp3"}},
		{"按文件名降序", SortFileNameDesc, []string{"charlie.mp3", "Bravo.mp3", "alpha.mp3"}},
		{"按修改时间升序", SortDateModified, []string{"charlie.mp3", "Bravo.mp3", "alpha.mp3"}},
		{"按修改时间降序", SortDateModifiedDesc, []string{"alpha.mp3", "Bravo.mp3", "charlie.mp3"}},
		{"按大小升序", SortFileSize, []string{"alpha.mp3", "charlie.mp3", "Bravo.mp3"}},
		{"按大小降序", SortFileSizeDesc, []string{"Bravo.mp3", "charlie.mp3", "alpha.mp3"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			sorted := SortTracks(tracks, testCase.mode)
			got := make([]string, len(sorted))
			for i, track := range sorted {
				got[i] = filepath.Base(track.FilePath)
			}
			if !equalStringSlices(got, testCase.want) {
				t.Fatalf("排序结果不符：得到 %v，期望 %v", got, testCase.want)
			}
		})
	}

	// 入参切片必须保持原样（SortTracks 返回副本）
	wantOriginal := []string{"Bravo.mp3", "alpha.mp3", "charlie.mp3"}
	gotOriginal := make([]string, len(tracks))
	for i, track := range tracks {
		gotOriginal[i] = filepath.Base(track.FilePath)
	}
	if !equalStringSlices(gotOriginal, wantOriginal) {
		t.Fatalf("SortTracks 修改了入参切片：%v", gotOriginal)
	}

	if out := SortTracks(nil, SortFileName); len(out) != 0 {
		t.Fatalf("空列表排序应返回空结果，得到 %v", out)
	}
}

// TestSortModePersistsToConfigAndAppliesOnScan 排序模式写进配置，并在重新扫描后依然生效。
// 防的回归：重启启动器（重新扫描）后排序被打回文件名升序，用户设置看起来"没保存"。
func TestSortModePersistsToConfigAndAppliesOnScan(t *testing.T) {
	root := t.TempDir()
	writeAudioFile(t, root, "small.mp3", 16)
	writeAudioFile(t, root, "big.mp3", 4096)

	store := NewMemoryConfigStore()
	library := NewMusicLibrary(store)
	if err := library.SetFolder(root); err != nil {
		t.Fatalf("设置音乐文件夹失败：%v", err)
	}
	if library.SortMode() != SortFileName {
		t.Fatalf("默认排序模式应为 FileName，得到 %q", library.SortMode())
	}

	library.SetSortMode(SortFileSizeDesc)
	if saved := store.GetValue(sortKey); saved != string(SortFileSizeDesc) {
		t.Fatalf("排序模式未写入配置：%q", saved)
	}
	tracks := library.Tracks()
	if len(tracks) != 2 || filepath.Base(tracks[0].FilePath) != "big.mp3" {
		t.Fatalf("按大小降序应把 big.mp3 排在最前，得到 %v", trackPaths(tracks))
	}

	// 模拟重启：新建库实例（读同一份配置）后重新扫描
	reopened := NewMusicLibrary(store)
	reopened.Scan()
	if reopened.SortMode() != SortFileSizeDesc {
		t.Fatalf("重新扫描后排序模式应保留为 FileSizeDesc，得到 %q", reopened.SortMode())
	}
	reopenedTracks := reopened.Tracks()
	if len(reopenedTracks) != 2 || filepath.Base(reopenedTracks[0].FilePath) != "big.mp3" {
		t.Fatalf("重新扫描后应沿用已保存的排序，得到 %v", trackPaths(reopenedTracks))
	}

	// 配置里的排序值被手改坏时回落到原有模式，不能崩
	store.SetValue(sortKey, "NotAMode")
	reopened.Scan()
	if reopened.SortMode() != SortFileSizeDesc {
		t.Fatalf("非法排序值应保留上一次模式，得到 %q", reopened.SortMode())
	}

	// 配置里没有文件夹时扫描应清空列表
	empty := NewMusicLibrary(NewMemoryConfigStore())
	empty.Scan()
	if tracks := empty.Tracks(); len(tracks) != 0 {
		t.Fatalf("未设置文件夹时曲库应为空，得到 %v", trackPaths(tracks))
	}
}

// TestSearchIsCaseInsensitiveOnTitle 搜索按文件名（不含扩展名）模糊匹配且不区分大小写。
// 防的回归：界面搜索框输入大写或中文关键词后搜不到歌（大小写敏感），
// 以及空关键词把列表清空而不是返回全部。
func TestSearchIsCaseInsensitiveOnTitle(t *testing.T) {
	root := t.TempDir()
	writeAudioFile(t, root, "Alpha.mp3", 16)
	writeAudioFile(t, root, "beta.ogg", 16)
	writeAudioFile(t, root, "夜的第七章.flac", 16)

	library := newTestLibrary()
	if err := library.SetFolder(root); err != nil {
		t.Fatalf("设置音乐文件夹失败：%v", err)
	}

	if got := library.Search(""); len(got) != 3 {
		t.Fatalf("空关键词应返回全部曲目，得到 %d 首", len(got))
	}
	if got := library.Search("   "); len(got) != 3 {
		t.Fatalf("纯空白关键词应返回全部曲目，得到 %d 首", len(got))
	}
	if got := library.Search("ALPHA"); len(got) != 1 || filepath.Base(got[0].FilePath) != "Alpha.mp3" {
		t.Fatalf("大小写不敏感搜索失败：%v", trackPaths(got))
	}
	if got := library.Search("BETA"); len(got) != 1 {
		t.Fatalf("大写关键词应命中 beta.ogg，得到 %v", trackPaths(got))
	}
	// 中文没有大小写，这里防的是"只按扩展名/路径匹配"的实现走偏
	if got := library.Search("第七章"); len(got) != 1 {
		t.Fatalf("中文关键词应命中，得到 %v", trackPaths(got))
	}
	if got := library.Search("nope"); len(got) != 0 {
		t.Fatalf("无匹配时应返回空列表，得到 %v", trackPaths(got))
	}
}

// TestVolumeAndPlaybackModeRoundTrip 音量夹紧 0-100、播放模式非法值回落顺序播放。
// 防的回归：前端滑块拖到范围外把音量写成 -5/150（声音失真或彻底静音）后无法自愈，
// 以及配置里残留的旧模式字符串让播放模式显示成空。
func TestVolumeAndPlaybackModeRoundTrip(t *testing.T) {
	store := NewMemoryConfigStore()
	library := NewMusicLibrary(store)

	if volume := library.Volume(); volume != 80 {
		t.Fatalf("未配置时音量默认 80，得到 %d", volume)
	}
	for _, testCase := range []struct {
		set  int
		want int
	}{
		{-5, 0},
		{0, 0},
		{42, 42},
		{100, 100},
		{150, 100},
	} {
		library.SetVolume(testCase.set)
		if got := library.Volume(); got != testCase.want {
			t.Fatalf("设置音量 %d 后应夹紧为 %d，得到 %d", testCase.set, testCase.want, got)
		}
	}

	// 配置里是非数字时回落 80，而不是 0（0 会让用户以为静音坏了）
	store.SetValue(volumeKey, "loud")
	if volume := library.Volume(); volume != 80 {
		t.Fatalf("非法音量值应回落 80，得到 %d", volume)
	}

	if mode := library.PlaybackMode(); mode != ModeSequential {
		t.Fatalf("默认播放模式应为 Sequential，得到 %q", mode)
	}
	library.SetPlaybackMode(ModeShuffle)
	if mode := library.PlaybackMode(); mode != ModeShuffle {
		t.Fatalf("播放模式应持久化为 Shuffle，得到 %q", mode)
	}
	store.SetValue(playbackModeKey, "Random")
	if mode := library.PlaybackMode(); mode != ModeSequential {
		t.Fatalf("非法播放模式应回落 Sequential，得到 %q", mode)
	}
}

// TestMemoryConfigStoreHandlesZeroValue 没走构造函数的内存配置也能用（Get/Set 不 panic）。
// 防的回归：结构体零值（value 为 nil map）时 SetValue 直接 panic 拖垮整个服务。
func TestMemoryConfigStoreHandlesZeroValue(t *testing.T) {
	var store MemoryConfigStore
	if got := store.GetValue("missing"); got != "" {
		t.Fatalf("缺失键应返回空串，得到 %q", got)
	}
	store.SetValue("k", "v")
	if got := store.GetValue("k"); got != "v" {
		t.Fatalf("写入后应能读回，得到 %q", got)
	}

	// NewMusicLibrary(nil) 必须自带内存实现，不能因 nil 配置 panic
	library := NewMusicLibrary(nil)
	library.SetVolume(30)
	if volume := library.Volume(); volume != 30 {
		t.Fatalf("nil 配置应回落到内存实现，音量 = %d", volume)
	}
}

// TestTrackDisplayHelpers 曲目元数据/格式判断的显示口径。
// 防的回归：标题带扩展名、扩展名大写、大小单位换挡错误
// （MB 阈值写成 1000000）、时间格式串写错导致界面出现 "0001-01-01"。
func TestTrackDisplayHelpers(t *testing.T) {
	track := MusicTrack{
		FilePath:     `C:\music\Demo Song.MP3`,
		FileSize:     4404019, // 4.2 MB
		LastModified: time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC),
	}
	if title := track.Title(); title != "Demo Song" {
		t.Fatalf("标题应去掉扩展名，得到 %q", title)
	}
	if ext := track.Extension(); ext != ".mp3" {
		t.Fatalf("扩展名应小写，得到 %q", ext)
	}
	if display := track.ExtensionDisplay(); display != "mp3" {
		t.Fatalf("扩展名显示应去掉点，得到 %q", display)
	}
	if size := track.SizeDisplay(); size != "4.2 MB" {
		t.Fatalf("4.2MB 的显示不符：%q", size)
	}
	if meta := track.MetaDisplay(); !strings.HasPrefix(meta, "mp3 · ") {
		t.Fatalf("副标题应以扩展名开头：%q", meta)
	}
	if date := track.DateDisplay(); date != "2024-05-06 07:08" {
		t.Fatalf("时间显示格式不符：%q", date)
	}
	if str := track.String(); str != "Demo Song" {
		t.Fatalf("String() 应返回标题，得到 %q", str)
	}

	sizeCases := []struct {
		size int64
		want string
	}{
		{0, "0 B"},
		{1023, "1023 B"},
		{1024, "1 KB"},
		{1048575, "1024 KB"},
		{1048576, "1.0 MB"},
	}
	for _, testCase := range sizeCases {
		if got := (MusicTrack{FileSize: testCase.size}).SizeDisplay(); got != testCase.want {
			t.Errorf("大小 %d 字节显示为 %q，期望 %q", testCase.size, got, testCase.want)
		}
	}

	supported := []string{"a.mp3", "a.MP3", "a.WaV", "a.ogg", "a.flac", "a.aac", "a.m4a", "a.opus"}
	for _, name := range supported {
		if !IsSupported(name) {
			t.Errorf("%s 应被判定为支持的格式", name)
		}
	}
	// 注意：".mp3" 这种纯扩展名按 filepath.Ext 也是 ".mp3"，属于受支持，不列入反例
	unsupported := []string{"a.wma", "a.mp4", "a.txt", "", "a", "a."}
	for _, name := range unsupported {
		if IsSupported(name) {
			t.Errorf("%s 不应被判定为支持的格式", name)
		}
	}
}

func equalStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
