package download

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"
)

// 内容下载任务注册表：Mod / 资源包 / 整合包 / Java 等内容类下载的可观测层。
//
// 每个任务注册后拿到 report（进度上报）与 finish（终态标记）；注册表聚合出
// 任务快照列表，绑定层经 OnContentTasksChanged 节流推送给前端右下角的下载
// 中心（此前内容下载只有一条无身份的 download:contentProgress，浮标里看不见、
// 也无法取消）。OnContentTaskProgress 保留单条进度回调，兼容弹层内的进度条。

// ContentTaskPhase 任务阶段（与 GameDownloadPhase 的取值语义对齐）。
type ContentTaskPhase int

const (
	ContentTaskActive    ContentTaskPhase = 1
	ContentTaskCompleted ContentTaskPhase = 3
	ContentTaskFailed    ContentTaskPhase = 4
	ContentTaskCancelled ContentTaskPhase = 5
)

// ContentTaskSnapshot 单个内容下载任务的快照。
type ContentTaskSnapshot struct {
	ID              string           `json:"id"`
	Name            string           `json:"name"`
	// Kind 任务类别：content（单文件）/ modpack（整合包批量）/ java（运行时）。
	Kind            string           `json:"kind"`
	Phase           ContentTaskPhase `json:"phase"`
	Detail          string           `json:"detail"`
	DownloadedBytes int64            `json:"downloadedBytes"`
	// TotalBytes 未知总大小时为 0（前端按不定进度条渲染）。
	TotalBytes     int64   `json:"totalBytes"`
	BytesPerSecond float64 `json:"bytesPerSecond"`
	// EtaSeconds 预计剩余秒数；-1 = 无法估算（总大小未知或速度尚未稳定）。
	EtaSeconds float64 `json:"etaSeconds"`
}

type contentTask struct {
	snapshot ContentTaskSnapshot
	cancel   context.CancelFunc

	// 速度估算：EMA 平滑（下载是突发性的，瞬时值抖得没法看）
	lastTime  time.Time
	lastBytes int64
	speed     float64

	lastEmit time.Time
}

var (
	contentTasksMu   sync.Mutex
	contentTasks     = map[string]*contentTask{}
	contentTaskSeq   int64
	contentTaskOrder []string
	notifyPending    bool
	notifyTimer      *time.Timer
)

// 事件回调由绑定层注入（wiring.go）；nil 时静默跳过。
var (
	// OnContentTasksChanged 任务列表变化（全局 200ms 节流合并）后触发。
	OnContentTasksChanged func()
	// OnContentTaskProgress 兼容旧 download:contentProgress 的单条进度（每任务 100ms 节流）。
	OnContentTaskProgress func(downloaded, total int64)
)

const (
	contentProgressInterval = 100 * time.Millisecond
	contentTaskListInterval = 200 * time.Millisecond
	// terminalTasksKeep 终态任务保留条数：完成后仍在下载中心短暂可见。
	terminalTasksKeep = 6
)

// StartContentTask 注册新任务，返回任务 ID、进度上报函数与终态标记函数。
// cancel 用于"下载中心里点取消"：注册表只存取消函数，不感知 ctx。
func StartContentTask(
	kind, name string,
	cancel context.CancelFunc,
) (id string, report func(downloaded, total int64), finish func(err error)) {
	contentTasksMu.Lock()
	defer contentTasksMu.Unlock()
	contentTaskSeq++
	id = "ct-" + strconv.FormatInt(contentTaskSeq, 10)
	contentTasks[id] = &contentTask{
		snapshot: ContentTaskSnapshot{
			ID:    id,
			Name:  name,
			Kind:  kind,
			Phase: ContentTaskActive,
		},
		cancel:   cancel,
		lastTime: time.Now(),
	}
	contentTaskOrder = append(contentTaskOrder, id)
	pruneTerminalLocked()
	scheduleNotifyLocked()

	return id, makeTaskReport(id), makeTaskFinish(id)
}

// ContentTasksList 当前任务快照（活跃在前，其后按创建序排列的终态任务）。
func ContentTasksList() []ContentTaskSnapshot {
	contentTasksMu.Lock()
	defer contentTasksMu.Unlock()

	active := make([]ContentTaskSnapshot, 0, len(contentTasks))
	done := make([]ContentTaskSnapshot, 0, len(contentTaskOrder))
	for _, id := range contentTaskOrder {
		task, ok := contentTasks[id]
		if !ok {
			continue
		}
		if task.snapshot.Phase == ContentTaskActive {
			active = append(active, task.snapshot)
		} else {
			done = append(done, task.snapshot)
		}
	}

	return append(active, done...)
}

// CancelContentTask 取消任务（仅活跃任务可取消），返回是否找到了活跃任务。
func CancelContentTask(id string) bool {
	contentTasksMu.Lock()
	task, ok := contentTasks[id]
	if !ok || task.snapshot.Phase != ContentTaskActive {
		contentTasksMu.Unlock()

		return false
	}
	cancel := task.cancel
	contentTasksMu.Unlock()

	if cancel != nil {
		cancel()
	}
	return true
}

// makeTaskReport 进度上报：更新字节数、速度 EMA 与剩余时间估算；单任务 100ms
// 节流地喂 OnContentTaskProgress（旧事件），任务列表刷新走全局节流（scheduleNotify）。
func makeTaskReport(id string) func(downloaded, total int64) {
	return func(downloaded, total int64) {
		var progressEvent bool

		contentTasksMu.Lock()
		task := contentTasks[id]
		if task != nil && task.snapshot.Phase == ContentTaskActive {
			now := time.Now()
			if elapsed := now.Sub(task.lastTime).Seconds(); elapsed > 0.15 {
				instant := float64(downloaded-task.lastBytes) / elapsed
				if task.speed == 0 || instant >= 0 {
					task.speed = 0.6*task.speed + 0.4*instant
				}
				task.lastTime = now
				task.lastBytes = downloaded
			}
			if total > 0 && downloaded >= total {
				task.speed = 0
			}
			task.snapshot.DownloadedBytes = downloaded
			task.snapshot.TotalBytes = total
			task.snapshot.BytesPerSecond = task.speed
			// 剩余时间：总大小未知或速度尚未稳定（首窗口内）时给 -1
			if remaining := total - downloaded; total > 0 && remaining > 0 && task.speed > 1 {
				task.snapshot.EtaSeconds = float64(remaining) / task.speed
			} else {
				task.snapshot.EtaSeconds = -1
			}
			if OnContentTaskProgress != nil && now.Sub(task.lastEmit) >= contentProgressInterval {
				task.lastEmit = now
				progressEvent = true
			}
		}
		contentTasksMu.Unlock()

		if progressEvent && OnContentTaskProgress != nil {
			OnContentTaskProgress(downloaded, total)
		}
		scheduleNotify()
	}
}

// UpdateContentTaskDetail 更新任务阶段描述（如"解压整合包文件…"/"下载依赖文件…"）。
// 任务可能已结束（终态后迟到的回调），此时静默忽略。
func UpdateContentTaskDetail(id, detail string) {
	contentTasksMu.Lock()
	task := contentTasks[id]
	if task == nil || task.snapshot.Phase != ContentTaskActive {
		contentTasksMu.Unlock()

		return
	}
	task.snapshot.Detail = detail
	contentTasksMu.Unlock()
	scheduleNotify()
}

// makeTaskFinish 终态标记：成功 / 取消 / 失败（带 Detail）。
func makeTaskFinish(id string) func(err error) {
	return func(err error) {
		contentTasksMu.Lock()
		task := contentTasks[id]
		if task == nil || task.snapshot.Phase != ContentTaskActive {
			contentTasksMu.Unlock()

			return
		}
		switch {
		case err == nil:
			task.snapshot.Phase = ContentTaskCompleted
		case errors.Is(err, context.Canceled):
			task.snapshot.Phase = ContentTaskCancelled
			task.snapshot.Detail = "已取消"
		default:
			task.snapshot.Phase = ContentTaskFailed
			task.snapshot.Detail = err.Error()
		}
		task.speed = 0
		task.snapshot.BytesPerSecond = 0
		contentTasksMu.Unlock()

		notifyNow()
	}
}

// scheduleNotify 任务列表变化的全局节流：200ms 内的多次上报合并成一次推送。
func scheduleNotify() {
	contentTasksMu.Lock()
	defer contentTasksMu.Unlock()
	scheduleNotifyLocked()
}

func scheduleNotifyLocked() {
	if OnContentTasksChanged == nil || notifyPending {
		return
	}
	notifyPending = true
	notifyTimer = time.AfterFunc(contentTaskListInterval, func() {
		contentTasksMu.Lock()
		notifyPending = false
		notifyTimer = nil
		contentTasksMu.Unlock()
		notifyNow()
	})
}

func notifyNow() {
	contentTasksMu.Lock()
	timer := notifyTimer
	contentTasksMu.Unlock()
	if timer != nil {
		timer.Stop()
	}
	if OnContentTasksChanged != nil {
		OnContentTasksChanged()
	}
}

// RunContentDownload 把一次内容下载包装成可观测任务：注册任务、派生可取消
// 的 ctx、进度喂给注册表（下载中心 + 旧 contentProgress 事件），结束时标记
// 终态。run 收到的 report 即任务进度上报（直接透传给内部下载函数），
// setDetail 用于更新"当前下载阶段"（解压中 / 下载依赖 / 阶段名等）。
func RunContentDownload(
	ctx context.Context,
	kind, name string,
	run func(taskCtx context.Context, report ProgressBytes, setDetail func(string)) error,
) error {
	taskCtx, cancel := context.WithCancel(ctx)
	id, report, finish := StartContentTask(kind, name, cancel)
	setDetail := func(detail string) { UpdateContentTaskDetail(id, detail) }
	defer finish(nil)

	err := run(taskCtx, report, setDetail)
	if err != nil {
		finish(err)
	}
	return err
}

// pruneTerminalLocked 终态任务只保留最近 N 条，防止长会话内存与列表无限增长。
func pruneTerminalLocked() {
	terminal := 0
	for i := len(contentTaskOrder) - 1; i >= 0; i-- {
		task, ok := contentTasks[contentTaskOrder[i]]
		if !ok || task.snapshot.Phase == ContentTaskActive {
			continue
		}
		terminal++
		if terminal <= terminalTasksKeep {
			continue
		}
		delete(contentTasks, contentTaskOrder[i])
		contentTaskOrder = append(contentTaskOrder[:i], contentTaskOrder[i+1:]...)
	}
}
