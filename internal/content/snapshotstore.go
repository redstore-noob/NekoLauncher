// 快照引擎（Rewind）：把任意目标目录的多次状态串成时间线。
//
// 目前接入两类目标（见 snapshotKind）：
//   - save：Minecraft 世界存档目录；
//   - instance：实例的游戏目录（.minecraft 式根目录）。
//
// 采用文件级内容寻址（类似 Git）：文件内容按 SHA-256 去重落在**全局** blobs/，
// 每个快照是一份「相对路径 → blob 哈希」清单；未变的文件不重复占盘，不同目标之间
// 相同的内容（库文件、资源索引、重复的存档区块）也只存一份。
// 快照本身不可变，回滚时把 blob 复制回目标目录，避免被游戏原地写坏。
//
// 仓库布局（位于启动器存储目录，不污染游戏目录）：
//
//	<storage>/rewind/blobs/<aa>/<sha256>              全局共享数据块
//	<storage>/rewind/repos/<repoKey>/manifests/<id>.json
//
// repoKey 由目标目录绝对路径派生（见 snapshotRepoKey），两类目标共用一个命名空间。
// 旧版布局（save-snapshots/<key>/{blobs,manifests}，blob 按 repo 隔离）在每次仓库
// 操作前自动迁移，见 migrateLegacySnapshotStore。
package content

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"nekolauncher/internal/config"
)

const (
	// snapshotRootName 仓库根目录名（位于存储目录下）。
	snapshotRootName = "rewind"
	// snapshotBlobsDirName 全局内容寻址数据块目录（相对仓库根）。
	snapshotBlobsDirName = "blobs"
	// snapshotReposDirName 各目标清单仓库的父目录（相对仓库根）。
	snapshotReposDirName = "repos"
	// snapshotManifestDirName 快照清单目录（相对 repo）。
	snapshotManifestDirName = "manifests"
	// legacySnapshotRootName 旧版仓库根目录名（v1 布局，迁移成功后删除）。
	legacySnapshotRootName = "save-snapshots"
	// 快照来源标记。
	snapshotReasonManual         = "manual"
	snapshotReasonBeforeRollback = "before-rollback"
	// snapshotReasonBeforeLaunch 启动前自动快照（时间机器的"还原点"）。
	//
	// 与 before-rollback 的区别很重要：before-rollback 是"用户马上要回滚，
	// 先兜个底"，属于稀有事件；before-launch 每次启动都产生一个，是高频事件。
	// 因此它必须计入 snapshotMaxAutoSnapshots 的淘汰口径，否则每次启动留一个，
	// 几天就能把盘吃满（历史实现只对 before-rollback 计数，新增来源必须一起算）。
	snapshotReasonBeforeLaunch = "before-launch"
	// 自动安全快照的默认备注。
	snapshotSafetyLabel = "回滚前自动快照"
	// snapshotLaunchLabel 启动前自动快照的默认备注。
	snapshotLaunchLabel = "启动前自动快照"

	// snapshotMaxAutoSnapshots 自动安全快照（回滚前的兜底快照）保留上限。
	// 每次回滚都会产生一个，反复回滚会把盘吃满，所以只留最近这些个。
	snapshotMaxAutoSnapshots = 10
	// snapshotMaxTotalBytes 单仓库数据块体积预算（4 GiB）。超过时按
	// "先自动快照、后无标记手动快照"的顺序淘汰最旧的，**带用户标记的快照永不自动删除**。
	// blob 已全局共享：两个仓库引用同一块时两边都计入 AddedSize，
	// 所以这是"每仓库增量"的近似上界，不是全局磁盘占用。
	snapshotMaxTotalBytes int64 = 4 << 30
	// snapshotRestoreSuffix 回滚时"先恢复到临时目录"用的目录后缀。
	// 目录名以点开头：它和目标目录同级（改名必须同卷），这样存档列表/打包逻辑
	// 按惯例跳过点目录，进程中断留下的残骸也不会被当成一个世界。
	snapshotRestoreSuffix = ".nya-restore-"
	// snapshotBackupSuffix 原子替换时把原目标目录挪到这里的后缀。
	snapshotBackupSuffix = ".nya-rollback-backup-"
)

// snapshotKind 快照目标类型。清单里记录该字段以便区分历史数据；空串按 save 处理。
type snapshotKind string

const (
	snapshotKindSave     snapshotKind = "save"
	snapshotKindInstance snapshotKind = "instance"
)

// snapshotColors 快照标记颜色允许值：语义色键（随明暗主题自动适配），
// 空串表示未标记（手动快照为主题色、安全快照为琥珀色）。
var snapshotColors = map[string]bool{
	"":          true,
	"primary":   true,
	"secondary": true,
	"success":   true,
	"warning":   true,
	"danger":    true,
}

// normalizeSnapshotColor 校验标记颜色，非法值回退为未标记。
func normalizeSnapshotColor(color string) string {
	color = strings.ToLower(strings.TrimSpace(color))
	if !snapshotColors[color] {
		return ""
	}
	return color
}

// instanceIgnoredTopDirs 实例快照跳过的顶层目录：体积大且启动器能重新下载、
// 或随时可再生的内容（库文件、资源索引、日志、崩溃报告、缓存）。
// 快照与回滚都绕开它们——回滚时这些目录被整体搬进新目录原样保留
// （见 restoreSnapshotAtomically），不会因为"不在清单里"而被删掉。
var instanceIgnoredTopDirs = map[string]bool{
	"libraries":     true,
	"assets":        true,
	"logs":          true,
	"crash-reports": true,
	"cache":         true,
}

// snapshotIgnores 判断一个（正斜杠）相对路径是否不参与快照。
// isDir 为 true 时调用方应跳过整个子树（WalkDir 返回 SkipDir）。
func snapshotIgnores(kind snapshotKind, relative string, isDir bool) bool {
	// 会话锁是运行期临时文件，任何层级出现都不参与快照
	base := relative
	if index := strings.LastIndexByte(relative, '/'); index >= 0 {
		base = relative[index+1:]
	}
	if strings.EqualFold(base, sessionLockName) {
		return true
	}
	if kind != snapshotKindInstance {
		return false
	}
	top := relative
	if index := strings.IndexByte(relative, '/'); index >= 0 {
		top = relative[:index]
	}
	if instanceIgnoredTopDirs[strings.ToLower(top)] {
		return true
	}
	// versions/<id>/natives[-<hash>]/ 在每次启动时重新解压，跳过。
	// 只匹配 natives / natives- 前缀的完整段，避免误伤恰好同名的用户文件。
	for _, segment := range strings.Split(relative, "/") {
		lower := strings.ToLower(segment)
		if lower == "natives" || strings.HasPrefix(lower, "natives-") {
			return true
		}
	}
	return false
}

// preservedTopLevelNames(kind) 返回回滚"整目录换名"时需要单独搬走保留的顶层目录名：
// 即该目标类型被快照跳过的顶层目录。save 目标没有保留集，回滚行为与旧版逐字节一致。
func preservedTopLevelNames(kind snapshotKind) map[string]bool {
	if kind != snapshotKindInstance {
		return map[string]bool{}
	}
	return instanceIgnoredTopDirs
}

// SaveSnapshotFile 快照清单里的单个文件（相对目标目录）。
type SaveSnapshotFile struct {
	// Path 目标内相对路径（正斜杠）。
	Path string
	// Hash SHA-256 十六进制（内容寻址键）。
	Hash string
	// Size 原始字节数。
	Size int64
	// ModTime Unix 纳秒；既是"下次快照跳过未变文件"的依据，
	// 也是启动前变更检测的基准（见 launchSnapshotBaseline）。
	ModTime int64
}

// SaveSnapshot 单个快照的元数据（时间线一项）。存档与实例快照共用同一结构。
type SaveSnapshot struct {
	Id        string
	CreatedAt time.Time
	Label     string
	// Reason 来源：manual（手动）/ before-rollback（回滚前自动）。
	Reason string
	// FileCount 快照包含的文件数。
	FileCount int
	// TotalSize 快照全部文件原始大小之和。
	TotalSize int64
	// AddedSize 相对上一快照新写入的数据块字节（近似新增占盘）。
	AddedSize int64
	// AddedFiles 相对上一快照发生变化的文件数。
	AddedFiles int
	// Color 用户标记的颜色（语义色键；空串表示未标记）。
	Color string
	// WorldName 目标目录名（展示用）。
	WorldName string
}

// saveSnapshotManifest 落盘清单。字段沿用 PascalCase，与 Wails 生成模型一致。
type saveSnapshotManifest struct {
	Id         string
	CreatedAt  time.Time
	Label      string
	Reason     string
	Color      string
	Kind       string
	FileCount  int
	TotalSize  int64
	AddedSize  int64
	AddedFiles int
	WorldName  string
	Files      []SaveSnapshotFile
}

var (
	snapshotLocksMu sync.Mutex
	snapshotLocks   = map[string]*sync.Mutex{}
)

// lockSnapshotRepo 串行化同一目标的快照操作，避免并发写坏仓库。
func lockSnapshotRepo(directory string) func() {
	key := snapshotRepoKey(directory)

	snapshotLocksMu.Lock()
	lock, ok := snapshotLocks[key]
	if !ok {
		lock = &sync.Mutex{}
		snapshotLocks[key] = lock
	}
	snapshotLocksMu.Unlock()

	lock.Lock()
	return lock.Unlock
}

// snapshotRootDirectory 仓库根目录。
func snapshotRootDirectory() string {
	return filepath.Join(config.StorageDirectory(), snapshotRootName)
}

// snapshotBlobsDirectory 全局数据块目录。
func snapshotBlobsDirectory() string {
	return filepath.Join(snapshotRootDirectory(), snapshotBlobsDirName)
}

// snapshotReposRootDirectory 各目标清单仓库的父目录。
func snapshotReposRootDirectory() string {
	return filepath.Join(snapshotRootDirectory(), snapshotReposDirName)
}

// snapshotRepoKey 由目标目录绝对路径派生的稳定仓库键（Windows 忽略大小写）。
func snapshotRepoKey(directory string) string {
	normalized := trimEndingSep(mustAbsPath(directory))
	if runtime.GOOS == "windows" {
		normalized = strings.ToLower(normalized)
	}
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])[:24]
}

// snapshotRepoDirectory 某个目标的清单仓库目录。
func snapshotRepoDirectory(directory string) string {
	return filepath.Join(
		snapshotReposRootDirectory(),
		snapshotRepoKey(directory),
	)
}

// ---- 旧布局迁移 ----

// legacyMigrationMu 串行化迁移检查；迁移本体幂等，重复执行无害。
var legacyMigrationMu sync.Mutex

// ensureLegacySnapshotStoreMigrated 把旧版（save-snapshots/，blob 按 repo 隔离）布局
// 迁到当前布局。所有仓库入口在触碰磁盘前都要先调它——进程内不允许"新旧并存"地读。
// 旧根不存在时只是一次 Stat；迁移失败不阻塞新快照（旧快照暂不可见但绝不丢失，
// 下次操作会自动重试）。
func ensureLegacySnapshotStoreMigrated() {
	legacyMigrationMu.Lock()
	defer legacyMigrationMu.Unlock()
	_ = migrateLegacySnapshotStore()
}

// migrateLegacySnapshotStore 执行一次迁移。内容寻址保证同名即同内容，
// blob 并入全局库时目标已存在就直接丢弃源文件。
func migrateLegacySnapshotStore() error {
	legacyRoot := filepath.Join(config.StorageDirectory(), legacySnapshotRootName)
	if _, err := os.Stat(legacyRoot); err != nil {
		return nil // 没有旧库（全新安装或已迁完）
	}
	if err := os.MkdirAll(snapshotBlobsDirectory(), 0o755); err != nil {
		return err
	}
	reposRoot := snapshotReposRootDirectory()
	if err := os.MkdirAll(reposRoot, 0o755); err != nil {
		return err
	}

	entries, err := os.ReadDir(legacyRoot)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		key := entry.Name()

		// 清单：优先整目录搬（同卷 rename，父目录须已存在）；目标已存在
		// （上次迁移中断）或换名失败时逐文件补齐
		legacyManifests := filepath.Join(legacyRoot, key, snapshotManifestDirName)
		targetManifests := filepath.Join(reposRoot, key, snapshotManifestDirName)
		if _, err := os.Stat(legacyManifests); err == nil {
			if err := os.MkdirAll(filepath.Dir(targetManifests), 0o755); err != nil {
				return err
			}
			if err := os.Rename(legacyManifests, targetManifests); err != nil {
				leftovers, err := os.ReadDir(legacyManifests)
				if err != nil {
					return err
				}
				for _, leftover := range leftovers {
					source := filepath.Join(legacyManifests, leftover.Name())
					if err := os.Rename(source, filepath.Join(targetManifests, leftover.Name())); err != nil &&
						!os.IsExist(err) {
						return err
					}
				}
			}
		}

		// 数据块并入全局库
		legacyBlobs := filepath.Join(legacyRoot, key, snapshotBlobsDirName)
		if err := mergeLegacyBlobs(legacyBlobs, snapshotBlobsDirectory()); err != nil {
			return err
		}
	}
	return os.RemoveAll(legacyRoot)
}

// mergeLegacyBlobs 把旧库的按-repo 数据块并入全局库。
func mergeLegacyBlobs(legacyBlobs, globalBlobs string) error {
	buckets, err := os.ReadDir(legacyBlobs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, bucket := range buckets {
		if !bucket.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(legacyBlobs, bucket.Name()))
		if err != nil {
			continue
		}
		for _, file := range files {
			source := filepath.Join(legacyBlobs, bucket.Name(), file.Name())
			target := filepath.Join(globalBlobs, bucket.Name(), file.Name())
			if _, err := os.Stat(target); err == nil {
				_ = os.Remove(source)
				continue
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.Rename(source, target); err != nil {
				return err
			}
		}
	}
	return nil
}

// normalizeSnapshotDirectory 校验并归一化快照目标目录。
func normalizeSnapshotDirectory(directory string) (string, error) {
	if strings.TrimSpace(directory) == "" {
		return "", errors.New("参数无效")
	}
	normalized := trimEndingSep(mustAbsPath(directory))
	info, err := os.Stat(normalized)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf(
			"目标目录不存在：%s（若刚执行过回滚，请检查目标旁边是否有 %s 开头的目录）",
			directory,
			snapshotBackupSuffix,
		)
	}
	return normalized, nil
}

// ListSaveSnapshots 列出某个存档的全部快照（新的在前）。
func ListSaveSnapshots(worldDirectory string) []SaveSnapshot {
	return listSnapshots(worldDirectory)
}

// ListInstanceSnapshots 列出某个实例游戏目录的全部快照（新的在前）。
func ListInstanceSnapshots(instanceDirectory string) []SaveSnapshot {
	return listSnapshots(instanceDirectory)
}

func listSnapshots(directory string) []SaveSnapshot {
	normalized, err := normalizeSnapshotDirectory(directory)
	if err != nil {
		return []SaveSnapshot{}
	}
	ensureLegacySnapshotStoreMigrated()
	return listSnapshotsLocked(snapshotRepoDirectory(normalized))
}

func listSnapshotsLocked(repo string) []SaveSnapshot {
	manifests, err := loadSnapshotManifests(repo)
	if err != nil || len(manifests) == 0 {
		return []SaveSnapshot{}
	}
	result := make([]SaveSnapshot, 0, len(manifests))
	for _, manifest := range manifests {
		result = append(result, snapshotSummary(manifest))
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].Id > result[j].Id
		}
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result
}

// CreateSaveSnapshot 给世界当前状态创建一个快照，返回快照元数据。
// color 为可选的标记颜色（语义色键，见 snapshotColors）。
func CreateSaveSnapshot(
	ctx context.Context,
	worldDirectory, label, color string,
) (SaveSnapshot, error) {
	return createSnapshot(
		ctx, snapshotKindSave, worldDirectory, label, color)
}

// CreateInstanceSnapshot 给实例当前状态创建一个快照。
// 调用方须保证游戏未在运行（bindings 层有守卫）：运行中创建会把"写了一半"的
// 状态定格进时间线。
func CreateInstanceSnapshot(
	ctx context.Context,
	instanceDirectory, label, color string,
) (SaveSnapshot, error) {
	return createSnapshot(
		ctx, snapshotKindInstance, instanceDirectory, label, color)
}

func createSnapshot(
	ctx context.Context,
	kind snapshotKind,
	directory, label, color string,
) (SaveSnapshot, error) {
	return createSnapshotWithReason(
		ctx, kind, directory, label, snapshotReasonManual, color)
}

// createSnapshotWithReason 与 createSnapshot 相同，区别是来源标记在创建时就写进清单。
//
// 来源不是装饰：淘汰逻辑按它区分自动快照与手动快照。先建了再改写（旧实现里
// CreateLaunchSnapshot 的做法）会让创建时那一次淘汰看不到真实来源——新快照不计入
// 自动名额，自动快照上限实际会停在"上限+1"（多留一份还原点就是多占一份存档的盘）。
func createSnapshotWithReason(
	ctx context.Context,
	kind snapshotKind,
	directory, label, reason, color string,
) (SaveSnapshot, error) {
	normalized, err := normalizeSnapshotDirectory(directory)
	if err != nil {
		return SaveSnapshot{}, err
	}

	unlock := lockSnapshotRepo(normalized)
	defer unlock()

	return createSnapshotLocked(
		ctx, kind, normalized, label, reason, normalizeSnapshotColor(color))
}

// createSnapshotLocked 在持有仓库锁的前提下创建快照。
func createSnapshotLocked(
	ctx context.Context,
	kind snapshotKind,
	directory, label, reason, color string,
) (SaveSnapshot, error) {
	ensureLegacySnapshotStoreMigrated()

	repo := snapshotRepoDirectory(directory)
	blobsDir := snapshotBlobsDirectory()
	manifestsDir := filepath.Join(repo, snapshotManifestDirName)
	if err := os.MkdirAll(blobsDir, 0o755); err != nil {
		return SaveSnapshot{}, err
	}
	if err := os.MkdirAll(manifestsDir, 0o755); err != nil {
		return SaveSnapshot{}, err
	}

	previous := latestSnapshotManifest(repo)
	previousIndex := map[string]SaveSnapshotFile{}
	if previous != nil {
		for _, file := range previous.Files {
			previousIndex[file.Path] = file
		}
	}

	files := make([]SaveSnapshotFile, 0, len(previousIndex)+16)
	createdBlobs := map[string]bool{}
	var totalSize, addedSize int64
	var addedFiles int

	// 跨仓库的 GC 安全：blob 先落盘、清单后落盘，两步之间该 blob 在任何清单里都无
	// 引用。本仓库锁管不到别的仓库正在进行的全局 GC，所以创建期间把新 blob "钉住"，
	// GC 遇到被钉住的哈希一律跳过。
	var blobPins []func()
	defer func() {
		for _, unpin := range blobPins {
			unpin()
		}
	}()

	walkErr := filepath.WalkDir(directory, func(
		path string,
		entry fs.DirEntry,
		err error,
	) error {
		if err != nil {
			return err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		relative, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if snapshotIgnores(kind, relative, entry.IsDir()) {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}

		// 变更检测：内容哈希是唯一可信的判据。
		//
		// 曾经用"大小 + mtime 都没变"来复用上一次的哈希省一次读盘，但同一文件系统
		// 时间粒度内、大小又恰好不变的改动（例如游戏覆盖写同尺寸的区块）会被判成
		// "没变"，那次改动就永久丢了——快照的全部价值就是"能回到过去"，这里不能省。
		// 代价是每次快照要完整读一遍目标目录（SHA-256 流式读，通常几百 MB/s）。
		hash, err := hashFileSHA256(path)
		if err != nil {
			return err
		}

		if !snapshotBlobExists(blobsDir, hash) {
			blobPins = append(blobPins, pinSnapshotBlob(hash))
			if err := storeSnapshotBlob(ctx, path, blobsDir, hash); err != nil {
				return err
			}
			if !createdBlobs[hash] {
				addedSize += info.Size()
			}
		}
		createdBlobs[hash] = true

		if previousFile, ok := previousIndex[relative]; !ok ||
			previousFile.Hash != hash {
			addedFiles++
		}

		totalSize += info.Size()
		files = append(files, SaveSnapshotFile{
			Path:    relative,
			Hash:    hash,
			Size:    info.Size(),
			ModTime: info.ModTime().UnixNano(),
		})
		return nil
	})
	if walkErr != nil {
		return SaveSnapshot{}, walkErr
	}

	sort.SliceStable(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})

	now := time.Now()
	manifest := saveSnapshotManifest{
		Id:         newSnapshotID(manifestsDir),
		CreatedAt:  now,
		Label:      strings.TrimSpace(label),
		Reason:     reason,
		Color:      normalizeSnapshotColor(color),
		Kind:       string(kind),
		FileCount:  len(files),
		TotalSize:  totalSize,
		AddedSize:  addedSize,
		AddedFiles: addedFiles,
		WorldName:  filepath.Base(directory),
		Files:      files,
	}
	if err := writeSnapshotManifest(repo, manifest); err != nil {
		return SaveSnapshot{}, err
	}
	// 护栏：快照不能无限增长。先按数量收紧自动安全快照，再按体积预算淘汰最旧的无标记快照
	// （带用户标记的快照永不自动删除，刚创建的这个也不会被自己触发的清理删掉）。
	if err := pruneSnapshotsLocked(repo, manifest.Id); err != nil {
		return SaveSnapshot{}, err
	}

	return snapshotSummary(manifest), nil
}

// pruneSnapshotsLocked 按数量与体积预算清理旧快照（调用方需已持仓库锁）。
// keepId 是"本次刚创建的快照"，永不参与淘汰。
//
// 顺序与理由：
//  1. 自动安全快照（每次回滚都会生成）只保留最近 snapshotMaxAutoSnapshots 个；
//  2. 仍超出 snapshotMaxTotalBytes 时，从最旧的开始淘汰：先自动快照，
//     再无标记手动快照；
//  3. **带用户标记（Label 或 Color 非空）的快照一律不删**——那是用户明确要留住的东西，
//     宁可超出预算也不能替他做主。
//
// 淘汰数据块走与删除快照相同的全局 GC 路径，不会留下无主 blob。
func pruneSnapshotsLocked(repo, keepId string) error {
	manifests, err := loadSnapshotManifests(repo)
	if err != nil {
		return err
	}
	if len(manifests) == 0 {
		return nil
	}

	// 新的在前（与时间线展示一致），便于"保留最近 N 个"
	sort.SliceStable(manifests, func(left, right int) bool {
		if manifests[left].CreatedAt.Equal(manifests[right].CreatedAt) {
			return manifests[left].Id > manifests[right].Id
		}

		return manifests[left].CreatedAt.After(manifests[right].CreatedAt)
	})

	remove := map[string]bool{}
	autoKept := 0
	for _, manifest := range manifests {
		if !isAutomaticSnapshotReason(manifest.Reason) {
			continue
		}
		// 刚创建的那份同样占一个名额：把它排除在计数外，"上限"实际会停在
		// 上限+1 份，与注释和测试断言的上限都对不上（多留一份还原点 = 多占
		// 一份存档大小的盘）。但它自己永不入选淘汰名单——正常情况下它按时间
		// 最新、本来就轮不到，只有时钟回拨把它排到老的那一侧时这个豁免才生效。
		autoKept++
		if manifest.Id == keepId {
			continue
		}
		if autoKept > snapshotMaxAutoSnapshots {
			remove[manifest.Id] = true
		}
	}

	// 体积预算：AddedSize 之和近似等于该仓库的增量占盘（每个快照只记录新增块）
	totalBytes := int64(0)
	for _, manifest := range manifests {
		if !remove[manifest.Id] {
			totalBytes += manifest.AddedSize
		}
	}
	if totalBytes > snapshotMaxTotalBytes {
		// 从最旧的一端淘汰：先自动快照，再无标记手动快照
		for _, pass := range []bool{true, false} {
			for index := len(manifests) - 1; index >= 0 && totalBytes > snapshotMaxTotalBytes; index-- {
				manifest := manifests[index]
				if remove[manifest.Id] || manifest.Id == keepId || isProtectedSnapshot(manifest) {
					continue
				}
				if isAutomaticSnapshotReason(manifest.Reason) != pass {
					continue
				}
				remove[manifest.Id] = true
				totalBytes -= manifest.AddedSize
			}
		}
	}

	if len(remove) == 0 {
		return nil
	}

	removed := 0
	for id := range remove {
		manifestPath := filepath.Join(repo, snapshotManifestDirName, id+".json")
		if err := os.Remove(manifestPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		removed++
	}
	if removed == 0 {
		return nil
	}

	return garbageCollectSnapshotBlobs()
}

// isProtectedSnapshot 带用户标记的快照（备注或颜色）不参与自动淘汰。
func isProtectedSnapshot(manifest saveSnapshotManifest) bool {
	return strings.TrimSpace(manifest.Label) != "" || strings.TrimSpace(manifest.Color) != ""
}

// isAutomaticSnapshotReason 该来源是否为"系统自动创建"的快照。
//
// 自动快照共享一个保留上限（snapshotMaxAutoSnapshots）与同一套淘汰优先级。
// **新增自动来源时必须登记在这里**，否则它在 count 上限与体积淘汰两条路径上
// 都会被当成"手动快照"而永不清理——一个高频自动快照足以把磁盘写满。
func isAutomaticSnapshotReason(reason string) bool {
	switch reason {
	case snapshotReasonBeforeRollback, snapshotReasonBeforeLaunch:
		return true
	}
	return false
}

// CreateLaunchSnapshot 在启动游戏前创建"还原点"快照（时间机器的自动刻度）。
//
// 与手动快照的差别：
//   - 来源标为 before-launch（计入自动快照上限，会被正常淘汰）；
//   - **已存在内容相同的快照时跳过**：玩家连续启动十次、存档没变，
//     不该留下十个一模一样的还原点（见 shouldCreateLaunchSnapshot）。
//
// 返回 (快照, 是否真的创建了)。跳过时快照为零值、第二返回值为 false。
func CreateLaunchSnapshot(
	ctx context.Context,
	worldDirectory string,
) (SaveSnapshot, bool, error) {
	if strings.TrimSpace(worldDirectory) == "" {
		return SaveSnapshot{}, false, nil
	}
	if !shouldCreateLaunchSnapshot(worldDirectory) {
		return SaveSnapshot{}, false, nil
	}
	snapshot, err := createSnapshotWithReason(
		ctx,
		snapshotKindSave,
		worldDirectory,
		snapshotLaunchLabel,
		snapshotReasonBeforeLaunch,
		"",
	)
	if err != nil {
		return SaveSnapshot{}, false, err
	}

	return snapshot, true, nil
}

// shouldCreateLaunchSnapshot 判断是否需要为本次启动留还原点。
//
// 跳过条件：最近一个快照与当前内容一致（没有变化就没有还原价值）。
// 这里用"最近快照之后目标目录是否被修改过"作为廉价的近似判断：
// 逐个文件哈希代价太高，而"没改过文件"是最常见的连续启动场景。
func shouldCreateLaunchSnapshot(worldDirectory string) bool {
	normalized, err := normalizeSnapshotDirectory(worldDirectory)
	if err != nil {
		// 目录不可用（不存在/不是目录）：按"需要创建"处理，
		// 让 createSnapshot 去报出真实错误，与"一份快照都没有"同一条路径。
		return true
	}
	ensureLegacySnapshotStoreMigrated()
	latest := latestSnapshotManifest(snapshotRepoDirectory(normalized))
	if latest == nil {
		return true // 从没快照过：第一次启动必须留一个
	}
	// 只有当目标目录里存在比最近快照记录的更新文件时，才值得再存一份
	return directoryModifiedAfter(normalized, launchSnapshotBaseline(*latest))
}

// launchSnapshotBaseline 变更检测的基准时刻：最近快照记录到的最新文件时间。
//
// 不能拿快照自身的 CreatedAt（墙钟）当基准——两侧时钟域不同：CreatedAt 是纳秒
// 墙钟，文件 mtime 的粒度却由文件系统决定（ext3/HFS+ 秒级、FAT 2 秒级，网络盘
// 还可能有额外偏差）。快照之后紧接着发生的改动，mtime 被截断到整秒后会落到
// CreatedAt 之前，于是被判成"没变"、这次启动不留还原点（CI 上就是这么红的）。
// 基准取自同一份快照记录的 mtime 后，两边同源同粒度：快照记录之后又被写过的
// 文件，在任何粒度的文件系统上都判得出来。
//
// 清单里没有可用的文件时间时（空目录，或旧版本清单没落 ModTime）退回 CreatedAt。
// 内容时间晚于快照自己的创建时间说明时间戳不可信（世界来自时钟超前的机器），
// 同样退回 CreatedAt：宁可多留一个还原点，也不要漏掉变化。
func launchSnapshotBaseline(manifest saveSnapshotManifest) time.Time {
	var newest int64
	for _, file := range manifest.Files {
		if file.ModTime > newest {
			newest = file.ModTime
		}
	}
	if newest <= 0 {
		return manifest.CreatedAt
	}
	moment := time.Unix(0, newest)
	if moment.After(manifest.CreatedAt) {
		return manifest.CreatedAt
	}
	return moment
}

// directoryModifiedAfter 目标目录里是否存在晚于给定时刻的常规文件。
// 读不到的项按"没变化"处理：宁可少存一个还原点，也不要因为一个权限错误
// 在每次启动都存一遍。
//
// 忽略规则与快照内容保持一致（用 createSnapshot 采集时的同一个 kind）：被快照
// 排除的运行期临时文件（session.lock 由游戏每次启动重写）不能算作"内容变化"，
// 否则连续启动每次都多出一个内容完全相同的还原点——正是本功能要避免的重复。
func directoryModifiedAfter(directory string, moment time.Time) bool {
	modified := false
	_ = filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || modified {
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		relative, relErr := filepath.Rel(directory, path)
		if relErr != nil {
			return nil
		}
		if snapshotIgnores(snapshotKindSave, filepath.ToSlash(relative), false) {
			return nil
		}
		if info, infoErr := entry.Info(); infoErr == nil {
			if info.ModTime().After(moment) {
				modified = true
			}
		}
		return nil
	})
	return modified
}

// RollbackSaveSnapshot 把世界回滚到指定快照。
// 回滚前会自动创建一个安全快照，返回它以便用户反悔。
func RollbackSaveSnapshot(
	ctx context.Context,
	worldDirectory, snapshotID string,
) (SaveSnapshot, error) {
	return rollbackSnapshot(ctx, snapshotKindSave, worldDirectory, snapshotID)
}

// RollbackInstanceSnapshot 把实例回滚到指定快照。实例快照不包含的目录
// （libraries 等，见 instanceIgnoredTopDirs）会被原样保留。
func RollbackInstanceSnapshot(
	ctx context.Context,
	instanceDirectory, snapshotID string,
) (SaveSnapshot, error) {
	return rollbackSnapshot(ctx, snapshotKindInstance, instanceDirectory, snapshotID)
}

func rollbackSnapshot(
	ctx context.Context,
	kind snapshotKind,
	directory, snapshotID string,
) (SaveSnapshot, error) {
	normalized, err := normalizeSnapshotDirectory(directory)
	if err != nil {
		return SaveSnapshot{}, err
	}

	unlock := lockSnapshotRepo(normalized)
	defer unlock()

	repo := snapshotRepoDirectory(normalized)
	target, err := loadSnapshotManifest(repo, snapshotID)
	if err != nil {
		return SaveSnapshot{}, err
	}

	// 先给当前状态打安全快照，回滚失败/后悔都能还原
	safety, err := createSnapshotLocked(
		ctx,
		kind,
		normalized,
		snapshotSafetyLabel,
		snapshotReasonBeforeRollback,
		"",
	)
	if err != nil {
		return SaveSnapshot{}, fmt.Errorf("回滚前自动快照失败：%w", err)
	}

	if err := restoreSnapshotAtomically(ctx, kind, normalized, snapshotBlobsDirectory(), target); err != nil {
		return safety, fmt.Errorf(
			"回滚失败：%w（已保留回滚前快照 %s）",
			err,
			safety.Id,
		)
	}

	return safety, nil
}

// restoreSnapshotAtomically 原子回滚：先把快照内容完整恢复到目标目录**旁边**的
// 临时目录，确认无误后再用两次改名把目标目录换掉。
//
// 为什么不能直接往目标目录里恢复：中途失败（磁盘满、文件被占用、用户取消）会留下
// 一半旧一半新的目录——那时既不是回滚前也不是回滚后，连游戏都可能读不动。现在的
// 失败窗口只剩"两次改名之间"，且原地失败时会把原目录改回来。
//
// 实例目标的额外一步：快照跳过的顶层目录（libraries 等）不在清单里，直接换名会把
// 它们一起丢掉，所以先搬进临时目录，让换名把它们一并带回新目录；任何失败路径都会
// 把它们搬回原处。
func restoreSnapshotAtomically(
	ctx context.Context,
	kind snapshotKind,
	worldDirectory, blobsDir string,
	manifest saveSnapshotManifest,
) error {
	parent := filepath.Dir(worldDirectory)
	base := filepath.Base(worldDirectory)
	staging := filepath.Join(parent, snapshotRestoreSuffix+base)
	backup := filepath.Join(parent, snapshotBackupSuffix+base)
	preserved := preservedTopLevelNames(kind)

	// 上次中断留下的残骸先处理。两次改名之间中断的形态是"目标目录不在了、备份还在"：
	// 此时 staging 里可能有已搬入的保留目录，绝不能删——报错让用户手动恢复，数据都还在。
	if _, err := os.Stat(worldDirectory); err != nil {
		if _, backupErr := os.Stat(backup); backupErr == nil {
			return fmt.Errorf(
				"检测到上次回滚在换名途中中断：%s 缺失，原内容在 %s，快照内容与保留目录在 %s，请手动恢复后重试",
				base, backup, staging)
		}
	}
	// staging 残留（目标目录健在）：把误搬进来的保留目录先搬回，再清掉半成品
	salvagePreservedDirectories(staging, worldDirectory, preserved)

	if err := os.MkdirAll(staging, 0o755); err != nil {
		return err
	}
	if err := materializeSnapshot(ctx, staging, blobsDir, manifest); err != nil {
		// 恢复没成功：目标目录一个字节都没动过，直接清掉临时目录
		_ = os.RemoveAll(staging)

		return err
	}

	// 把"快照不管"的顶层目录搬进 staging，换名后它们随新目录一起归位
	moved := make([]string, 0, len(preserved))
	for name := range preserved {
		source := filepath.Join(worldDirectory, name)
		if _, err := os.Stat(source); err != nil {
			continue
		}
		if err := os.Rename(source, filepath.Join(staging, name)); err != nil {
			// 搬不动（被占用等）：中止，把已搬的搬回去，目标目录保持原样
			movePreservedBack(staging, worldDirectory, moved)
			_ = os.RemoveAll(staging)

			return fmt.Errorf("保留 %s 失败：%w", name, err)
		}
		moved = append(moved, name)
	}

	// 两次改名完成替换：目标 → 备份，临时 → 目标
	if err := os.Rename(worldDirectory, backup); err != nil {
		movePreservedBack(staging, worldDirectory, moved)
		_ = os.RemoveAll(staging)

		return err
	}
	if err := os.Rename(staging, worldDirectory); err != nil {
		// 第二步失败：把原目录改回来，保持"回滚没发生"的状态
		if restoreErr := os.Rename(backup, worldDirectory); restoreErr != nil {
			return fmt.Errorf(
				"%w（原目录在 %s，保留目录在 %s，请手动改回 %s）",
				err, backup, staging, worldDirectory)
		}
		movePreservedBack(staging, worldDirectory, moved)
		_ = os.RemoveAll(staging)

		return err
	}

	return os.RemoveAll(backup)
}

// salvagePreservedDirectories 中断残留的 staging 里可能存着上一次已从目标目录搬出的
// 保留目录：先搬回（目标已有同名目录时不动，staging 里那份随清理丢弃——它们都是
// 可再生内容），最后清掉 staging。save 目标没有保留集，等价于直接删除残留。
func salvagePreservedDirectories(staging, worldDirectory string, preserved map[string]bool) {
	entries, err := os.ReadDir(staging)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !preserved[entry.Name()] {
			continue
		}
		target := filepath.Join(worldDirectory, entry.Name())
		if _, err := os.Stat(target); os.IsNotExist(err) {
			_ = os.Rename(filepath.Join(staging, entry.Name()), target)
		}
	}
	_ = os.RemoveAll(staging)
}

// movePreservedBack 把已搬入 staging 的保留目录搬回目标目录（中止/失败路径）。
// 目标已有同名目录时放弃搬运：那份副本会随 staging 清掉，且它们都是可再生内容。
func movePreservedBack(staging, worldDirectory string, moved []string) {
	for _, name := range moved {
		source := filepath.Join(staging, name)
		target := filepath.Join(worldDirectory, name)
		if _, err := os.Stat(target); err == nil {
			continue
		}
		_ = os.Rename(source, target)
	}
}

// materializeSnapshot 把清单内容落回目标目录：删除多余文件 + 复制缺失/变化文件。
func materializeSnapshot(
	ctx context.Context,
	worldDirectory, blobsDir string,
	manifest saveSnapshotManifest,
) error {
	wanted := make(map[string]SaveSnapshotFile, len(manifest.Files))
	for _, file := range manifest.Files {
		wanted[file.Path] = file
	}

	// 1. 删除目标清单里没有的文件
	if err := filepath.WalkDir(worldDirectory, func(
		path string,
		entry fs.DirEntry,
		err error,
	) error {
		if err != nil {
			return err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		// 会话锁不属于快照内容，回滚时也不动它
		if strings.EqualFold(entry.Name(), sessionLockName) {
			return nil
		}
		relative, err := filepath.Rel(worldDirectory, path)
		if err != nil {
			return err
		}
		if _, ok := wanted[filepath.ToSlash(relative)]; ok {
			return nil
		}
		return os.Remove(path)
	}); err != nil {
		return err
	}

	// 2. 复制清单里的每个文件（覆盖目标中的现有内容）
	for _, file := range manifest.Files {
		if err := ctx.Err(); err != nil {
			return err
		}
		source := snapshotBlobPath(blobsDir, file.Hash)
		if source == "" {
			return fmt.Errorf("快照清单中的摘要非法：%q", file.Hash)
		}
		if _, err := os.Stat(source); err != nil {
			return fmt.Errorf("快照数据块缺失：%s", file.Hash)
		}
		destination := filepath.Join(
			worldDirectory,
			filepath.FromSlash(file.Path),
		)
		if !pathWithinDirectory(worldDirectory, destination) {
			return fmt.Errorf("快照路径越出目标目录：%q", file.Path)
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return err
		}
		if err := overwriteByCopy(source, destination); err != nil {
			return err
		}
	}

	removeEmptyDirectories(worldDirectory)
	return nil
}

// SetSaveSnapshotColor 更新存档快照的标记颜色（空串恢复默认色）。
func SetSaveSnapshotColor(worldDirectory, snapshotID, color string) error {
	return setSnapshotColor(worldDirectory, snapshotID, color)
}

// SetInstanceSnapshotColor 更新实例快照的标记颜色（空串恢复默认色）。
func SetInstanceSnapshotColor(instanceDirectory, snapshotID, color string) error {
	return setSnapshotColor(instanceDirectory, snapshotID, color)
}

func setSnapshotColor(directory, snapshotID, color string) error {
	normalized, err := normalizeSnapshotDirectory(directory)
	if err != nil {
		return err
	}

	unlock := lockSnapshotRepo(normalized)
	defer unlock()

	ensureLegacySnapshotStoreMigrated()
	repo := snapshotRepoDirectory(normalized)
	manifest, err := loadSnapshotManifest(repo, snapshotID)
	if err != nil {
		return err
	}
	manifest.Color = normalizeSnapshotColor(color)
	return writeSnapshotManifest(repo, manifest)
}

// DeleteSaveSnapshot 删除存档快照，并回收不再被引用的数据块。
func DeleteSaveSnapshot(worldDirectory, snapshotID string) error {
	return deleteSnapshot(worldDirectory, snapshotID)
}

// DeleteInstanceSnapshot 删除实例快照，并回收不再被引用的数据块。
func DeleteInstanceSnapshot(instanceDirectory, snapshotID string) error {
	return deleteSnapshot(instanceDirectory, snapshotID)
}

func deleteSnapshot(directory, snapshotID string) error {
	normalized, err := normalizeSnapshotDirectory(directory)
	if err != nil {
		return err
	}

	unlock := lockSnapshotRepo(normalized)
	defer unlock()

	ensureLegacySnapshotStoreMigrated()
	repo := snapshotRepoDirectory(normalized)
	manifestPath := filepath.Join(
		repo,
		snapshotManifestDirName,
		snapshotID+".json",
	)
	if err := os.Remove(manifestPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return garbageCollectSnapshotBlobs()
}

// garbageCollectSnapshotBlobs 删除所有仓库的清单都不再引用的数据块（全局库）。
// 创建中的 blob 由 pin 机制保护（见 createSnapshotLocked）。
func garbageCollectSnapshotBlobs() error {
	referenced := map[string]bool{}
	reposRoot := snapshotReposRootDirectory()
	keys, err := os.ReadDir(reposRoot)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, key := range keys {
		if !key.IsDir() {
			continue
		}
		manifests, err := loadSnapshotManifests(filepath.Join(reposRoot, key.Name()))
		if err != nil {
			continue
		}
		for _, manifest := range manifests {
			for _, file := range manifest.Files {
				referenced[file.Hash] = true
			}
		}
	}

	blobsDir := snapshotBlobsDirectory()
	buckets, err := os.ReadDir(blobsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, bucket := range buckets {
		if !bucket.IsDir() {
			continue
		}
		bucketPath := filepath.Join(blobsDir, bucket.Name())
		files, err := os.ReadDir(bucketPath)
		if err != nil {
			continue
		}
		for _, file := range files {
			if referenced[file.Name()] || snapshotBlobPinned(file.Name()) {
				continue
			}
			_ = os.Remove(filepath.Join(bucketPath, file.Name()))
		}
		if remaining, err := os.ReadDir(bucketPath); err == nil &&
			len(remaining) == 0 {
			_ = os.Remove(bucketPath)
		}
	}
	return nil
}

// ---- 跨仓库 GC 的在途保护 ----

var snapshotBlobPins = struct {
	sync.Mutex
	refs map[string]int
}{refs: map[string]int{}}

// pinSnapshotBlob 把一个哈希标记为"正在入仓"，返回解除标记的函数。
func pinSnapshotBlob(hash string) func() {
	snapshotBlobPins.Lock()
	snapshotBlobPins.refs[hash]++
	snapshotBlobPins.Unlock()

	return func() {
		snapshotBlobPins.Lock()
		if snapshotBlobPins.refs[hash]--; snapshotBlobPins.refs[hash] <= 0 {
			delete(snapshotBlobPins.refs, hash)
		}
		snapshotBlobPins.Unlock()
	}
}

// snapshotBlobPinned 哈希是否正被某个仓库的创建过程引用。
func snapshotBlobPinned(hash string) bool {
	snapshotBlobPins.Lock()
	defer snapshotBlobPins.Unlock()

	return snapshotBlobPins.refs[hash] > 0
}

// ---- 清单读写 ----

func loadSnapshotManifests(repo string) ([]saveSnapshotManifest, error) {
	directory := filepath.Join(repo, snapshotManifestDirName)
	entries, err := os.ReadDir(directory)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	result := make([]saveSnapshotManifest, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			continue
		}
		var manifest saveSnapshotManifest
		if json.Unmarshal(data, &manifest) != nil {
			continue
		}
		if manifest.Id == "" {
			manifest.Id = strings.TrimSuffix(entry.Name(), ".json")
		}
		result = append(result, manifest)
	}
	return result, nil
}

// latestSnapshotManifest 取最新的一份落盘清单，没有快照时返回 nil。
// 取 Id 最大的一份：新 Id 形如 20060102-150405.000，字典序即时间序。
func latestSnapshotManifest(repo string) *saveSnapshotManifest {
	manifests, err := loadSnapshotManifests(repo)
	if err != nil || len(manifests) == 0 {
		return nil
	}
	latest := manifests[0]
	for _, manifest := range manifests[1:] {
		if manifest.Id > latest.Id {
			latest = manifest
		}
	}
	return &latest
}

func loadSnapshotManifest(repo, id string) (saveSnapshotManifest, error) {
	data, err := os.ReadFile(
		filepath.Join(repo, snapshotManifestDirName, id+".json"),
	)
	if err != nil {
		return saveSnapshotManifest{}, fmt.Errorf("快照不存在：%s", id)
	}
	var manifest saveSnapshotManifest
	if json.Unmarshal(data, &manifest) != nil {
		return saveSnapshotManifest{}, err
	}
	if manifest.Id == "" {
		manifest.Id = id
	}
	return manifest, nil
}

func writeSnapshotManifest(repo string, manifest saveSnapshotManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	target := filepath.Join(
		repo,
		snapshotManifestDirName,
		manifest.Id+".json",
	)
	temporary := target + ".tmp"
	if err := os.WriteFile(temporary, data, 0o644); err != nil {
		return err
	}
	// 直接 rename 覆盖：Go 在 Windows 上走 MoveFileEx(MOVEFILE_REPLACE_EXISTING)，
	// 本来就能覆盖已存在文件。先删目标再改名是多余且危险的——一旦改名仍失败，
	// 旧 manifest 已经被删掉，快照就成了没有清单的孤儿数据（GC 时被清掉）。
	if err := os.Rename(temporary, target); err != nil {
		_ = os.Remove(temporary)

		return err
	}

	return nil
}

// newSnapshotID 生成按时间可排序、且不重名的快照 id。
func newSnapshotID(manifestsDir string) string {
	return newSnapshotIDAt(manifestsDir, time.Now())
}

// newSnapshotIDAt 同上，时间可注入（测试里要构造重名场景）。
//
// 冲突后缀补零到两位（-01、-02…-10）：不补零时字符串排序会把 "…-10" 排到 "…-2"
// 前面（时间线按 CreatedAt 排序不受影响，但按 id 比较/展示的地方会错序）。
func newSnapshotIDAt(manifestsDir string, now time.Time) string {
	base := now.Format("20060102-150405.000")
	candidate := base
	for index := 1; ; index++ {
		if _, err := os.Stat(
			filepath.Join(manifestsDir, candidate+".json"),
		); os.IsNotExist(err) {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%02d", base, index)
	}
}

func snapshotSummary(manifest saveSnapshotManifest) SaveSnapshot {
	return SaveSnapshot{
		Id:         manifest.Id,
		CreatedAt:  manifest.CreatedAt,
		Label:      manifest.Label,
		Reason:     manifest.Reason,
		Color:      manifest.Color,
		FileCount:  manifest.FileCount,
		TotalSize:  manifest.TotalSize,
		AddedSize:  manifest.AddedSize,
		AddedFiles: manifest.AddedFiles,
		WorldName:  manifest.WorldName,
	}
}

// ---- 数据块 ----

// snapshotBlobPath 摘要 → 数据块路径；摘要短于 2 字符（清单损坏/被手改）
// 时返回空串，由调用方报错——否则 hash[:2] 会直接 panic 掉整个进程。
func snapshotBlobPath(blobsDir, hash string) string {
	if len(hash) < 2 {
		return ""
	}
	return filepath.Join(blobsDir, hash[:2], hash)
}

func snapshotBlobExists(blobsDir, hash string) bool {
	if len(hash) < 2 {
		return false
	}
	info, err := os.Stat(snapshotBlobPath(blobsDir, hash))
	return err == nil && !info.IsDir()
}

// storeSnapshotBlob 把文件内容写入内容寻址存储（原子落盘，已存在则跳过）。
func storeSnapshotBlob(
	ctx context.Context,
	sourcePath, blobsDir, hash string,
) error {
	if len(hash) < 2 {
		return fmt.Errorf("摘要无效：%q", hash)
	}
	bucket := filepath.Join(blobsDir, hash[:2])
	if err := os.MkdirAll(bucket, 0o755); err != nil {
		return err
	}
	target := filepath.Join(bucket, hash)
	if _, err := os.Stat(target); err == nil {
		return nil
	}

	temporary, err := os.CreateTemp(bucket, hash+".tmp-")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(temporaryName)
		}
	}()

	source, err := os.Open(sourcePath)
	if err != nil {
		_ = temporary.Close()
		return err
	}
	_, copyErr := io.Copy(temporary, source)
	_ = source.Close()
	closeErr := temporary.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := os.Rename(temporaryName, target); err != nil {
		// 并发写入时目标可能刚被创建：视为成功
		if _, statErr := os.Stat(target); statErr == nil {
			return nil
		}
		return err
	}
	cleanup = false
	return nil
}

func hashFileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// ---- 工具 ----

func pathWithinDirectory(root, path string) bool {
	rootAbsolute := filepath.Clean(mustAbsPath(root))
	pathAbsolute := filepath.Clean(mustAbsPath(path))
	if runtime.GOOS == "windows" {
		rootAbsolute = strings.ToLower(rootAbsolute)
		pathAbsolute = strings.ToLower(pathAbsolute)
	}
	if pathAbsolute == rootAbsolute {
		return true
	}
	return strings.HasPrefix(
		pathAbsolute,
		rootAbsolute+string(filepath.Separator),
	)
}

func removeEmptyDirectories(root string) {
	var directories []string
	_ = filepath.WalkDir(root, func(
		path string,
		entry fs.DirEntry,
		err error,
	) error {
		if err != nil || !entry.IsDir() {
			return nil
		}
		directories = append(directories, path)
		return nil
	})
	// 从最深层往上删，父目录才有机会变空
	sort.SliceStable(directories, func(i, j int) bool {
		return len(directories[i]) > len(directories[j])
	})
	for _, directory := range directories {
		if directory == root {
			continue
		}
		entries, err := os.ReadDir(directory)
		if err == nil && len(entries) == 0 {
			_ = os.Remove(directory)
		}
	}
}
