/*
 * AI 助手逻辑层（由 layouts/ai.tsx 拆分）：工具真实执行：经 Wails bindings 操作实例文件与内容。
 */
import type {
  instance,
  bindings,
  content,
  download,
  launch,
  models,
} from "../../../wailsjs/go/models";

import {
  GetCurrentInstanceSnapshot,
  GetVersionDetails,
  SelectInstance,
} from "../../../wailsjs/go/bindings/InstanceAPI";
import {
  ReadTextFile,
  ListDirectory,
  OpenInExplorer,
} from "../../../wailsjs/go/bindings/SystemAPI";
import {
  GetLogText,
  DiagnoseCrash,
  GetLaunchSnapshot,
  Launch,
  StopGame,
} from "../../../wailsjs/go/bindings/LauncherAPI";
import {
  SearchResources,
  ListResourceVersions,
  DownloadResourceVersion,
} from "../../../wailsjs/go/bindings/DownloadAPI";
import {
  ToggleContentEntry,
  AnalyzeModConflicts,
  CheckInstanceContentUpdates,
} from "../../../wailsjs/go/bindings/ContentAPI";

import { truncateForModel, truncateMiddle } from "./tool-parse";
import { formatFileSizeStatic } from "./compose";
import { BINARY_FILE_EXTENSIONS } from "./agent-config";

/** 工具执行时用到的实例上下文（懒解析） */
export interface ToolContext {
  snapshot: instance.GameInstanceSnapshot | null;
  details: instance.GameVersionDetails | null;
}

export const NO_INSTANCE_MSG = "未找到当前实例，请先在启动器中选择一个实例";

/** 解析 AI 传入的路径：空 → 实例内容目录；相对路径 → 相对实例内容目录；绝对路径原样 */
export function resolveInstancePath(
  ctx: ToolContext,
  p: string,
): string | null {
  const raw = p.trim().replace(/[\\/]+$/, "");

  if (!raw) {
    return (
      ctx.details?.ContentDirectory ?? ctx.snapshot?.MinecraftDirectory ?? null
    );
  }
  const normalized = raw.replace(/\\/g, "/");

  if (/^[a-zA-Z]:\//.test(normalized) || normalized.startsWith("//")) {
    return normalized;
  }
  const base =
    ctx.details?.ContentDirectory ?? ctx.snapshot?.MinecraftDirectory;

  return base ? `${base.replace(/[\\/]+$/, "")}/${normalized}` : null;
}

export async function resolveToolContext(): Promise<ToolContext> {
  try {
    const snapshot = await GetCurrentInstanceSnapshot();

    if (!snapshot?.SelectedVersionId)
      return { snapshot: snapshot ?? null, details: null };
    const details = await GetVersionDetails(snapshot.SelectedVersionId);

    return { snapshot, details };
  } catch {
    return { snapshot: null, details: null };
  }
}

/** 修改类工具执行后刷新上下文：同一轮里后续的 list_mods / get_instance_info
 * 才能读到刚装上/刚切换的状态，而不是开轮时的旧快照。 */
async function refreshToolContext(ctx: ToolContext): Promise<void> {
  try {
    const snapshot = await GetCurrentInstanceSnapshot();

    ctx.snapshot = snapshot;
    ctx.details = snapshot?.SelectedVersionId
      ? await GetVersionDetails(snapshot.SelectedVersionId)
      : null;
  } catch {
    /* 刷新失败保留旧上下文，工具结果照常返回 */
  }
}

export function contentEntrySummary(
  entries: content.GameContentEntry[] | undefined,
) {
  return (entries ?? []).map((e) => ({
    name: e.Name,
    info: e.MetadataLine || undefined,
    disabled: e.IsDisabled || undefined,
  }));
}

/** 真实执行一个工具调用，全部异常就地转为 {ok:false} */
export async function executeTool(
  name: string,
  args: Record<string, unknown>,
  ctx: ToolContext,
): Promise<{ ok: boolean; result: string }> {
  try {
    switch (name) {
      case "get_instance_info": {
        if (!ctx.snapshot || !ctx.details)
          return { ok: false, result: NO_INSTANCE_MSG };
        const d = ctx.details;

        return {
          ok: true,
          result: truncateForModel(
            JSON.stringify(
              {
                selectedVersionId: d.VersionId,
                availableVersions: ctx.snapshot.VersionIds,
                versionType: d.VersionType,
                baseGameVersion: d.BaseGameVersion,
                loader:
                  [d.LoaderName, d.LoaderVersion].filter(Boolean).join(" ") ||
                  "无",
                isolated: d.IsIsolated,
                releaseTime: d.ReleaseTime,
                javaRequirement: d.JavaRequirement,
                gameDirectory: ctx.snapshot.GameDirectory || d.VersionDirectory,
                contentDirectory: d.ContentDirectory,
                counts: {
                  mods: d.Mods?.length ?? 0,
                  resourcePacks: d.ResourcePacks?.length ?? 0,
                  shaders: d.Shaders?.length ?? 0,
                  saves: d.Saves?.length ?? 0,
                },
              },
              null,
              2,
            ),
          ),
        };
      }
      case "list_mods":
      case "list_resourcepacks":
      case "list_shaders":
      case "list_saves": {
        if (!ctx.details) return { ok: false, result: NO_INSTANCE_MSG };
        const map = {
          list_mods: ctx.details.Mods,
          list_resourcepacks: ctx.details.ResourcePacks,
          list_shaders: ctx.details.Shaders,
          list_saves: ctx.details.Saves,
        } as const;
        const entries = contentEntrySummary(map[name as keyof typeof map]);

        return {
          ok: true,
          result: truncateForModel(
            entries.length
              ? JSON.stringify(entries, null, 2)
              : "当前实例没有相关内容",
          ),
        };
      }
      case "get_game_settings": {
        const dirs = [
          ctx.details?.ContentDirectory,
          ctx.snapshot?.GameDirectory,
          ctx.snapshot?.MinecraftDirectory,
        ].filter(Boolean) as string[];

        for (const dir of dirs) {
          try {
            const text = await ReadTextFile(
              `${dir.replace(/[\\/]+$/, "")}/options.txt`,
            );

            if (text) return { ok: true, result: truncateForModel(text) };
          } catch {
            /* 换下一个候选目录 */
          }
        }

        return {
          ok: false,
          result: "未找到 options.txt（实例可能尚未生成游戏设置）",
        };
      }
      case "list_directory": {
        const dir = resolveInstancePath(ctx, String(args.path ?? ""));

        if (!dir) return { ok: false, result: NO_INSTANCE_MSG };
        const entries: bindings.SystemFileEntry[] = await ListDirectory(dir);
        const shown = entries.slice(0, 200);
        const lines = shown.map(
          (e) =>
            `${e.isDir ? "[目录]" : "[文件]"} ${e.name}${e.isDir ? "" : ` (${formatFileSizeStatic(e.size)})`}${e.modifiedAt ? ` · ${e.modifiedAt}` : ""}`,
        );

        return {
          ok: true,
          result: truncateForModel(
            `${dir}\n共 ${entries.length} 项${
              entries.length > shown.length ? "（仅显示前 200 项）" : ""
            }：\n${lines.join("\n")}`,
          ),
        };
      }
      case "read_file": {
        const target = resolveInstancePath(ctx, String(args.path ?? ""));

        if (!target) return { ok: false, result: NO_INSTANCE_MSG };
        const dotIndex = target.lastIndexOf(".");
        const ext = dotIndex >= 0 ? target.slice(dotIndex).toLowerCase() : "";

        if (BINARY_FILE_EXTENSIONS.has(ext)) {
          return { ok: false, result: `不支持读取二进制文件：${target}` };
        }
        const text = await ReadTextFile(target);

        if (!text.trim()) return { ok: true, result: `${target}\n（空文件）` };

        return { ok: true, result: truncateMiddle(`${target}\n${text}`) };
      }
      case "get_launch_log": {
        const text: string = await GetLogText();

        if (!text.trim()) return { ok: true, result: "当前没有日志内容" };

        return { ok: true, result: truncateMiddle(text) };
      }
      case "diagnose_crash": {
        // 后端自动读实例游戏目录下最新的 crash-reports/*.txt 并判定常见原因
        const d: launch.CrashDiagnosis = await DiagnoseCrash();

        return {
          ok: true,
          result: truncateForModel(
            JSON.stringify(
              {
                reportPath: d.ReportPath,
                summary: d.Summary,
                description: d.Description,
                exception: d.Exception,
                suspected: d.Suspected,
                suggestions: d.Suggestions,
              },
              null,
              2,
            ),
          ),
        };
      }
      case "get_launch_config": {
        const snap: launch.GameLaunchSnapshot = await GetLaunchSnapshot();

        return {
          ok: true,
          result: truncateForModel(JSON.stringify(snap, null, 2)),
        };
      }
      case "search_mod": {
        const query = String(args.query ?? "").trim();

        if (!query) return { ok: false, result: "缺少 query 参数" };
        const request: models.ResourceSearchRequest = {
          source: String(args.source ?? "modrinth"),
          projectType: "mod",
          query,
          gameVersion: ctx.details?.BaseGameVersion ?? "",
          loader: (ctx.details?.LoaderName ?? "").toLowerCase(),
          loaders: [],
          limit: 8,
        };
        const result = await SearchResources(request);
        const hits = (result?.hits ?? []).slice(0, 8).map((h) => ({
          projectId: h.projectId,
          slug: h.slug,
          title: h.title,
          description: h.description?.slice(0, 120),
          downloads: h.downloadsDisplay || h.downloads,
        }));

        return {
          ok: true,
          result: truncateForModel(
            hits.length
              ? JSON.stringify(
                  {
                    hits,
                    note: "安装时请把 projectId 作为 project_id 传入 install_mod",
                  },
                  null,
                  2,
                )
              : "没有找到相关模组",
          ),
        };
      }
      case "open_folder": {
        if (!ctx.details) return { ok: false, result: NO_INSTANCE_MSG };
        const target = String(args.target ?? "content");
        const base = ctx.details.ContentDirectory.replace(/[\\/]+$/, "");
        const dirMap: Record<string, string> = {
          content: base,
          mods: `${base}/mods`,
          resourcepacks: `${base}/resourcepacks`,
          shaderpacks: `${base}/shaderpacks`,
          saves: `${base}/saves`,
        };
        const dir = dirMap[target];

        if (!dir) {
          return {
            ok: false,
            result: `未知目录：${target}，可选 content/mods/resourcepacks/shaderpacks/saves`,
          };
        }
        await OpenInExplorer(dir);

        return { ok: true, result: `已在资源管理器中打开：${dir}` };
      }
      case "install_mod": {
        if (!ctx.details) return { ok: false, result: NO_INSTANCE_MSG };
        const projectId = String(args.project_id ?? "").trim();

        if (!projectId) {
          return {
            ok: false,
            result: "缺少 project_id 参数，请先调用 search_mod 获取",
          };
        }
        const source = String(args.source ?? "modrinth");
        let versionId = String(args.version_id ?? "").trim();

        if (!versionId) {
          const listReq: models.ResourceVersionRequest = {
            source,
            projectId,
            gameVersion: ctx.details.BaseGameVersion ?? "",
            loader: (ctx.details.LoaderName ?? "").toLowerCase(),
          };
          const list = await ListResourceVersions(listReq);
          const versions = list?.versions ?? [];
          const matched =
            versions.find((v) => v.matchesInstance) ?? versions[0];

          if (!matched?.versionId) {
            return { ok: false, result: "该模组没有可用于当前实例的版本" };
          }
          versionId = matched.versionId;
        }
        const downloadReq: models.ResourceDownloadRequest = {
          source,
          projectId,
          versionId,
          contentDirectory: ctx.details.ContentDirectory,
          subDirectory: "mods",
        };
        const downloadResult = await DownloadResourceVersion(downloadReq);

        await refreshToolContext(ctx);

        return {
          ok: true,
          result: truncateForModel(
            JSON.stringify(
              {
                ok: true,
                fileName: downloadResult?.fileName,
                savedPath: downloadResult?.savedPath,
                fileSize: downloadResult?.fileSize,
              },
              null,
              2,
            ),
          ),
        };
      }
      case "toggle_mod": {
        if (!ctx.details) return { ok: false, result: NO_INSTANCE_MSG };
        const nameArg = String(args.name ?? "").trim();

        if (!nameArg) return { ok: false, result: "缺少 name 参数" };
        const lower = nameArg.toLowerCase();
        const entry = (ctx.details.Mods ?? []).find(
          (m) =>
            m.Name?.toLowerCase() === lower ||
            m.SourcePath?.toLowerCase().endsWith(lower) ||
            m.Name?.toLowerCase().includes(lower),
        );

        if (!entry) return { ok: false, result: `未找到模组：${nameArg}` };
        const disable =
          args.disable === undefined ? true : Boolean(args.disable);

        if (entry.IsDisabled === disable) {
          return {
            ok: true,
            result: `模组 ${entry.Name} 已处于${disable ? "禁用" : "启用"}状态，无需操作`,
          };
        }
        await ToggleContentEntry(entry.SourcePath, disable);
        await refreshToolContext(ctx);

        return {
          ok: true,
          result: `已${disable ? "禁用" : "启用"}模组：${entry.Name}`,
        };
      }
      case "launch_instance": {
        const result: launch.LaunchResult = await Launch("", null, "");

        return {
          ok: result?.Success ?? false,
          result:
            result?.Message ||
            (result?.Success ? "启动指令已发出" : "启动失败"),
        };
      }
      case "stop_game": {
        const result: launch.LaunchResult = await StopGame();

        return {
          ok: result?.Success ?? false,
          result:
            result?.Message || (result?.Success ? "已停止游戏" : "停止失败"),
        };
      }
      case "analyze_mod_conflicts": {
        if (!ctx.details) return { ok: false, result: NO_INSTANCE_MSG };
        // 后端按实例版本 + 加载器做已知冲突库比对（api_content.go 同一套逻辑）
        const report: content.ModConflictReport = await AnalyzeModConflicts();
        const conflicts = (report?.Conflicts ?? []).map((c) => ({
          kind: c.Kind,
          severity: c.Severity,
          subject: c.DisplayName || c.Subject,
          files: c.Files,
          detail: c.Detail,
          related: c.Related,
        }));

        return {
          ok: true,
          result: truncateForModel(
            JSON.stringify(
              {
                analyzedMods: report?.AnalyzedMods ?? 0,
                unreadableMods: report?.UnreadableMods ?? 0,
                conflictCount: conflicts.length,
                conflicts,
                ...(conflicts.length === 0
                  ? { note: "没有发现已知冲突或重复安装" }
                  : {}),
              },
              null,
              2,
            ),
          ),
        };
      }
      case "check_content_updates": {
        if (
          !ctx.snapshot?.SelectedVersionId ||
          !ctx.snapshot?.MinecraftDirectory
        )
          return { ok: false, result: NO_INSTANCE_MSG };
        const result: download.ContentUpdateCheckResult =
          await CheckInstanceContentUpdates(
            ctx.snapshot.SourcePath,
            ctx.snapshot.MinecraftDirectory,
            ctx.snapshot.SelectedVersionId,
          );
        const updatable = (result?.Files ?? [])
          .filter((f) => f.Status === "updatable")
          .map((f) => ({
            file: f.FileName,
            name: f.ProjectName || undefined,
            currentVersion: f.CurrentVersion || undefined,
            latestVersion: f.LatestVersion || undefined,
            statusText: f.StatusText,
          }));

        return {
          ok: true,
          result: truncateForModel(
            JSON.stringify(
              {
                checkedFiles: result?.CheckedFileCount ?? 0,
                updatableCount: result?.UpdatableCount ?? 0,
                latestCount: result?.LatestCount ?? 0,
                unknownCount: result?.UnknownCount ?? 0,
                failedCount: result?.FailedCount ?? 0,
                duplicateFileCount: result?.DuplicateFileCount ?? 0,
                updatable,
              },
              null,
              2,
            ),
          ),
        };
      }
      case "switch_instance": {
        const versionId = String(args.version_id ?? "").trim();

        if (!versionId) return { ok: false, result: "缺少 version_id 参数" };
        const known = ctx.snapshot?.VersionIds ?? [];
        const match =
          known.find((v) => v === versionId) ??
          known.find((v) => v.toLowerCase() === versionId.toLowerCase());

        if (!match) {
          return {
            ok: false,
            result: `未找到实例：${versionId}。可用实例：${known.join("、") || "（无）"}`,
          };
        }
        const ok = await SelectInstance(match);

        if (!ok) return { ok: false, result: `切换实例失败：${match}` };
        await refreshToolContext(ctx);

        return {
          ok: true,
          result: `已切换到实例：${match}（${ctx.details?.LoaderName ?? "?"} ${ctx.details?.BaseGameVersion ?? "?"}）`,
        };
      }
      case "search_resource": {
        const query = String(args.query ?? "").trim();

        if (!query) return { ok: false, result: "缺少 query 参数" };
        const projectType = String(args.project_type ?? "shader");
        const request: models.ResourceSearchRequest = {
          source: String(args.source ?? "modrinth"),
          projectType,
          query,
          // 材质包/光影按游戏版本过滤即可，不挑加载器
          gameVersion: ctx.details?.BaseGameVersion ?? "",
          loader: "",
          loaders: [],
          limit: 8,
        };
        const result = await SearchResources(request);
        const hits = (result?.hits ?? []).slice(0, 8).map((h) => ({
          projectId: h.projectId,
          slug: h.slug,
          title: h.title,
          description: h.description?.slice(0, 120),
          downloads: h.downloadsDisplay || h.downloads,
        }));

        return {
          ok: true,
          result: truncateForModel(
            hits.length
              ? JSON.stringify(
                  {
                    hits,
                    note: "安装时请把 projectId 与 project_type 传入 install_resource",
                  },
                  null,
                  2,
                )
              : "没有找到相关资源",
          ),
        };
      }
      case "install_resource": {
        if (!ctx.details) return { ok: false, result: NO_INSTANCE_MSG };
        const projectId = String(args.project_id ?? "").trim();

        if (!projectId) {
          return {
            ok: false,
            result: "缺少 project_id 参数，请先调用 search_resource 获取",
          };
        }
        const projectType = String(args.project_type ?? "shader");

        if (projectType !== "shader" && projectType !== "resourcepack") {
          return {
            ok: false,
            result: `不支持安装该类型：${projectType}（仅 shader / resourcepack）`,
          };
        }
        const source = String(args.source ?? "modrinth");
        let versionId = String(args.version_id ?? "").trim();

        if (!versionId) {
          const listReq: models.ResourceVersionRequest = {
            source,
            projectId,
            gameVersion: ctx.details.BaseGameVersion ?? "",
            loader: "",
          };
          const list = await ListResourceVersions(listReq);
          const versions = list?.versions ?? [];
          const matched =
            versions.find((v) => v.matchesInstance) ?? versions[0];

          if (!matched?.versionId) {
            return {
              ok: false,
              result: "该资源没有可用于当前实例的版本",
            };
          }
          versionId = matched.versionId;
        }
        const downloadReq: models.ResourceDownloadRequest = {
          source,
          projectId,
          versionId,
          contentDirectory: ctx.details.ContentDirectory,
          subDirectory:
            projectType === "shader" ? "shaderpacks" : "resourcepacks",
        };
        const downloadResult = await DownloadResourceVersion(downloadReq);

        await refreshToolContext(ctx);

        return {
          ok: true,
          result: truncateForModel(
            JSON.stringify(
              {
                ok: true,
                fileName: downloadResult?.fileName,
                savedPath: downloadResult?.savedPath,
                fileSize: downloadResult?.fileSize,
              },
              null,
              2,
            ),
          ),
        };
      }
      default:
        return { ok: false, result: `未知工具：${name}，请查看可用工具列表` };
    }
  } catch (ex) {
    return { ok: false, result: (ex as Error)?.message || String(ex) };
  }
}
