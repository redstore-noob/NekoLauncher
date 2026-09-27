package logs

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// 日志文件名的约定形状：时间戳 + .log，且不含冒号（否则 Windows 上文件名非法）。
var (
	logFileNamePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}_\d{2}-\d{2}-\d{2}\.log$`)
	logLinePattern     = regexp.MustCompile(`^\[\d{4}-\d{2}-\d{2}_\d{2}-\d{2}-\d{2}\]\[[A-Z]+\]\S*$`)
)

// useTempLogDirectory 把日志目录指到临时目录：
// logDirectory 由 USERPROFILE/HOME 推导，覆盖环境变量即可，生产代码无需为测试改动；
// 同时重置进程内共享路径，避免用例之间看到对方写下的日志。
// 返回日志目录的完整路径。
func useTempLogDirectory(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	resetSharedLogPath(t)
	t.Cleanup(func() { resetSharedLogPath(t) })
	return filepath.Join(home, "NekoLauncher", "Logs")
}

// resetSharedLogPath 清掉进程内共享日志路径（需持锁，与 Write 串行）。
func resetSharedLogPath(t *testing.T) {
	t.Helper()
	writeMu.Lock()
	sharedFilePath = ""
	writeMu.Unlock()
}

// currentLogFile 返回当前共享日志文件路径；尚未写入过时直接失败。
func currentLogFile(t *testing.T) string {
	t.Helper()
	writeMu.Lock()
	path := sharedFilePath
	writeMu.Unlock()
	if path == "" {
		t.Fatal("共享日志路径为空：本用例应先调用 Write")
	}
	return path
}

// logFilesIn 列举目录下匹配的文件（用于断言"只有一个日志文件"这类不变量）。
func logFilesIn(t *testing.T, dir, pattern string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		t.Fatalf("列举 %s 失败：%v", pattern, err)
	}
	return matches
}

// readText 读文本文件，失败即终止用例。
func readText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读文件失败 %s：%v", path, err)
	}
	return string(data)
}

// TestTimeGetIsSafeForFileNames 防的回归：时间戳格式改成带冒号的 "15:04:05"，
// Windows 上文件名非法，日志直接写不出去（且这个格式同时用于日志行前缀）。
func TestTimeGetIsSafeForFileNames(t *testing.T) {
	stamp := timeGet()
	if !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}_\d{2}-\d{2}-\d{2}$`).MatchString(stamp) {
		t.Fatalf("时间戳格式 = %q，期望 2006-01-02_15-04-05", stamp)
	}
	for _, illegal := range []string{":", "/", "\\", "*", "?", "\"", "<", ">", "|"} {
		if strings.Contains(stamp, illegal) {
			t.Fatalf("时间戳含 Windows 文件名非法字符 %q：%q", illegal, stamp)
		}
	}
}

// TestWriteCreatesDirectoryAndFileOnFirstWrite 防的回归：
// 首次写入时目录不存在导致 Write 返回 false，启动器第一屏就丢日志。
func TestWriteCreatesDirectoryAndFileOnFirstWrite(t *testing.T) {
	dir := useTempLogDirectory(t)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("前置条件不成立：%s 不该已存在", dir)
	}

	if !Write("INFO", "首条日志") {
		t.Fatal("首次写入应成功")
	}

	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Fatalf("日志目录未被自动创建：%v", err)
	}
	path := currentLogFile(t)
	if filepath.Dir(path) != dir {
		t.Fatalf("日志落在 %q，期望在 %q 下", path, dir)
	}
	if !logFileNamePattern.MatchString(filepath.Base(path)) {
		t.Fatalf("日志文件名 = %q，不符合 时间戳.log 约定", filepath.Base(path))
	}
	if content := readText(t, path); !strings.Contains(content, "首条日志") {
		t.Fatalf("日志内容 = %q，未包含写入的文本", content)
	}
}

// TestWriteAppendsInsteadOfOverwriting 防的回归：用 O_TRUNC 打开日志，
// 每次写入把之前的日志清空（用户导出日志时只剩最后一行）。
func TestWriteAppendsInsteadOfOverwriting(t *testing.T) {
	useTempLogDirectory(t)

	if !Write("INFO", "第一条") || !Write("WARN", "第二条") || !Write("INFO", "第三条") {
		t.Fatal("连续写入应全部成功")
	}
	path := currentLogFile(t)
	content := readText(t, path)

	first := strings.Index(content, "第一条")
	second := strings.Index(content, "第二条")
	third := strings.Index(content, "第三条")
	if first < 0 || second < 0 || third < 0 {
		t.Fatalf("追加写入丢内容：%q", content)
	}
	if !(first < second && second < third) {
		t.Fatalf("日志顺序错乱：%q", content)
	}
	if lines := strings.Split(strings.TrimRight(content, "\n"), "\n"); len(lines) != 3 {
		t.Fatalf("应有 3 行日志，实际 %d 行：%q", len(lines), content)
	}
}

// TestWriteKeepsSingleSharedFile 防的回归：每次写入都新建一个时间戳文件，
// 一次运行产生几十个日志文件，用户报障时找不到完整上下文。
func TestWriteKeepsSingleSharedFile(t *testing.T) {
	dir := useTempLogDirectory(t)

	for i := 0; i < 5; i++ {
		if !Write("INFO", "同一文件") {
			t.Fatalf("第 %d 次写入失败", i)
		}
	}
	files := logFilesIn(t, dir, "*.log")
	if len(files) != 1 {
		t.Fatalf("一次运行只应有一个日志文件，实际 %d 个：%v", len(files), files)
	}
	if files[0] != currentLogFile(t) {
		t.Fatalf("共享路径 %q 与磁盘上的 %q 不一致", currentLogFile(t), files[0])
	}
}

// TestWriteLineFormat 防的回归：行格式里少一个方括号或类型字段，
// 日志解析/筛选（用户与支持人员按 [ERROR] 检索）全部失效。
func TestWriteLineFormat(t *testing.T) {
	useTempLogDirectory(t)

	if !Write("ERROR", "测试行格式") {
		t.Fatal("写入失败")
	}
	content := strings.TrimRight(readText(t, currentLogFile(t)), "\n")
	if !logLinePattern.MatchString(content) {
		t.Fatalf("日志行 = %q，期望 [时间戳][类型]内容", content)
	}
	if !strings.HasSuffix(content, "][ERROR]测试行格式") {
		t.Fatalf("日志行 = %q，类型或内容位置不对", content)
	}
}

// TestInitWritesInitializationLine 防的回归：Init 只打印控制台不落盘，
// 启动器正常启动的日志文件里缺少"初始化成功"这条基准记录。
func TestInitWritesInitializationLine(t *testing.T) {
	useTempLogDirectory(t)

	Init()

	path := currentLogFile(t)
	content := readText(t, path)
	if !strings.Contains(content, "日志系统初始化成功") {
		t.Fatalf("初始化日志未落盘：%q", content)
	}
	if !strings.Contains(content, "][INFO]") {
		t.Fatalf("初始化日志类型应为 INFO：%q", content)
	}
}

// TestAddLogsCallsOnErrorForErrorType 防的回归：
// ERROR 级别日志不触发错误回调（界面/托盘不提示），以及普通日志误触发回调
// 导致每次写 INFO 都弹一次错误框。
func TestAddLogsCallsOnErrorForErrorType(t *testing.T) {
	useTempLogDirectory(t)

	calls := 0
	onError := func() { calls++ }

	if result := AddLogs("出错了", onError, "ERROR"); !result {
		t.Fatal("写入成功时 AddLogs 应返回 true")
	}
	if calls != 1 {
		t.Fatalf("ERROR 类型应触发一次错误回调，实际 %d 次", calls)
	}

	if result := AddLogs("一切正常", onError, "INFO"); !result {
		t.Fatal("写入成功时 AddLogs 应返回 true")
	}
	if calls != 1 {
		t.Fatalf("INFO 类型不应触发错误回调，实际累计 %d 次", calls)
	}
}

// TestAddLogsWriteFailureCallsOnError 防的回归：
// 写盘失败时既不返回 false 也不回调，日志静默丢失（用户报障时没有任何线索）。
// 这里让日志目录的父级变成文件，制造真实的 MkdirAll 失败。
func TestAddLogsWriteFailureCallsOnError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	resetSharedLogPath(t)
	t.Cleanup(func() { resetSharedLogPath(t) })

	// <home>/NekoLauncher 建成普通文件 → <home>/NekoLauncher/Logs 无法创建
	blocker := filepath.Join(home, "NekoLauncher")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("构造失败场景出错：%v", err)
	}

	if Write("INFO", "写不进去") {
		t.Fatal("目录无法创建时 Write 应返回 false")
	}

	calls := 0
	if result := AddLogs("写不进去", func() { calls++ }, "INFO"); result {
		t.Fatal("写入失败时 AddLogs 应返回 false")
	}
	if calls != 1 {
		t.Fatalf("写入失败应触发一次错误回调，实际 %d 次", calls)
	}
	// onError 为 nil 时不能 panic（调用方常常不关心）
	if result := AddLogs("写不进去", nil, "ERROR"); result {
		t.Fatal("写入失败时 AddLogs 应返回 false")
	}
	// 失败后共享路径应保持为空，ReadCurrent 不该去读一个不存在的文件而报错
	if content, err := ReadCurrent(); err != nil || content != "" {
		t.Fatalf("写入失败后 ReadCurrent = (%q, %v)，期望空串且无错误", content, err)
	}
}

// TestReadCurrentBeforeAnyWrite 防的回归：还没写过日志时 ReadCurrent
// 去读一个空路径，返回"文件不存在"错误，前端打开日志窗口就报错。
func TestReadCurrentBeforeAnyWrite(t *testing.T) {
	useTempLogDirectory(t)

	content, err := ReadCurrent()
	if err != nil {
		t.Fatalf("未写入时不应报错：%v", err)
	}
	if content != "" {
		t.Fatalf("未写入时应返回空串，实际 %q", content)
	}
}

// TestReadCurrentMissingFileReturnsEmpty 防的回归：
// 日志文件被外部删掉（用户手动清理）后 ReadCurrent 报错，而不是当作"暂无日志"。
func TestReadCurrentMissingFileReturnsEmpty(t *testing.T) {
	useTempLogDirectory(t)

	missing := filepath.Join(t.TempDir(), "gone.log")
	writeMu.Lock()
	sharedFilePath = missing
	writeMu.Unlock()

	content, err := ReadCurrent()
	if err != nil {
		t.Fatalf("文件不存在时不应报错：%v", err)
	}
	if content != "" {
		t.Fatalf("文件不存在时应返回空串，实际 %q", content)
	}

	// 其它读错误（这里用目录冒充日志文件）必须如实上报，不能吞成"空日志"
	directory := t.TempDir()
	writeMu.Lock()
	sharedFilePath = directory
	writeMu.Unlock()
	if _, err := ReadCurrent(); err == nil {
		t.Fatal("非「文件不存在」的读取错误应上报")
	}
}

// TestWriteRotatesWhenFileExceedsLimit 防的回归：
// 大小上限判断写错（不该轮转时轮转 / 该轮转时不轮转）导致单个日志文件无限增长，
// 或每次写入都轮转把日志切成无数碎片。
func TestWriteRotatesWhenFileExceedsLimit(t *testing.T) {
	dir := useTempLogDirectory(t)

	if !Write("INFO", "轮转前的第一条") {
		t.Fatal("写入失败")
	}
	current := currentLogFile(t)
	archived := strings.TrimSuffix(current, filepath.Ext(current)) + ".old.log"

	// 阶段一：文件刚好小于上限 → 不轮转，旧内容仍在
	if err := os.Truncate(current, maximumLogFileSizeBytes-1); err != nil {
		t.Fatalf("撑大日志文件失败：%v", err)
	}
	if !Write("INFO", "还没到上限") {
		t.Fatal("写入失败")
	}
	if len(logFilesIn(t, dir, "*.old.log")) != 0 {
		t.Fatal("未超过上限就轮转了：日志被无谓地切碎")
	}
	if content := readText(t, current); !strings.Contains(content, "还没到上限") {
		t.Fatalf("未轮转时新日志应写进同一个文件：%q", content)
	}

	// 阶段二：达到上限 → 轮转为 .old.log，新文件从零开始
	if err := os.Truncate(current, maximumLogFileSizeBytes); err != nil {
		t.Fatalf("撑大日志文件失败：%v", err)
	}
	if !Write("INFO", "轮转之后") {
		t.Fatal("写入失败")
	}

	info, err := os.Stat(archived)
	if err != nil {
		t.Fatalf("超过上限后应归档为 %s：%v", archived, err)
	}
	if info.Size() != maximumLogFileSizeBytes {
		t.Fatalf("归档大小 = %d，期望轮转前的 %d", info.Size(), maximumLogFileSizeBytes)
	}
	fresh := readText(t, current)
	if !strings.Contains(fresh, "轮转之后") {
		t.Fatalf("轮转后的新日志未写入新文件：%q", fresh)
	}
	if strings.Contains(fresh, "轮转前的第一条") {
		t.Fatalf("轮转后新文件里残留旧内容：%q", fresh)
	}
}

// TestRotateKeepsOnlyOneArchivedGeneration 防的回归：
// 每次轮转都新造一个 .old.log，日志目录无限膨胀（注释里的承诺是"只保留一代"）。
func TestRotateKeepsOnlyOneArchivedGeneration(t *testing.T) {
	dir := useTempLogDirectory(t)

	if !Write("INFO", "第一代") {
		t.Fatal("写入失败")
	}
	current := currentLogFile(t)

	for round := 0; round < 3; round++ {
		if err := os.Truncate(current, maximumLogFileSizeBytes); err != nil {
			t.Fatalf("第 %d 轮撑大文件失败：%v", round, err)
		}
		if !Write("INFO", "轮转") {
			t.Fatalf("第 %d 轮写入失败", round)
		}
	}

	if files := logFilesIn(t, dir, "*.old.log"); len(files) != 1 {
		t.Fatalf("归档只应保留一代，实际 %d 个：%v", len(files), files)
	}
	if files := logFilesIn(t, dir, "*.log"); len(files) != 2 {
		t.Fatalf("目录里应只有 1 个当前日志 + 1 个归档，实际 %d 个：%v", len(files), files)
	}
}

// TestClearLogsRemovesOnlyLogFilesAndResetsPath 防的回归：
// 清空日志时把目录里的非日志文件一并删掉（配置文件、导出文件被误删），
// 以及清空后共享路径没重置，后续日志继续往已删除的文件句柄路径追加（写进虚空）。
func TestClearLogsRemovesOnlyLogFilesAndResetsPath(t *testing.T) {
	dir := useTempLogDirectory(t)

	if !Write("INFO", "清空前的日志") {
		t.Fatal("写入失败")
	}
	for _, name := range []string{"1999-01-01_00-00-00.log", "1999-01-01_00-00-00.old.log", "keep.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("造文件 %s 失败：%v", name, err)
		}
	}

	deleted := ClearLogs()
	if deleted != 3 {
		t.Fatalf("应删除 3 个 .log 文件，实际 %d", deleted)
	}
	if files := logFilesIn(t, dir, "*.log"); len(files) != 0 {
		t.Fatalf("清空后仍有日志文件：%v", files)
	}
	if _, err := os.Stat(filepath.Join(dir, "keep.txt")); err != nil {
		t.Fatalf("非日志文件不应被删除：%v", err)
	}
	if path := currentLogFileOrEmpty(); path != "" {
		t.Fatalf("清空后共享路径应被重置，实际仍为 %q", path)
	}

	// 清空后继续写入：按当前时刻新建文件，而不是往已删除的旧路径追加
	if !Write("INFO", "清空后的日志") {
		t.Fatal("清空后写入应成功")
	}
	files := logFilesIn(t, dir, "*.log")
	if len(files) != 1 {
		t.Fatalf("清空后应新建 1 个日志文件，实际 %d 个：%v", len(files), files)
	}
	content := readText(t, files[0])
	if !strings.Contains(content, "清空后的日志") {
		t.Fatalf("清空后的日志未落盘：%q", content)
	}
	if strings.Contains(content, "清空前的日志") {
		t.Fatalf("清空前的日志又出现了：%q", content)
	}
}

// currentLogFileOrEmpty 读取共享路径（允许为空，用于断言"已重置"）。
func currentLogFileOrEmpty() string {
	writeMu.Lock()
	defer writeMu.Unlock()
	return sharedFilePath
}

// TestClearLogsWithoutDirectoryReturnsZero 防的回归：
// 日志目录还不存在时 ClearLogs 返回 -1（异常）或 panic，前端提示"清空失败"，
// 而实际只是没有日志可清。
func TestClearLogsWithoutDirectoryReturnsZero(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	resetSharedLogPath(t)
	t.Cleanup(func() { resetSharedLogPath(t) })

	if _, err := os.Stat(filepath.Join(home, "NekoLauncher", "Logs")); !os.IsNotExist(err) {
		t.Fatal("前置条件不成立：日志目录不该存在")
	}
	if deleted := ClearLogs(); deleted != 0 {
		t.Fatalf("目录不存在时应返回 0，实际 %d", deleted)
	}
}

// TestExportCurrentCopiesLogFile 防的回归：导出的是空文件或旧文件，
// 用户点"导出日志"拿到的东西跟界面上看到的不是同一份。
func TestExportCurrentCopiesLogFile(t *testing.T) {
	useTempLogDirectory(t)
	if !Write("INFO", "导出内容") {
		t.Fatal("写入失败")
	}
	current := readText(t, currentLogFile(t))

	destination := filepath.Join(t.TempDir(), "exported.log")
	if err := ExportCurrent(destination); err != nil {
		t.Fatalf("导出失败：%v", err)
	}
	if exported := readText(t, destination); exported != current {
		t.Fatalf("导出内容与当前日志不一致：\n导出=%q\n当前=%q", exported, current)
	}
}

// TestExportCurrentRejectsEmptyAndBadDestination 防的回归：
// 空路径被当成文件名写出（当前目录下凭空多一个文件），
// 以及目标不可写时静默返回 nil，用户以为导出成功了。
func TestExportCurrentRejectsEmptyAndBadDestination(t *testing.T) {
	useTempLogDirectory(t)
	if !Write("INFO", "内容") {
		t.Fatal("写入失败")
	}

	for _, dest := range []string{"", "   ", "\t"} {
		err := ExportCurrent(dest)
		if err == nil {
			t.Fatalf("空路径 %q 应返回错误", dest)
		}
		if !strings.Contains(err.Error(), "导出路径不能为空") {
			t.Fatalf("空路径的错误信息应可读，实际 %q", err.Error())
		}
	}

	// 目标是一个目录 → 写文件必然失败，必须上报
	if err := ExportCurrent(t.TempDir()); err == nil {
		t.Fatal("目标是目录时应返回错误")
	}

	// 目标父目录不存在 → 同样必须上报（而不是静默成功）
	if err := ExportCurrent(filepath.Join(t.TempDir(), "no-such-dir", "a.log")); err == nil {
		t.Fatal("父目录不存在时应返回错误")
	}
}

// TestWriteHandlesOversizedAndInvalidContent 防的回归：
// 超大单行或含非法 UTF-8 / 控制字符的内容让 Write panic 或截断，
// 一处异常内容把整条日志链断掉（Java 崩溃日志里这类字节很常见）。
func TestWriteHandlesOversizedAndInvalidContent(t *testing.T) {
	useTempLogDirectory(t)

	huge := strings.Repeat("A", 1<<20)
	if !Write("INFO", huge) {
		t.Fatal("超大行写入应成功")
	}

	weird := "换行\n回车\r制表\t空字节\x00无效UTF8\xff\xfe"
	if !Write("ERROR", weird) {
		t.Fatal("非法内容写入应成功")
	}

	content := readText(t, currentLogFile(t))
	if !strings.Contains(content, huge) {
		t.Fatal("超大行被截断或改写")
	}
	if !strings.Contains(content, weird) {
		t.Fatal("非法内容被截断或改写（日志必须按字节原样落盘）")
	}
}

// TestWriteIsSafeUnderConcurrentWriters 防的回归：
// 并发写入（游戏进程日志转发 + 后台下载 + 界面操作同时发生）时行与行互相穿插，
// 生成读不出来的半截日志行。
func TestWriteIsSafeUnderConcurrentWriters(t *testing.T) {
	useTempLogDirectory(t)

	const writers = 20
	var waitGroup sync.WaitGroup
	waitGroup.Add(writers)
	for index := 0; index < writers; index++ {
		go func(index int) {
			defer waitGroup.Done()
			if !Write("INFO", "并发-"+strings.Repeat("x", index%5)) {
				t.Errorf("并发写入失败：%d", index)
			}
		}(index)
	}
	waitGroup.Wait()

	content := readText(t, currentLogFile(t))
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	if len(lines) != writers {
		t.Fatalf("应有 %d 行日志，实际 %d 行（写入丢失或多出空行）", writers, len(lines))
	}
	for _, line := range lines {
		if !logLinePattern.MatchString(line) {
			t.Fatalf("日志行被并发写入撕裂：%q", line)
		}
	}
}
