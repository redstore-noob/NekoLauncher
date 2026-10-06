/*
 * 本地音乐播放页（三栏式桌面布局重绘版）：
 *   左侧窄栏（曲库 / 收藏 / 最近播放 / 播放队列 切换）
 *   + 中间曲目列表（搜索 / 排序 / 双击即播）
 *   + 右侧当前播放面板（封面色块 + 歌词，窄窗口自动隐藏）
 *   + 底部全局控制条（完整控制 / 进度缓冲 / 音量 / 倍速 / EQ / 迷你模式）。
 * 曲库扫描/排序/搜索走 MusicAPI；播放控制调 PlayTrack/Pause 等后端方法，
 * 实际发声由 lib/audioBridge.ts 的 <audio> 单例完成（监听 music:play 等事件）。
 * 收藏 / 最近播放 / 上次曲目 / 倍速 / EQ 经 ConfigAPI 键值持久化（lib/musicStore.ts）。
 */
import type { music } from "../../wailsjs/go/models";

import React, {
  useCallback,
  useDeferredValue,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  Button,
  Input,
  Popover,
  PopoverContent,
  PopoverTrigger,
  Select,
  SelectItem,
  Slider,
  Tooltip,
} from "@heroui/react";
// 图标统一用 Fluent UI System Icons（20px 系），别名保持语义命名
import {
  MusicNote220Regular as MusicNoteIcon,
  FolderOpen20Regular as FolderIcon,
  ArrowClockwise20Regular as RefreshIcon,
  Search20Regular as SearchIcon,
  Previous20Regular as SkipPreviousIcon,
  Next20Regular as SkipNextIcon,
  Play20Regular as PlayIcon,
  Pause20Regular as PauseIcon,
  Stop20Regular as StopIcon,
  ArrowRepeatAll20Regular as RepeatIcon,
  ArrowRepeat120Regular as RepeatOneIcon,
  ArrowShuffle20Regular as ShuffleIcon,
  List20Regular as SequentialIcon,
  List20Regular as QueueIcon,
  SpeakerMute20Regular as VolumeOffIcon,
  Speaker120Regular as VolumeLowIcon,
  Speaker220Regular as VolumeHighIcon,
  Library20Regular as LibraryIcon,
  Heart20Regular as HeartIcon,
  Heart20Filled as HeartFilledIcon,
  History20Regular as HistoryIcon,
  ArrowRotateCounterclockwise20Regular as RewindIcon,
  ArrowRotateClockwise20Regular as ForwardIcon,
  TopSpeed20Regular as SpeedIcon,
  Options20Regular as EqIcon,
  ArrowMinimize20Regular as MiniModeIcon,
  ArrowMaximize20Regular as ExitMiniIcon,
} from "@fluentui/react-icons";
import { AnimatePresence, motion } from "framer-motion";

import { SelectDirectory } from "../../wailsjs/go/bindings/SystemAPI";
import {
  GetMusicFolderPath,
  SetMusicFolder,
  ScanMusicLibrary,
  GetMusicTracks,
  SearchMusicTracks,
  GetSortedMusicTracks,
  GetMusicSortMode,
  SetMusicSortMode,
  GetMusicVolume,
  SetMusicVolume,
  GetMusicPlaybackMode,
  SetMusicPlaybackMode,
  PlayTrack,
  PausePlayback,
  ResumePlayback,
  StopPlayback,
  NextTrack,
  PreviousTrack,
  GetCurrentTrack,
  GetPlaylist,
  SetPlaylist,
  SeekPlayback,
} from "../../wailsjs/go/bindings/MusicAPI";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { notify } from "../components/overlay/dialog";
import {
  applyEqGains,
  audioState,
  EQ_BANDS,
  EQ_PRESETS,
  setAudioRate,
  startAudioBridge,
  useAudioState,
} from "../lib/audioBridge";
import {
  loadEq,
  loadFavorites,
  loadLastTrackPath,
  loadRate,
  loadRecent,
  pushRecent,
  saveEq,
  saveFavorites,
  saveLastTrackPath,
  saveRate,
  type RecentEntry,
} from "../lib/musicStore";
import { asArray } from "../lib/guards";
import { getCoverUrl } from "../lib/coverArt";
import {
  listItemVariants,
  popoverMotionProps,
  tooltipMotionProps,
  selectPopoverProps,
} from "../lib/motion";
import { startVisiblePoll } from "../lib/visibility";
import { t } from "../i18n";
import { LyricLines, useLrc } from "../components/music/Lyrics";
import NowPlayingView from "../components/music/NowPlayingView";

type MusicTrack = music.MusicTrack;

type ViewTab = "library" | "favorites" | "recent" | "queue";

const SORT_OPTIONS = [
  { value: "FileName", label: t("文件名 A-Z") },
  { value: "FileNameDesc", label: t("文件名 Z-A") },
  { value: "DateModified", label: t("修改时间 ↑") },
  { value: "DateModifiedDesc", label: t("修改时间 ↓") },
  { value: "FileSize", label: t("文件大小 ↑") },
  { value: "FileSizeDesc", label: t("文件大小 ↓") },
];
const MODE_CYCLE = ["Sequential", "RepeatAll", "RepeatOne", "Shuffle"] as const;
const MODE_META: Record<string, { icon: React.ReactNode; tip: string }> = {
  Sequential: { icon: <SequentialIcon />, tip: t("顺序播放") },
  RepeatAll: { icon: <RepeatIcon />, tip: t("列表循环") },
  RepeatOne: { icon: <RepeatOneIcon />, tip: t("单曲循环") },
  Shuffle: { icon: <ShuffleIcon />, tip: t("随机播放") },
};
const RATE_CYCLE = [1, 1.25, 1.5, 2, 0.75, 0.5];
const EQ_PRESET_NAMES = ["Off", "Pop", "Rock", "Classical", "Vocal"];
const EQ_PRESET_LABELS: Record<string, string> = {
  Off: t("关闭"),
  Pop: t("流行"),
  Rock: t("摇滚"),
  Classical: t("古典"),
  Vocal: t("人声"),
  Custom: t("自定义"),
};

// 曲目显示工具（对应 music.MusicTrack.FilePath；标题 = 去扩展名文件名）
function trackTitle(track: MusicTrack): string {
  const base = track.FilePath.split(/[\\/]/).pop() ?? "";

  return base.replace(/\.[^.]+$/, "");
}

function trackMeta(track: MusicTrack): string {
  const ext = (track.FilePath.match(/\.([^.\\/]+)$/)?.[1] ?? "").toLowerCase();
  const size =
    track.FileSize >= 1048576
      ? `${(track.FileSize / 1048576).toFixed(1)} MB`
      : track.FileSize >= 1024
        ? `${Math.round(track.FileSize / 1024)} KB`
        : `${track.FileSize} B`;

  return `${ext} · ${size}`;
}

function formatTime(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds <= 0) return "0:00";
  const total = Math.floor(seconds);

  return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, "0")}`;
}

const MusicPage: React.FC = () => {
  const audio = useAudioState();

  // --- 曲库 / 视图状态 ---
  const [library, setLibrary] = useState<MusicTrack[]>([]); // 完整曲库
  const [displayTracks, setDisplayTracks] = useState<MusicTrack[]>([]); // 曲库页（搜索过滤后的展示列表）
  const [keyword, setKeyword] = useState("");
  const [sortMode, setSortMode] = useState<string>("FileName");
  const [folderPath, setFolderPath] = useState("");
  const [tab, setTab] = useState<ViewTab>("library");
  const [favorites, setFavorites] = useState<string[]>([]);
  const [recent, setRecent] = useState<RecentEntry[]>([]);
  const [queue, setQueue] = useState<MusicTrack[]>([]); // 共享播放列表的本地镜像

  // --- 播放器状态 ---
  const [playbackMode, setPlaybackMode] = useState("Sequential");
  const [volume, setVolume] = useState(80);
  const [currentTrack, setCurrentTrack] = useState<MusicTrack | null>(null);
  const [playbackState, setPlaybackState] = useState("Stopped");
  const [nowPlayingOpen, setNowPlayingOpen] = useState(false);
  const [miniMode, setMiniMode] = useState(false);
  const [rate, setRate] = useState(1);
  const [eqOpen, setEqOpen] = useState(false);
  const [eqPreset, setEqPreset] = useState("Off");
  const [eqGains, setEqGains] = useState<number[]>([0, 0, 0, 0, 0]);

  const [userDragging, setUserDragging] = useState(false);
  const [progressValue, setProgressValue] = useState(0);
  const lastSeekedValue = useRef(-1);

  // 引用最新状态的 ref（事件回调与定时器内读取）
  const sortModeRef = useRef(sortMode);

  sortModeRef.current = sortMode;
  const displayTracksRef = useRef(displayTracks);

  displayTracksRef.current = displayTracks;
  const currentTrackRef = useRef(currentTrack);

  currentTrackRef.current = currentTrack;
  const playbackStateRef = useRef(playbackState);

  playbackStateRef.current = playbackState;
  const libraryRef = useRef(library);

  libraryRef.current = library;
  const queueRef = useRef(queue);

  queueRef.current = queue;
  const favoritesRef = useRef(favorites);

  favoritesRef.current = favorites;
  const tabRef = useRef(tab);

  tabRef.current = tab;

  // --- Web Audio 桥状态 ---
  const isPlaying = audio.playing;
  const positionSec = userDragging ? progressValue : audio.positionMs / 1000;
  const durationSec = audio.durationMs / 1000;
  const bufferedPct =
    durationSec > 0
      ? Math.min(100, (audio.bufferedMs / 1000 / durationSec) * 100)
      : 0;

  const progressEnabled =
    playbackState !== "Stopped" && durationSec > 0 && audio.canPlay;

  const trackCountText =
    library.length > 0
      ? t("{0} 首歌曲 · {1}", { "0": library.length, "1": folderPath })
      : folderPath
        ? t("未找到音频文件 · {0}", { "0": folderPath })
        : "";

  let nowTitle: string;

  if (playbackState === "Stopped" && !currentTrack) nowTitle = t("未在播放");
  else if (playbackState === "Stopped" && !audio.canPlay && audio.error)
    nowTitle = t("播放失败");
  else nowTitle = currentTrack ? trackTitle(currentTrack) : t("未在播放");

  let nowInfo: string;

  if (audio.error && playbackState !== "Playing" && playbackState !== "Paused")
    nowInfo = audio.error;
  else if (audio.error && !audio.canPlay && currentTrack)
    nowInfo = t("暂不可播：无法读取本地音频文件");
  else nowInfo = currentTrack ? trackMeta(currentTrack) : "";

  const modeIcon = MODE_META[playbackMode]?.icon ?? MODE_META.Sequential.icon;
  const modeTip = MODE_META[playbackMode]?.tip ?? t("顺序播放");
  const VolumeIcon =
    volume === 0 ? VolumeOffIcon : volume < 50 ? VolumeLowIcon : VolumeHighIcon;
  const isFavCurrent =
    !!currentTrack && favorites.includes(currentTrack.FilePath);

  // 列表关键词降优先级：输入回显走即时 state，过滤与整列表重渲染
  // 作为可中断的低优先级更新——几千首时打字不再拖住输入框
  const deferredKeyword = useDeferredValue(keyword);

  // 当前视图下的曲目列表（收藏 / 最近 / 队列均叠加关键词过滤）
  const viewTracks: MusicTrack[] = useMemo(() => {
    const kw = deferredKeyword.trim().toLowerCase();

    if (tab === "library") return displayTracks;
    if (tab === "queue")
      return kw
        ? queue.filter((track) => trackTitle(track).toLowerCase().includes(kw))
        : queue;
    if (tab === "favorites") {
      const favSet = new Set(favorites);

      return library.filter(
        (track) =>
          favSet.has(track.FilePath) &&
          (!kw || trackTitle(track).toLowerCase().includes(kw)),
      );
    }
    // recent：按最近播放时间倒序映射回曲库（已删除的文件自动过滤）
    const byPath = new Map(library.map((track) => [track.FilePath, track]));

    return recent
      .filter((e) => byPath.has(e.path))
      .filter((e) => {
        const track = byPath.get(e.path);

        return track && (!kw || trackTitle(track).toLowerCase().includes(kw));
      })
      .map((e) => byPath.get(e.path) as MusicTrack);
  }, [tab, displayTracks, queue, library, favorites, recent, deferredKeyword]);

  const viewTracksRef = useRef(viewTracks);

  viewTracksRef.current = viewTracks;

  function isCurrent(track: MusicTrack): boolean {
    return !!currentTrack && track.FilePath === currentTrack.FilePath;
  }

  // ------------------------------------------------------------------
  // 曲库加载 / 排序 / 搜索
  // ------------------------------------------------------------------

  const refreshLibrary = useCallback(async () => {
    try {
      // 必须 await：扫描是 Go 侧的耗时绑定，之前不等待就取列表，
      // 大曲库首屏拿到的是上一次（甚至空）结果。
      await ScanMusicLibrary();
      setFolderPath(await GetMusicFolderPath());
      const mode = await GetMusicSortMode();

      setSortMode(mode as string);
      const full = asArray<MusicTrack>(await GetMusicTracks());

      setLibrary(full);
      setDisplayTracks(asArray<MusicTrack>(await GetSortedMusicTracks(mode)));
      // 共享播放列表 = 完整（未过滤）曲目列表，供自动切歌/上下一首使用
      const playlist = asArray<MusicTrack>(await GetPlaylist());

      setQueue(playlist);
    } catch (ex) {
      notify.error(
        t("扫描曲库失败：{0}", { "0": (ex as Error)?.message ?? ex }),
      );
    }
  }, []);

  async function selectFolder() {
    let path = "";

    try {
      path = await SelectDirectory(t("选择音乐文件夹"));
    } catch {
      /* 用户取消或选择失败 */
    }
    if (!path) return;
    try {
      await SetMusicFolder(path);
      await refreshLibrary();
    } catch (ex) {
      notify.error(
        t("设置音乐文件夹失败：{0}", { "0": (ex as Error)?.message ?? ex }),
      );
    }
  }

  async function onSortChanged(mode: string) {
    if (!mode) return;
    setSortMode(mode);
    SetMusicSortMode(mode as never);
    setDisplayTracks(
      asArray<MusicTrack>(await GetSortedMusicTracks(mode as never)),
    );
    SetPlaylist([...library]);
    setQueue([...library]);
  }

  // 搜索是"打字即发"的：慢的旧请求可能后于新请求返回，把列表刷成过期结果，
  // 用一个自增序号，回来时不是最新那次就丢弃
  const filterSeqRef = useRef(0);

  // 搜索"打字即发"但不"打字即查"：180ms 防抖再发后端查询，几百首时
  // 每敲一个字符都全库检索会连打一串请求；过期结果由下面的序号丢弃
  const filterTimerRef = useRef<number | null>(null);

  function applyFilter(text: string) {
    setKeyword(text); // 输入回显即时；列表交给防抖后的查询（低优先级跟随）
    if (filterTimerRef.current !== null)
      window.clearTimeout(filterTimerRef.current);
    filterTimerRef.current = window.setTimeout(() => {
      filterTimerRef.current = null;
      void runFilter(text);
    }, 180);
  }

  async function runFilter(text: string) {
    const seq = ++filterSeqRef.current;

    if (!text.trim()) {
      const all = asArray<MusicTrack>(
        await GetSortedMusicTracks(sortModeRef.current as never),
      );

      if (seq === filterSeqRef.current) setDisplayTracks(all);

      return;
    }
    const hits = asArray<MusicTrack>(await SearchMusicTracks(text));

    if (seq === filterSeqRef.current) setDisplayTracks(hits);
  }

  // 卸载时清掉待触发的防抖查询
  useEffect(
    () => () => {
      if (filterTimerRef.current !== null)
        window.clearTimeout(filterTimerRef.current);
    },
    [],
  );

  // ------------------------------------------------------------------
  // 播放控制（转给 Go 状态机，发声经 audioBridge）
  // ------------------------------------------------------------------

  /** 在指定上下文（队列）中播放：先写共享播放列表，再点播目标曲目。 */
  async function playInContext(list: MusicTrack[], track: MusicTrack) {
    const cur = currentTrackRef.current;

    if (
      cur &&
      playbackStateRef.current !== "Stopped" &&
      cur.FilePath === track.FilePath &&
      queueRef.current.length === list.length &&
      queueRef.current.every((item, i) => item.FilePath === list[i]?.FilePath)
    )
      return; // 同一队列同一曲目：不重启（避免自动切歌后被打回 0:00）
    try {
      SetPlaylist([...list]);
      setQueue([...list]);
      await PlayTrack(track);
    } catch (ex) {
      notify.error(t("播放失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    }
  }

  async function onTrackClick(track: MusicTrack) {
    const list = viewTracksRef.current;

    if (
      tabRef.current === "queue" &&
      list.some((t2) => t2.FilePath === track.FilePath)
    ) {
      // 队列视图：直接点播，不打乱现有队列
      const cur = currentTrackRef.current;

      if (
        cur &&
        playbackStateRef.current !== "Stopped" &&
        cur.FilePath === track.FilePath
      )
        return;
      try {
        await PlayTrack(track);
      } catch (ex) {
        notify.error(t("播放失败：{0}", { "0": (ex as Error)?.message ?? ex }));
      }

      return;
    }
    await playInContext(list, track);
  }

  function togglePlayPause() {
    if (playbackStateRef.current === "Playing") PausePlayback();
    else if (playbackStateRef.current === "Paused") ResumePlayback();
    else if (viewTracksRef.current.length > 0) {
      // 停止态：播放当前视图首项
      playInContext(viewTracksRef.current, viewTracksRef.current[0]).catch(
        () => {
          /* ignore */
        },
      );
    }
  }

  function previous() {
    PreviousTrack().catch(() => {
      /* ignore */
    });
  }
  function next() {
    NextTrack().catch(() => {
      /* ignore */
    });
  }
  function stop() {
    StopPlayback().catch(() => {
      /* ignore */
    });
  }

  /** 相对当前进度快退/快进（秒），经 Go SeekPlayback 统一走事件桥。 */
  function nudge(deltaSec: number) {
    if (!progressEnabled) return;
    const base = audioState.positionMs / 1000;
    const target = Math.min(durationSec, Math.max(0, base + deltaSec));

    SeekPlayback(Math.round(target * 1000)).catch(() => {
      /* ignore */
    });
  }

  async function cycleMode() {
    const nextMode =
      MODE_CYCLE[
        (MODE_CYCLE.indexOf(playbackMode as never) + 1) % MODE_CYCLE.length
      ];

    setPlaybackMode(nextMode);
    try {
      await SetMusicPlaybackMode(nextMode as never);
    } catch {
      /* 状态回退由事件驱动 */
    }
  }

  function cycleRate() {
    const nextRate =
      RATE_CYCLE[(RATE_CYCLE.indexOf(rate) + 1) % RATE_CYCLE.length] ?? 1;

    setRate(nextRate);
    setAudioRate(nextRate);
    saveRate(nextRate).catch(() => {
      /* ignore */
    });
  }

  function onVolumeCommit(v: number) {
    const value = Math.round(Number(v) || 0);

    setVolume(value);
    SetMusicVolume(value).catch(() => {
      /* ignore */
    });
  }

  // --- 队列管理 ---

  async function removeFromQueue(path: string) {
    const next = queueRef.current.filter((track) => track.FilePath !== path);

    setQueue(next);
    try {
      await SetPlaylist([...next]);
    } catch {
      /* ignore */
    }
  }

  async function clearQueue() {
    setQueue([]);
    try {
      await SetPlaylist([]);
    } catch {
      /* ignore */
    }
  }

  // --- 收藏 ---

  async function toggleFavorite(path: string) {
    const has = favoritesRef.current.includes(path);
    const next = has
      ? favoritesRef.current.filter((p) => p !== path)
      : [...favoritesRef.current, path];

    setFavorites(next);
    await saveFavorites(next).catch(() => {
      /* ignore */
    });
  }

  // --- 均衡器 ---

  function applyEq(preset: string, gains: number[]) {
    setEqPreset(preset);
    setEqGains(gains);
    applyEqGains(gains);
    saveEq(preset, gains).catch(() => {
      /* ignore */
    });
  }

  // ------------------------------------------------------------------
  // 进度条
  // ------------------------------------------------------------------

  function onProgressCommit(v: number | number[]) {
    const sec = Array.isArray(v) ? v[0] : v;

    setUserDragging(false);
    if (!progressEnabled) return;
    if (sec === lastSeekedValue.current) return;
    lastSeekedValue.current = sec;
    // SeekPlayback 会经 audioBridge 回发 music:seek 事件，由前端音频自行跳转
    SeekPlayback(Math.round(sec * 1000)).catch(() => {
      /* ignore */
    });
  }

  // ------------------------------------------------------------------
  // 播放器状态事件
  // ------------------------------------------------------------------

  function onStateChanged(state: unknown) {
    setPlaybackState(String(state));
  }

  function onTrackChanged(track: MusicTrack | null) {
    setCurrentTrack(track ?? null);
    if (!track) {
      setProgressValue(0);

      return;
    }
    // 记录最近播放 + 上次播放曲目（持久化）
    pushRecent(track.FilePath)
      .then(setRecent)
      .catch(() => {
        /* ignore */
      });
    saveLastTrackPath(track.FilePath).catch(() => {
      /* ignore */
    });
    // 自动切歌后跟随高亮：过滤列表里存在则滚动到可见
    document
      .querySelector(".track-tile.current")
      ?.scrollIntoView({ block: "nearest" });
  }

  function onModeChanged(mode: unknown) {
    setPlaybackMode(String(mode));
  }

  // 进度跟随音频（非拖拽时，250ms 轮询；窗口隐藏时暂停，回前台立刻补一拍）
  useEffect(
    () =>
      startVisiblePoll(() => {
        if (!userDragging && progressEnabled) {
          setProgressValue(audioState.positionMs / 1000);
        }
      }, 250),
    [userDragging, progressEnabled],
  );

  useEffect(() => {
    startAudioBridge();
    // 逐个退订：EventsOff 会连主页音乐卡片的订阅一起清掉
    const offState = EventsOn("music:stateChanged", onStateChanged);
    const offTrack = EventsOn("music:trackChanged", onTrackChanged);
    const offMode = EventsOn("music:playbackModeChanged", onModeChanged);

    return () => {
      offState();
      offTrack();
      offMode();
    };
  }, []);

  // 初始化：加载已保存的设置（音量/模式/收藏/最近/倍速/EQ）；
  // 已有文件夹则自动扫描；有上次播放曲目则恢复为"暂停就绪"状态。
  useEffect(() => {
    (async () => {
      try {
        const savedVolume = await GetMusicVolume();

        if (Number.isFinite(savedVolume)) setVolume(savedVolume);
        setPlaybackMode(String(await GetMusicPlaybackMode()));
        setFolderPath(await GetMusicFolderPath());
        setSortMode(String(await GetMusicSortMode()));
        const savedFolder = await GetMusicFolderPath();

        // 用户数据并行加载
        loadFavorites()
          .then(setFavorites)
          .catch(() => {
            /* ignore */
          });
        loadRecent()
          .then(setRecent)
          .catch(() => {
            /* ignore */
          });
        loadRate()
          .then((r) => {
            setRate(r);
            setAudioRate(r);
          })
          .catch(() => {
            /* ignore */
          });
        loadEq()
          .then(({ preset, gains }) => {
            setEqPreset(preset);
            setEqGains(gains);
            applyEqGains(gains);
          })
          .catch(() => {
            /* ignore */
          });

        if (savedFolder) await refreshLibrary();
        // 共享播放器已有曲目在播时同步队列镜像
        const playlist = asArray<MusicTrack>(await GetPlaylist());

        if (playlist.length) {
          setQueue(playlist);
          if (!currentTrackRef.current) {
            const track = await GetCurrentTrack();

            if (track) setCurrentTrack(track);
          }
        } else if (savedFolder) {
          // 重启恢复：无共享曲目时，把上次播放的曲目加载为暂停态（不发声起播）
          const lastPath = await loadLastTrackPath();

          if (lastPath) {
            const track = libraryRef.current.find(
              (item) => item.FilePath === lastPath,
            );

            if (track) {
              try {
                await PlayTrack(track);
                await PausePlayback();
              } catch {
                /* 恢复失败忽略 */
              }
            }
          }
        }
      } catch (ex) {
        notify.error(
          t("音乐页初始化失败：{0}", { "0": (ex as Error)?.message ?? ex }),
        );
      }
    })();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // ------------------------------------------------------------------
  // 键盘快捷键（仅音乐页挂载期间生效）
  // ------------------------------------------------------------------

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null;

      if (
        target &&
        (target.tagName === "INPUT" ||
          target.tagName === "TEXTAREA" ||
          target.tagName === "SELECT" ||
          target.isContentEditable)
      )
        return;
      if (event.code === "Space") {
        event.preventDefault();
        togglePlayPause();
      } else if (event.key === "ArrowLeft") {
        if ((target as HTMLElement).closest?.('[role="slider"]')) return;
        event.preventDefault();
        nudge(-10);
      } else if (event.key === "ArrowRight") {
        if ((target as HTMLElement).closest?.('[role="slider"]')) return;
        event.preventDefault();
        nudge(10);
      }
    };

    window.addEventListener("keydown", onKey);

    return () => window.removeEventListener("keydown", onKey);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [progressEnabled, durationSec]);

  // --- 歌词（右栏预览与全屏页共用同一 .lrc 加载逻辑） ---
  const { lrc, missing: lrcMissing } = useLrc(currentTrack?.FilePath ?? "");

  // ------------------------------------------------------------------
  // 渲染
  // ------------------------------------------------------------------

  const emptyHint =
    tab === "library"
      ? library.length > 0
        ? t("没有匹配的歌曲")
        : t("暂无歌曲")
      : tab === "favorites"
        ? t("还没有收藏，点击曲目右侧的心心加入收藏")
        : tab === "recent"
          ? t("暂无播放记录")
          : t("队列为空，从曲库点播歌曲加入");

  const tabLabel: Record<ViewTab, string> = {
    library: t("曲库"),
    favorites: t("收藏"),
    recent: t("最近播放"),
    queue: t("播放队列"),
  };

  return (
    /* 三栏布局（左导航 / 中列表 / 右播放面板）+ 底部全局控制条 */
    <section className="flex h-full min-h-0 flex-col gap-3 overflow-hidden px-6 py-4">
      {!miniMode ? (
        <>
          {/* 顶栏 */}
          <header className="flex flex-none items-center gap-3">
            <div className="mr-auto flex min-w-0 flex-1 items-center gap-3">
              <div className="flex min-w-0 flex-col gap-0.5">
                <span className="overflow-hidden text-xl font-bold tracking-tight text-ellipsis whitespace-nowrap">
                  {t("音乐播放器")}
                </span>
                {trackCountText ? (
                  <span className="max-w-[380px] overflow-hidden text-[11px] text-gray-400 text-ellipsis whitespace-nowrap">
                    {trackCountText}
                  </span>
                ) : null}
              </div>
            </div>

            <Button
              radius="full"
              size="sm"
              startContent={<FolderIcon />}
              variant="flat"
              onPress={selectFolder}
            >
              {t("选择文件夹")}
            </Button>
            <Button
              isIconOnly
              aria-label={t("刷新")}
              radius="full"
              size="sm"
              variant="flat"
              onPress={refreshLibrary}
            >
              <RefreshIcon />
            </Button>
            <Select
              disallowEmptySelection
              aria-label={t("排序方式")}
              className="w-[130px]"
              popoverProps={selectPopoverProps}
              radius="full"
              selectedKeys={new Set([sortMode])}
              size="sm"
              onSelectionChange={(keys) =>
                onSortChanged(String(Array.from(keys)[0] ?? ""))
              }
            >
              {SORT_OPTIONS.map((option) => (
                <SelectItem key={option.value}>{option.label}</SelectItem>
              ))}
            </Select>
          </header>

          {/* 三栏主体 */}
          <div className="flex min-h-0 flex-1 gap-3">
            {/* 左侧窄栏：视图切换 */}
            <nav className="hidden w-14 flex-none flex-col items-center gap-1.5 rounded-large border nya-border nya-panel py-3 shadow-sm backdrop-blur-md md:flex">
              {(
                [
                  { id: "library", icon: <LibraryIcon />, tip: t("曲库") },
                  { id: "favorites", icon: <HeartIcon />, tip: t("收藏") },
                  { id: "recent", icon: <HistoryIcon />, tip: t("最近播放") },
                  { id: "queue", icon: <QueueIcon />, tip: t("播放队列") },
                ] as { id: ViewTab; icon: React.ReactNode; tip: string }[]
              ).map((item) => (
                <Tooltip
                  key={item.id}
                  content={item.tip}
                  delay={300}
                  motionProps={tooltipMotionProps}
                  placement="right"
                >
                  <button
                    aria-label={item.tip}
                    className={`flex size-10 cursor-pointer items-center justify-center rounded-lg transition-all active:scale-90 ${
                      tab === item.id
                        ? "bg-primary/15 text-primary shadow-inner"
                        : "text-gray-400 hover:bg-default-100/80 hover:text-gray-500"
                    }`}
                    type="button"
                    onClick={() => setTab(item.id)}
                  >
                    {item.icon}
                  </button>
                </Tooltip>
              ))}
              {/* 收藏数角标 */}
              {favorites.length > 0 ? (
                <span className="mt-auto rounded-full bg-default-100 px-2 py-0.5 text-[10px] text-gray-400 tabular-nums">
                  ♥ {favorites.length}
                </span>
              ) : null}
            </nav>

            {/* 中间曲目列表 */}
            <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-2.5">
              <Input
                aria-label={t("搜索歌曲")}
                classNames={{
                  inputWrapper:
                    "bg-default-100/80 data-[hover=true]:bg-default-200",
                }}
                placeholder={`${t("搜索歌曲")} · ${tabLabel[tab]}`}
                radius="full"
                size="sm"
                startContent={<SearchIcon className="text-gray-400" />}
                value={keyword}
                onValueChange={(v) => applyFilter(v)}
              />

              <div className="relative min-h-0 flex-1 overflow-hidden rounded-large border nya-border nya-panel shadow-sm backdrop-blur-md">
                <div className="nya-scroll h-full overflow-y-auto p-1.5">
                  <AnimatePresence initial={false}>
                    {viewTracks.map((track) => (
                      <motion.div
                        key={track.FilePath}
                        layout
                        animate="center"
                        className="overflow-hidden"
                        exit="exit"
                        initial="enter"
                        variants={listItemVariants}
                      >
                        <div
                          className={`track-tile group mx-0.5 my-[3px] flex cursor-pointer items-center gap-3 rounded-lg px-2.5 py-1.5 transition-all duration-150 ${
                            isCurrent(track)
                              ? "bg-primary/10 current"
                              : "hover:bg-default-100/80 hover:translate-x-0.5"
                          }`}
                          role="button"
                          tabIndex={0}
                          onClick={() => void onTrackClick(track)}
                          onKeyDown={(event) => {
                            if (event.key === "Enter" || event.key === " ") {
                              event.preventDefault();
                              void onTrackClick(track);
                            }
                          }}
                        >
                          <div
                            className={`flex size-[38px] flex-none items-center justify-center rounded-lg transition-colors ${
                              isCurrent(track)
                                ? "bg-primary text-primary-foreground shadow-md shadow-primary/25"
                                : "bg-default-100 text-gray-400 group-hover:text-gray-500"
                            }`}
                          >
                            {!isCurrent(track) ? (
                              <MusicNoteIcon className="h-[18px] w-[18px]" />
                            ) : (
                              /* 均衡器动画（播放中） */
                              <span className="flex h-[20px] items-end justify-center gap-[3px] pb-[6px]">
                                {[0, 1, 2].map((i) => (
                                  <span
                                    key={i}
                                    className={`nya-eq-bar ${isPlaying ? "" : "[animation-play-state:paused]"}`}
                                  />
                                ))}
                              </span>
                            )}
                          </div>

                          <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                            <span
                              className={`overflow-hidden text-[13px] font-semibold text-ellipsis whitespace-nowrap ${isCurrent(track) ? "text-primary" : ""}`}
                            >
                              {trackTitle(track)}
                            </span>
                            <span className="overflow-hidden text-[11px] text-gray-400 text-ellipsis whitespace-nowrap">
                              {tab === "recent"
                                ? t("{0} 前播放", {
                                    "0": relativeTime(
                                      recent.find(
                                        (e) => e.path === track.FilePath,
                                      )?.ts ?? 0,
                                    ),
                                  })
                                : trackMeta(track)}
                            </span>
                          </div>

                          {/* 收藏标记 */}
                          <button
                            aria-label={
                              favorites.includes(track.FilePath)
                                ? t("取消收藏")
                                : t("收藏")
                            }
                            className={`flex-none cursor-pointer rounded-full p-1 transition-colors ${
                              favorites.includes(track.FilePath)
                                ? "text-rose-500"
                                : "text-gray-300 opacity-0 group-hover:opacity-100 hover:text-rose-400 dark:text-gray-600"
                            }`}
                            type="button"
                            onClick={(event) => {
                              event.stopPropagation();
                              void toggleFavorite(track.FilePath);
                            }}
                          >
                            {favorites.includes(track.FilePath) ? (
                              <HeartFilledIcon className="h-4 w-4" />
                            ) : (
                              <HeartIcon className="h-4 w-4" />
                            )}
                          </button>

                          {/* 队列删除 */}
                          {tab === "queue" ? (
                            <button
                              aria-label={t("从队列移除")}
                              className="flex-none cursor-pointer rounded-full px-2 py-0.5 text-[11px] text-gray-400 opacity-0 transition-colors hover:text-danger group-hover:opacity-100"
                              type="button"
                              onClick={(event) => {
                                event.stopPropagation();
                                void removeFromQueue(track.FilePath);
                              }}
                            >
                              ✕
                            </button>
                          ) : isCurrent(track) ? (
                            <span className="flex-none text-primary opacity-0 transition-opacity group-hover:opacity-100">
                              <VolumeHighIcon />
                            </span>
                          ) : null}
                        </div>
                      </motion.div>
                    ))}
                  </AnimatePresence>
                </div>

                {/* 空态 / 队列清空 */}
                {viewTracks.length === 0 ? (
                  <div className="nya-enter absolute inset-0 flex flex-col items-center justify-center gap-3 text-center text-gray-400 backdrop-blur-[2px]">
                    <div className="flex size-16 items-center justify-center rounded-lg bg-gradient-to-br from-default-200 to-default-100 shadow-inner dark:from-gray-800 dark:to-gray-800/50">
                      <MusicNoteIcon className="h-8 w-8" />
                    </div>
                    <span className="text-[15px] font-semibold text-gray-500 dark:text-gray-400">
                      {emptyHint}
                    </span>
                    {tab === "queue" &&
                    queue.length === 0 &&
                    library.length > 0 ? (
                      <Button
                        color="primary"
                        radius="full"
                        size="sm"
                        variant="flat"
                        onPress={() => {
                          // 快捷：用完整曲库填满队列
                          SetPlaylist([...library]).catch(() => {
                            /* ignore */
                          });
                          setQueue([...library]);
                        }}
                      >
                        {t("载入全部曲目")}
                      </Button>
                    ) : null}
                  </div>
                ) : null}

                {/* 队列顶部操作条 */}
                {tab === "queue" && queue.length > 0 ? (
                  <div className="absolute top-2 right-3 z-10">
                    <Button
                      className="h-7 min-w-0 px-3 text-[11px] text-gray-400"
                      radius="full"
                      size="sm"
                      variant="flat"
                      onPress={() => void clearQueue()}
                    >
                      {t("清空队列")}
                    </Button>
                  </div>
                ) : null}
              </div>
            </div>

            {/* 右侧当前播放面板（cover 色块 + 歌词，xl 以下隐藏） */}
            <aside className="hidden w-[300px] flex-none flex-col gap-3 overflow-hidden rounded-large border nya-border nya-panel p-4 shadow-sm backdrop-blur-md xl:flex">
              <NowPlayingPanel
                isFavorite={isFavCurrent}
                lrc={lrc}
                lrcMissing={lrcMissing}
                playing={isPlaying}
                positionMs={audio.positionMs}
                track={currentTrack}
                onOpenFull={() => setNowPlayingOpen(true)}
                onSeekMs={(ms) => SeekPlayback(ms).catch(() => undefined)}
                onToggleFavorite={() =>
                  currentTrack && void toggleFavorite(currentTrack.FilePath)
                }
              />
            </aside>
          </div>
        </>
      ) : (
        /* 迷你模式头部：仅一行标题，让出空间给紧凑控制条 */
        <header className="flex flex-none items-center gap-2">
          <span className="text-sm font-bold tracking-tight">
            {t("迷你模式")}
          </span>
        </header>
      )}

      {/* 底部全局控制条 */}
      <footer
        className={`flex flex-none flex-col gap-1.5 rounded-large border nya-border nya-panel px-4 shadow-sm backdrop-blur-md ${miniMode ? "py-2.5" : "py-2.5"}`}
      >
        {miniMode ? (
          /* 迷你模式：单行紧凑条 */
          <div className="flex items-center gap-3">
            <button
              aria-label={t("打开播放页")}
              className="nya-cover-glow flex size-9 flex-none cursor-pointer items-center justify-center rounded-full bg-primary text-primary-foreground shadow-md"
              type="button"
              onClick={() => setNowPlayingOpen(true)}
            >
              <MusicNoteIcon className="h-4 w-4" />
            </button>
            <div className="min-w-0 flex-1">
              <div className="overflow-hidden text-[13px] font-semibold text-ellipsis whitespace-nowrap">
                {nowTitle}
              </div>
              <div className="overflow-hidden text-[10px] text-gray-400 text-ellipsis whitespace-nowrap">
                {nowInfo}
              </div>
            </div>
            <Button
              isIconOnly
              aria-label={t("上一首")}
              className="min-w-unit-8 h-unit-8"
              radius="full"
              size="sm"
              variant="flat"
              onPress={previous}
            >
              <SkipPreviousIcon />
            </Button>
            <Button
              isIconOnly
              aria-label={isPlaying ? t("暂停") : t("播放")}
              className="min-w-unit-9 h-unit-9"
              color="primary"
              radius="full"
              size="sm"
              onPress={togglePlayPause}
            >
              {isPlaying ? (
                <PauseIcon />
              ) : (
                <span className="inline-flex translate-x-px">
                  <PlayIcon />
                </span>
              )}
            </Button>
            <Button
              isIconOnly
              aria-label={t("下一首")}
              className="min-w-unit-8 h-unit-8"
              radius="full"
              size="sm"
              variant="flat"
              onPress={next}
            >
              <SkipNextIcon />
            </Button>
            <div className="hidden w-[120px] flex-none sm:block">
              <Slider
                aria-label={t("播放进度")}
                color="primary"
                isDisabled={!progressEnabled}
                maxValue={durationSec > 0 ? durationSec : 100}
                minValue={0}
                size="sm"
                step={0.1}
                value={progressValue}
                onChange={(v) => {
                  setUserDragging(true);
                  setProgressValue(Array.isArray(v) ? v[0] : v);
                }}
                onChangeEnd={(v) => onProgressCommit(v)}
              />
            </div>
            <Tooltip
              content={t("退出迷你模式")}
              delay={300}
              motionProps={tooltipMotionProps}
            >
              <Button
                isIconOnly
                aria-label={t("退出迷你模式")}
                className="min-w-unit-8 h-unit-8"
                radius="full"
                size="sm"
                variant="light"
                onPress={() => setMiniMode(false)}
              >
                <ExitMiniIcon />
              </Button>
            </Tooltip>
          </div>
        ) : (
          <>
            {/* 行 1：进度条（含缓冲段） */}
            <div className="flex items-center gap-3">
              <span className="w-[40px] flex-none text-center font-mono text-[11px] text-gray-400 tabular-nums">
                {formatTime(positionSec)}
              </span>
              <div className="relative min-w-0 flex-1 py-1.5">
                {/* 缓冲段（Slider 轨道透明，露出底下的缓冲条） */}
                <div className="pointer-events-none absolute inset-x-0 top-1/2 h-1 -translate-y-1/2 overflow-hidden rounded-full bg-default-200/70 dark:bg-gray-700/60">
                  <div
                    className="h-full rounded-full bg-default-300/80 dark:bg-gray-600/80 transition-[width] duration-300"
                    style={{ width: `${bufferedPct}%` }}
                  />
                </div>
                <Slider
                  aria-label={t("播放进度")}
                  classNames={{ track: "bg-transparent" }}
                  color="primary"
                  isDisabled={!progressEnabled}
                  maxValue={durationSec > 0 ? durationSec : 100}
                  minValue={0}
                  size="sm"
                  step={0.1}
                  value={progressValue}
                  onChange={(v) => {
                    setUserDragging(true);
                    setProgressValue(Array.isArray(v) ? v[0] : v);
                  }}
                  onChangeEnd={(v) => onProgressCommit(v)}
                />
              </div>
              <span className="w-[40px] flex-none text-right font-mono text-[11px] text-gray-400 tabular-nums">
                {durationSec > 0 ? formatTime(durationSec) : "--:--"}
              </span>
            </div>

            {/* 行 2：控制按钮 + 曲目信息 + 功能区 + 音量 */}
            <div className="flex items-center gap-4">
              <div className="flex flex-none items-center gap-1.5">
                <Tooltip
                  content={t("上一首")}
                  delay={300}
                  motionProps={tooltipMotionProps}
                >
                  <Button
                    isIconOnly
                    className="min-w-unit-9 h-unit-9 active:scale-90"
                    radius="full"
                    variant="flat"
                    onPress={previous}
                  >
                    <SkipPreviousIcon />
                  </Button>
                </Tooltip>
                <Tooltip
                  content={t("快退 10 秒")}
                  delay={300}
                  motionProps={tooltipMotionProps}
                >
                  <Button
                    isIconOnly
                    className="min-w-unit-8 h-unit-8 text-gray-400 active:scale-90"
                    isDisabled={!progressEnabled}
                    radius="full"
                    size="sm"
                    variant="light"
                    onPress={() => nudge(-10)}
                  >
                    <RewindIcon />
                  </Button>
                </Tooltip>
                <Tooltip
                  content={isPlaying ? t("暂停") : t("播放")}
                  delay={300}
                  motionProps={tooltipMotionProps}
                >
                  <Button
                    isIconOnly
                    className="min-w-unit-11 h-unit-11 shadow-md shadow-primary/25 active:scale-90"
                    color="primary"
                    radius="full"
                    onPress={togglePlayPause}
                  >
                    {isPlaying ? (
                      <PauseIcon />
                    ) : (
                      <span className="inline-flex translate-x-px">
                        <PlayIcon />
                      </span>
                    )}
                  </Button>
                </Tooltip>
                <Tooltip
                  content={t("快进 10 秒")}
                  delay={300}
                  motionProps={tooltipMotionProps}
                >
                  <Button
                    isIconOnly
                    className="min-w-unit-8 h-unit-8 text-gray-400 active:scale-90"
                    isDisabled={!progressEnabled}
                    radius="full"
                    size="sm"
                    variant="light"
                    onPress={() => nudge(10)}
                  >
                    <ForwardIcon />
                  </Button>
                </Tooltip>
                <Tooltip
                  content={t("下一首")}
                  delay={300}
                  motionProps={tooltipMotionProps}
                >
                  <Button
                    isIconOnly
                    className="min-w-unit-9 h-unit-9 active:scale-90"
                    radius="full"
                    variant="flat"
                    onPress={next}
                  >
                    <SkipNextIcon />
                  </Button>
                </Tooltip>
                <Tooltip
                  content={t("停止")}
                  delay={300}
                  motionProps={tooltipMotionProps}
                >
                  <Button
                    isIconOnly
                    className="min-w-unit-8 h-unit-8 text-gray-400 active:scale-90"
                    isDisabled={playbackState === "Stopped"}
                    radius="full"
                    size="sm"
                    variant="light"
                    onPress={stop}
                  >
                    <StopIcon />
                  </Button>
                </Tooltip>
                {/* 模式循环按钮：点击切换，Tooltip 显示当前模式 */}
                <Tooltip
                  content={modeTip}
                  delay={300}
                  motionProps={tooltipMotionProps}
                >
                  <Button
                    isIconOnly
                    className={`relative min-w-unit-8 h-unit-8 active:scale-90 ${playbackMode !== "Sequential" ? "text-primary" : "text-gray-400"}`}
                    radius="full"
                    size="sm"
                    variant="light"
                    onPress={cycleMode}
                  >
                    {modeIcon}
                    {playbackMode !== "Sequential" ? (
                      <span className="absolute right-1 bottom-1 size-[7px] rounded-full bg-primary" />
                    ) : null}
                  </Button>
                </Tooltip>
              </div>

              {/* 当前曲目信息 + 唱片封面（点击唤起黑胶播放页） */}
              <div className="flex min-w-0 flex-1 items-center gap-3">
                <button
                  aria-label={t("打开播放页")}
                  className={`nya-cover-glow relative flex size-[42px] flex-none cursor-pointer items-center justify-center overflow-hidden rounded-full bg-primary text-primary-foreground shadow-md transition-transform hover:scale-105 active:scale-95 ${isPlaying ? "" : ""}`}
                  type="button"
                  onClick={() => setNowPlayingOpen(true)}
                >
                  {/* 黑胶纹理环 */}
                  <span
                    className={`absolute inset-[3px] rounded-full border border-white/25 ${isPlaying ? "nya-vinyl" : ""}`}
                  >
                    <span className="absolute inset-[14%] rounded-full border border-white/15" />
                    <span className="absolute inset-[28%] rounded-full border border-white/10" />
                  </span>
                  <span className="relative z-10 flex size-[15px] items-center justify-center rounded-full bg-white/90 text-primary">
                    <MusicNoteIcon className="h-[10px] w-[10px]" />
                  </span>
                </button>
                <div className="flex min-w-0 flex-col gap-0.5">
                  {/* 换曲时旧信息上滑淡出、新信息滑入；长歌名横向滚动 */}
                  <AnimatePresence initial={false} mode="popLayout">
                    <motion.span
                      key={currentTrack?.FilePath ?? "idle"}
                      animate={{ opacity: 1, y: 0 }}
                      className="overflow-hidden text-sm font-semibold text-ellipsis whitespace-nowrap"
                      exit={{ opacity: 0, y: -8 }}
                      initial={{ opacity: 0, y: 8 }}
                      transition={{ duration: 0.18 }}
                    >
                      {nowTitle}
                    </motion.span>
                    <motion.span
                      key={`${currentTrack?.FilePath ?? "idle"}-info`}
                      animate={{ opacity: 1, y: 0 }}
                      className="overflow-hidden text-[11px] text-gray-400 text-ellipsis whitespace-nowrap"
                      exit={{ opacity: 0, y: -8 }}
                      initial={{ opacity: 0, y: 8 }}
                      transition={{ duration: 0.18 }}
                    >
                      {nowInfo}
                    </motion.span>
                  </AnimatePresence>
                </div>
                {/* 收藏当前曲目 */}
                {currentTrack ? (
                  <Tooltip
                    content={isFavCurrent ? t("取消收藏") : t("收藏")}
                    delay={300}
                    motionProps={tooltipMotionProps}
                  >
                    <Button
                      isIconOnly
                      aria-label={isFavCurrent ? t("取消收藏") : t("收藏")}
                      className={`min-w-unit-8 h-unit-8 shrink-0 ${isFavCurrent ? "text-rose-500" : "text-gray-400"}`}
                      radius="full"
                      size="sm"
                      variant="light"
                      onPress={() =>
                        currentTrack &&
                        void toggleFavorite(currentTrack.FilePath)
                      }
                    >
                      {isFavCurrent ? <HeartFilledIcon /> : <HeartIcon />}
                    </Button>
                  </Tooltip>
                ) : null}
              </div>

              {/* 功能区：倍速 / 均衡器 / 歌词 / 迷你模式 */}
              <div className="flex flex-none items-center gap-1">
                <Tooltip
                  content={`${t("倍速播放")}：${rate.toFixed(2).replace(/0+$/, "").replace(/\.$/, "")}x（${t("点击切换")}）`}
                  delay={300}
                  motionProps={tooltipMotionProps}
                >
                  <Button
                    isIconOnly
                    className={`min-w-unit-8 h-unit-8 ${rate !== 1 ? "text-primary" : "text-gray-400"}`}
                    radius="full"
                    size="sm"
                    variant="light"
                    onPress={cycleRate}
                  >
                    <SpeedIcon />
                  </Button>
                </Tooltip>
                <span className="w-8 flex-none text-center text-[10px] text-gray-400 tabular-nums">
                  {rate}x
                </span>
                <Popover
                  isOpen={eqOpen}
                  motionProps={popoverMotionProps}
                  offset={10}
                  placement="top"
                  onOpenChange={setEqOpen}
                >
                  <PopoverTrigger>
                    <Button
                      isIconOnly
                      aria-label={t("均衡器")}
                      className={`min-w-unit-8 h-unit-8 ${eqPreset !== "Off" ? "text-primary" : "text-gray-400"}`}
                      radius="full"
                      size="sm"
                      variant="light"
                    >
                      <EqIcon />
                    </Button>
                  </PopoverTrigger>
                  <PopoverContent className="border nya-border p-4">
                    <div className="flex w-[300px] flex-col gap-3">
                      <div className="flex items-center justify-between">
                        <span className="text-[13px] font-semibold">
                          {t("均衡器")}
                        </span>
                        <Select
                          disallowEmptySelection
                          aria-label={t("预设")}
                          className="w-[110px]"
                          popoverProps={selectPopoverProps}
                          radius="full"
                          selectedKeys={new Set([eqPreset])}
                          size="sm"
                          onSelectionChange={(keys) => {
                            const name =
                              String(Array.from(keys)[0] ?? "") || "Off";

                            applyEq(name, EQ_PRESETS[name] ?? [0, 0, 0, 0, 0]);
                          }}
                        >
                          {[...EQ_PRESET_NAMES, "Custom"].map((name) => (
                            <SelectItem key={name}>
                              {EQ_PRESET_LABELS[name] ?? name}
                            </SelectItem>
                          ))}
                        </Select>
                      </div>
                      <div className="flex items-end justify-between gap-2">
                        {EQ_BANDS.map((band, i) => (
                          <div
                            key={band.freq}
                            className="flex flex-1 flex-col items-center gap-1"
                          >
                            <span className="text-[10px] text-gray-400 tabular-nums">
                              {eqGains[i] > 0 ? "+" : ""}
                              {eqGains[i]?.toFixed(0) ?? 0}
                            </span>
                            <div className="h-16">
                              <Slider
                                aria-label={band.label}
                                className="h-16"
                                classNames={{
                                  track: "w-1",
                                  thumb: "w-3 h-3 after:hidden",
                                }}
                                color="primary"
                                formatOptions={{ maximumFractionDigits: 0 }}
                                maxValue={12}
                                minValue={-12}
                                orientation="vertical"
                                size="sm"
                                step={1}
                                value={eqGains[i] ?? 0}
                                onChange={(v) => {
                                  const next = [...eqGains];

                                  next[i] = Array.isArray(v) ? v[0] : v;
                                  setEqGains(next);
                                  setEqPreset("Custom");
                                  applyEqGains(next);
                                }}
                                onChangeEnd={(v) => {
                                  const next = [...eqGains];

                                  next[i] = Array.isArray(v) ? v[0] : v;
                                  applyEq("Custom", next);
                                }}
                              />
                            </div>
                            <span className="text-[9px] text-gray-400">
                              {band.label}
                            </span>
                          </div>
                        ))}
                      </div>
                      <span className="text-[10px] leading-relaxed text-gray-400">
                        {t("均衡器由 Web Audio 实时处理，预设与增益自动保存。")}
                      </span>
                    </div>
                  </PopoverContent>
                </Popover>
                <Tooltip
                  content={t("歌词全屏")}
                  delay={300}
                  motionProps={tooltipMotionProps}
                >
                  <Button
                    isIconOnly
                    aria-label={t("歌词全屏")}
                    className="min-w-unit-8 h-unit-8 text-gray-400"
                    radius="full"
                    size="sm"
                    variant="light"
                    onPress={() => setNowPlayingOpen(true)}
                  >
                    <span className="text-[11px] font-bold">词</span>
                  </Button>
                </Tooltip>
                <Tooltip
                  content={t("迷你模式")}
                  delay={300}
                  motionProps={tooltipMotionProps}
                >
                  <Button
                    isIconOnly
                    aria-label={t("迷你模式")}
                    className="min-w-unit-8 h-unit-8 text-gray-400"
                    radius="full"
                    size="sm"
                    variant="light"
                    onPress={() => setMiniMode(true)}
                  >
                    <MiniModeIcon />
                  </Button>
                </Tooltip>
              </div>

              {/* 音量 */}
              <div className="hidden flex-none items-center gap-2 lg:flex">
                <span className="text-gray-400">
                  <VolumeIcon />
                </span>
                <div className="w-[88px] flex-none">
                  <Slider
                    aria-label={t("音量")}
                    color="primary"
                    maxValue={100}
                    minValue={0}
                    size="sm"
                    step={1}
                    value={volume}
                    onChange={(v) => setVolume(Array.isArray(v) ? v[0] : v)}
                    onChangeEnd={(v) =>
                      onVolumeCommit(Array.isArray(v) ? v[0] : v)
                    }
                  />
                </div>
                <span className="w-[32px] flex-none text-[11px] text-gray-400 tabular-nums">
                  {volume}%
                </span>
              </div>
            </div>
          </>
        )}
      </footer>

      {/* 黑胶播放页（全屏浮层） */}
      <AnimatePresence>
        {nowPlayingOpen ? (
          <NowPlayingView
            isFavorite={isFavCurrent}
            queue={queue}
            track={currentTrack}
            onClose={() => setNowPlayingOpen(false)}
            onPlayTrack={(item) => void playInContext(queue, item)}
            onToggleFavorite={() =>
              currentTrack && void toggleFavorite(currentTrack.FilePath)
            }
          />
        ) : null}
      </AnimatePresence>
    </section>
  );
};

// ------------------------------------------------------------------
// 右栏"当前播放"面板：封面色块 + 曲目信息 + 歌词预览
// ------------------------------------------------------------------

const NowPlayingPanel: React.FC<{
  track: MusicTrack | null;
  playing: boolean;
  isFavorite: boolean;
  onToggleFavorite: () => void;
  lrc: ReturnType<typeof useLrc>["lrc"];
  lrcMissing: boolean;
  positionMs: number;
  onSeekMs: (ms: number) => void;
  onOpenFull: () => void;
}> = ({
  track,
  playing,
  isFavorite,
  onToggleFavorite,
  lrc,
  lrcMissing,
  positionMs,
  onSeekMs,
  onOpenFull,
}) => {
  const [coverUrl, setCoverUrl] = useState<string | null>(null);
  const [coverFailed, setCoverFailed] = useState(false);

  // 封面加载（lib/coverArt 提取，带缓存）
  useEffect(() => {
    let alive = true;
    const path = track?.FilePath ?? "";

    setCoverUrl(null);
    setCoverFailed(false);
    if (!path) return;
    getCoverUrl(path)
      .then((url) => {
        if (alive) {
          setCoverUrl(url);
          setCoverFailed(!url);
        }
      })
      .catch(() => {
        if (alive) setCoverFailed(true);
      });

    return () => {
      alive = false;
    };
  }, [track?.FilePath]);

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3">
      {/* 封面色块（无封面时渐变 + 音符） */}
      <button
        aria-label={t("打开播放页")}
        className="nya-cover-glow group relative aspect-square w-full flex-none cursor-pointer overflow-hidden rounded-lg bg-gradient-to-br from-violet-400/80 via-purple-500/80 to-fuchsia-500/80 shadow-lg shadow-purple-500/20"
        type="button"
        onClick={onOpenFull}
      >
        {coverUrl ? (
          <img
            alt={track ? trackTitle(track) : ""}
            className={`size-full object-cover transition-transform duration-500 ${playing ? "group-hover:scale-105" : ""}`}
            src={coverUrl}
          />
        ) : coverFailed ? (
          <span className="absolute inset-0 flex items-center justify-center text-white/80">
            <MusicNoteIcon className="h-12 w-12" />
          </span>
        ) : (
          <span className="absolute inset-0 flex items-center justify-center">
            <span className="size-10 animate-pulse rounded-full bg-white/40" />
          </span>
        )}
        {/* 播放中呼吸光斑 */}
        {playing ? (
          <span className="pointer-events-none absolute inset-0 rounded-lg ring-2 ring-white/30" />
        ) : null}
      </button>

      {/* 标题 + 收藏 */}
      <div className="flex flex-none items-start gap-2">
        <div className="min-w-0 flex-1">
          <div className="overflow-hidden text-[15px] font-bold text-ellipsis whitespace-nowrap">
            {track ? trackTitle(track) : t("未在播放")}
          </div>
          <div className="overflow-hidden text-[11px] text-gray-400 text-ellipsis whitespace-nowrap">
            {track ? trackMeta(track) : t("从曲库选一首开始听吧")}
          </div>
        </div>
        {track ? (
          <Button
            isIconOnly
            aria-label={isFavorite ? t("取消收藏") : t("收藏")}
            className={`min-w-unit-8 h-unit-8 shrink-0 ${isFavorite ? "text-rose-500" : "text-gray-400"}`}
            radius="full"
            size="sm"
            variant="light"
            onPress={onToggleFavorite}
          >
            {isFavorite ? <HeartFilledIcon /> : <HeartIcon />}
          </Button>
        ) : null}
      </div>

      {/* 歌词预览（点击行跳转进度） */}
      <LyricLines
        className="min-h-0 flex-1 rounded-lg"
        lineClassName="text-[13px]"
        lrc={lrc}
        missing={lrcMissing || !track}
        positionMs={positionMs}
        onSeekMs={onSeekMs}
      />
    </div>
  );
};

/** 简易相对时间（最近播放列表副标题用）。 */
function relativeTime(ts: number): string {
  if (!ts) return t("很久");
  const diff = Date.now() - ts;
  const min = Math.floor(diff / 60000);

  if (min < 1) return t("刚刚");
  if (min < 60) return `${min} ${t("分钟")}`;
  const hour = Math.floor(min / 60);

  if (hour < 24) return `${hour} ${t("小时")}`;
  const day = Math.floor(hour / 24);

  if (day < 30) return `${day} ${t("天")}`;

  return `${Math.floor(day / 30)} ${t("个月")}`;
}

export default MusicPage;
