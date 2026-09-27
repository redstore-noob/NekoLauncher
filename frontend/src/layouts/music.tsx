/*
 * 本地音乐播放页（MusicView 移植，UI 从 shadcn 版改写为 HeroUI）。
 * 曲库扫描/排序/搜索走 MusicAPI；播放控制调 PlayTrack/Pause 等后端方法，
 * 实际发声由 lib/audioBridge.ts 的 <audio> 单例完成（监听 music:play 等事件）。
 */
import type { music } from "../../wailsjs/go/models";

import React, { useCallback, useEffect, useRef, useState } from "react";
import {
  Button,
  Input,
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
  SpeakerMute20Regular as VolumeOffIcon,
  Speaker120Regular as VolumeLowIcon,
  Speaker220Regular as VolumeHighIcon,
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
  GetPlaylist,
  SetPlaylist,
  SeekPlayback,
} from "../../wailsjs/go/bindings/MusicAPI";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import {
  audioState,
  startAudioBridge,
  useAudioState,
} from "../lib/audioBridge";
import { asArray } from "../lib/guards";
import {
  listItemVariants,
  popoverMotionProps,
  tooltipMotionProps,
} from "../lib/motion";
import { t } from "../i18n";

type MusicTrack = music.MusicTrack;

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

// 曲目显示工具（对应 music.MusicTrack.Title / MetaDisplay）
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

  const [library, setLibrary] = useState<MusicTrack[]>([]); // 完整曲库（共享播放列表）
  const [displayTracks, setDisplayTracks] = useState<MusicTrack[]>([]); // 搜索过滤后的展示列表
  const [keyword, setKeyword] = useState("");
  const [sortMode, setSortMode] = useState<string>("FileName");
  const [folderPath, setFolderPath] = useState("");
  const [playbackMode, setPlaybackMode] = useState("Sequential");
  const [volume, setVolume] = useState(80);
  const [currentTrack, setCurrentTrack] = useState<MusicTrack | null>(null);
  const [playbackState, setPlaybackState] = useState("Stopped");

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

  // --- Web Audio 桥状态 ---
  const isPlaying = audio.playing;
  const positionSec = userDragging ? progressValue : audio.positionMs / 1000;
  const durationSec = audio.durationMs / 1000;

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

  function isCurrent(track: MusicTrack): boolean {
    return !!currentTrack && track.FilePath === currentTrack.FilePath;
  }

  // ------------------------------------------------------------------
  // 曲库加载 / 排序 / 搜索
  // ------------------------------------------------------------------

  const refreshLibrary = useCallback(async () => {
    try {
      ScanMusicLibrary();
      setFolderPath(await GetMusicFolderPath());
      const mode = await GetMusicSortMode();

      setSortMode(mode as string);
      const full = asArray<MusicTrack>(await GetMusicTracks());

      setLibrary(full);
      setDisplayTracks(asArray<MusicTrack>(await GetSortedMusicTracks(mode)));
      // 共享播放列表 = 完整（未过滤）曲目列表，供自动切歌/上下一首使用
      SetPlaylist([...full]);
    } catch (ex) {
      console.error(t("扫描曲库失败"), ex);
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
      console.error(t("选择文件夹失败"), ex);
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
  }

  // 搜索是"打字即发"的：慢的旧请求可能后于新请求返回，把列表刷成过期结果，
  // 用一个自增序号，回来时不是最新那次就丢弃
  const filterSeqRef = useRef(0);

  async function applyFilter(text: string) {
    setKeyword(text);
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

  // ------------------------------------------------------------------
  // 播放控制（转给 Go 状态机，发声经 audioBridge）
  // ------------------------------------------------------------------

  async function onTrackClick(track: MusicTrack) {
    // 曲目已在播放时不再重启（避免自动切歌后被打回 0:00）
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
      console.error(t("播放失败"), ex);
    }
  }

  function togglePlayPause() {
    if (playbackStateRef.current === "Playing") PausePlayback();
    else if (playbackStateRef.current === "Paused") ResumePlayback();
    else if (displayTracksRef.current.length > 0) {
      // 停止态：播放列表首项
      PlayTrack(displayTracksRef.current[0]).catch(() => {
        /* ignore */
      });
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

  function onVolumeCommit(v: number) {
    const value = Math.round(Number(v) || 0);

    setVolume(value);
    SetMusicVolume(value).catch(() => {
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
    } else {
      // 自动切歌后跟随高亮：过滤列表里存在则滚动到可见
      const index = displayTracksRef.current.findIndex(
        (item) => item.FilePath === track.FilePath,
      );

      if (index >= 0) {
        document
          .querySelector(".track-tile.current")
          ?.scrollIntoView({ block: "nearest" });
      }
    }
  }
  function onModeChanged(mode: unknown) {
    setPlaybackMode(String(mode));
  }

  // 进度跟随音频（非拖拽时，250ms 轮询）
  useEffect(() => {
    const handle = setInterval(() => {
      if (!userDragging && progressEnabled) {
        setProgressValue(audioState.positionMs / 1000);
      }
    }, 250);

    return () => clearInterval(handle);
  }, [userDragging, progressEnabled]);

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

  // 初始化：加载已保存的音量与播放模式；已有文件夹则自动扫描
  useEffect(() => {
    (async () => {
      try {
        const savedVolume = await GetMusicVolume();

        if (Number.isFinite(savedVolume)) setVolume(savedVolume);
        setPlaybackMode(String(await GetMusicPlaybackMode()));
        setFolderPath(await GetMusicFolderPath());
        setSortMode(String(await GetMusicSortMode()));
        const savedFolder = await GetMusicFolderPath();

        if (savedFolder) await refreshLibrary();
        // 共享播放器已有曲目在播时同步界面
        const playlist = await GetPlaylist();

        if (playlist?.length && !currentTrackRef.current) {
          setDisplayTracks(
            asArray<MusicTrack>(
              await GetSortedMusicTracks(await GetMusicSortMode()),
            ),
          );
        }
      } catch (ex) {
        console.error(t("音乐页初始化失败"), ex);
      }
    })();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    /* 顶栏 / 搜索+列表 / 底部控制栏 三段 flex 布局（min-h-0 链保证列表可收缩） */
    <section className="flex h-full min-h-0 flex-col gap-4 overflow-hidden px-6 py-5">
      {/* 顶栏 */}
      <header className="flex items-center gap-3">
        <div className="mr-auto flex min-w-0 flex-1 items-center gap-3">
          <div className="flex size-11 flex-none items-center justify-center rounded-2xl bg-primary text-primary-foreground shadow-lg shadow-primary/30">
            <MusicNoteIcon />
          </div>
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
          popoverProps={{ motionProps: popoverMotionProps }}
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

      {/* 搜索 + 列表 */}
      <div className="flex min-h-0 flex-1 flex-col gap-3">
        <Input
          aria-label={t("搜索歌曲")}
          classNames={{
            inputWrapper: "bg-default-100/80 data-[hover=true]:bg-default-200",
          }}
          placeholder={t("搜索歌曲")}
          radius="full"
          size="sm"
          startContent={<SearchIcon className="text-gray-400" />}
          value={keyword}
          onValueChange={(v) => applyFilter(v)}
        />

        <div className="relative min-h-0 flex-1 overflow-hidden rounded-2xl border nya-border nya-panel shadow-sm backdrop-blur-md">
          <div className="nya-scroll h-full overflow-y-auto p-1.5">
            <AnimatePresence initial={false}>
              {displayTracks.map((track) => (
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
                    className={`track-tile group mx-0.5 my-[3px] flex cursor-pointer items-center gap-3 rounded-xl px-2.5 py-1.5 transition-all duration-150 ${
                      isCurrent(track)
                        ? "bg-primary/10 current"
                        : "hover:bg-default-100/80 hover:translate-x-0.5"
                    }`}
                    role="button"
                    tabIndex={0}
                    onClick={() => onTrackClick(track)}
                    onKeyDown={(event) => {
                      if (event.key === "Enter" || event.key === " ") {
                        event.preventDefault();
                        onTrackClick(track);
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
                        <MusicNoteIcon className="w-[18px] h-[18px]" />
                      ) : (
                        /* 均衡器动画（播放中） */
                        <span className="flex items-end justify-center gap-[3px] pb-[6px] h-[20px]">
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
                        {trackMeta(track)}
                      </span>
                    </div>

                    {isCurrent(track) ? (
                      <span className="flex-none text-primary opacity-0 group-hover:opacity-100 transition-opacity">
                        <VolumeHighIcon />
                      </span>
                    ) : null}
                  </div>
                </motion.div>
              ))}
            </AnimatePresence>
          </div>

          {/* 空态 */}
          {displayTracks.length === 0 ? (
            <div className="nya-enter absolute inset-0 flex flex-col items-center justify-center gap-3 text-center text-gray-400 backdrop-blur-[2px]">
              <div className="flex size-16 items-center justify-center rounded-3xl bg-gradient-to-br from-default-200 to-default-100 dark:from-gray-800 dark:to-gray-800/50 shadow-inner">
                <MusicNoteIcon className="w-8 h-8" />
              </div>
              <span className="text-[15px] font-semibold text-gray-500 dark:text-gray-400">
                {library.length > 0 ? t("没有匹配的歌曲") : t("暂无歌曲")}
              </span>
            </div>
          ) : null}
        </div>
      </div>

      {/* 底部播放控制栏 */}
      <footer className="flex flex-none flex-col gap-2 rounded-2xl border nya-border nya-panel px-5 py-3 shadow-sm backdrop-blur-md">
        {/* 进度条 + 时间 */}
        <div className="flex items-center gap-3">
          <span className="w-[40px] flex-none text-center font-mono text-[11px] text-gray-400 tabular-nums">
            {formatTime(positionSec)}
          </span>
          <div className="min-w-0 flex-1">
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
          <span className="w-[40px] flex-none text-right font-mono text-[11px] text-gray-400 tabular-nums">
            {durationSec > 0 ? formatTime(durationSec) : "--:--"}
          </span>
        </div>

        {/* 控制按钮 + 曲目信息 + 音量 */}
        <div className="flex items-center gap-4">
          <div className="flex flex-none items-center gap-2">
            <Tooltip
              content={t("上一首")}
              delay={300}
              motionProps={tooltipMotionProps}
            >
              <Button
                isIconOnly
                className="min-w-unit-10 h-unit-10 active:scale-90"
                radius="full"
                variant="flat"
                onPress={previous}
              >
                <SkipPreviousIcon />
              </Button>
            </Tooltip>
            <Tooltip
              content={isPlaying ? t("暂停") : t("播放")}
              delay={300}
              motionProps={tooltipMotionProps}
            >
              <Button
                isIconOnly
                className="min-w-unit-12 h-unit-12 active:scale-90"
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
              content={t("下一首")}
              delay={300}
              motionProps={tooltipMotionProps}
            >
              <Button
                isIconOnly
                className="min-w-unit-10 h-unit-10 active:scale-90"
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
                className="min-w-unit-10 h-unit-10 active:scale-90"
                radius="full"
                variant="flat"
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
                className={`relative min-w-unit-10 h-unit-10 active:scale-90 ${playbackMode !== "Sequential" ? "text-primary" : ""}`}
                radius="full"
                variant="light"
                onPress={cycleMode}
              >
                {modeIcon}
                {playbackMode !== "Sequential" ? (
                  <span className="absolute bottom-1 right-1 size-[7px] rounded-full bg-primary" />
                ) : null}
              </Button>
            </Tooltip>
          </div>

          {/* 当前曲目信息 + 唱片封面 */}
          <div className="flex min-w-0 flex-1 items-center gap-3">
            <div
              className={`relative flex size-[46px] flex-none items-center justify-center overflow-hidden rounded-full bg-primary text-primary-foreground shadow-md ${isPlaying ? "nya-cover-glow" : ""}`}
            >
              {/* 黑胶纹理环 */}
              <span
                className={`absolute inset-[3px] rounded-full border border-white/25 ${isPlaying ? "nya-vinyl" : ""}`}
              >
                <span className="absolute inset-[14%] rounded-full border border-white/15" />
                <span className="absolute inset-[28%] rounded-full border border-white/10" />
              </span>
              <span className="relative z-10 flex size-[16px] items-center justify-center rounded-full bg-white/90 text-primary">
                <MusicNoteIcon className="w-[10px] h-[10px]" />
              </span>
            </div>
            <div className="flex min-w-0 flex-col gap-0.5">
              {/* 换曲时旧信息上滑淡出、新信息滑入 */}
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
          </div>

          {/* 音量 */}
          <div className="flex flex-none items-center gap-2">
            <span className="text-gray-400">
              <VolumeIcon />
            </span>
            <div className="w-[96px] flex-none">
              <Slider
                aria-label={t("音量")}
                color="primary"
                maxValue={100}
                minValue={0}
                size="sm"
                step={1}
                value={volume}
                onChange={(v) => setVolume(Array.isArray(v) ? v[0] : v)}
                onChangeEnd={(v) => onVolumeCommit(Array.isArray(v) ? v[0] : v)}
              />
            </div>
            <span className="w-[36px] flex-none text-[11px] text-gray-400 tabular-nums">
              {volume}%
            </span>
          </div>
        </div>
      </footer>
    </section>
  );
};

export default MusicPage;
