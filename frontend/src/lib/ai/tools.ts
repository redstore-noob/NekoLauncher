/*
 * AI 助手逻辑层（由 layouts/ai.tsx 拆分）：工具 schema 单一来源，生成 OpenAI / Anthropic 两种 tools 格式。
 */

// ------------------------------------------------------------------
// 工具 schema（单一来源，同时生成 OpenAI tools 与 Anthropic tools 格式）
// ------------------------------------------------------------------

export interface ToolParamSpec {
  name: string;
  type: "string" | "boolean" | "number";
  description: string;
  required?: boolean;
  enumValues?: string[];
}

export interface ToolSpec {
  name: string;
  description: string;
  params: ToolParamSpec[];
}

export const TOOL_SPECS: ToolSpec[] = [
  {
    name: "get_instance_info",
    description:
      "获取当前实例详细信息：版本、加载器、隔离状态、目录、各类内容数量",
    params: [],
  },
  {
    name: "list_mods",
    description: "列出实例已安装模组（含禁用状态）",
    params: [],
  },
  { name: "list_resourcepacks", description: "列出资源包", params: [] },
  { name: "list_shaders", description: "列出光影包", params: [] },
  { name: "list_saves", description: "列出存档", params: [] },
  {
    name: "get_game_settings",
    description: "读取 options.txt 游戏设置原文",
    params: [],
  },
  {
    name: "list_directory",
    description: "浏览实例目录，目录在前，含大小与修改时间",
    params: [
      {
        name: "path",
        type: "string",
        description: "相对实例内容目录的路径或绝对路径，不填为实例内容目录",
      },
    ],
  },
  {
    name: "read_file",
    description:
      "读取实例内的文本文件（日志/配置/json/txt 等），二进制文件会被拒绝",
    params: [
      {
        name: "path",
        type: "string",
        description: "文件路径，相对实例内容目录或绝对路径",
        required: true,
      },
    ],
  },
  {
    name: "get_launch_config",
    description: "获取启动器当前启动状态快照（版本、账号、阶段）",
    params: [],
  },
  {
    name: "get_launch_log",
    description: "读取启动器与游戏输出日志全文（过长时中段截断）",
    params: [],
  },
  {
    name: "diagnose_crash",
    description: "自动诊断最近一次崩溃报告，返回疑似原因与处置建议",
    params: [],
  },
  {
    name: "search_mod",
    description:
      "搜索模组；安装模组时必须使用它返回的 projectId 作为 project_id",
    params: [
      { name: "query", type: "string", description: "搜索词", required: true },
      {
        name: "source",
        type: "string",
        description: "搜索源，默认 modrinth",
        enumValues: ["modrinth", "curseforge"],
      },
    ],
  },
  {
    name: "open_folder",
    description: "在资源管理器中打开实例相关目录",
    params: [
      {
        name: "target",
        type: "string",
        description: "要打开的目录，默认 content",
        enumValues: [
          "content",
          "mods",
          "resourcepacks",
          "shaderpacks",
          "saves",
        ],
      },
    ],
  },
  {
    name: "install_mod",
    description: "安装模组到实例 mods 目录",
    params: [
      {
        name: "project_id",
        type: "string",
        description: "search_mod 返回的 projectId，不要编造",
        required: true,
      },
      {
        name: "version_id",
        type: "string",
        description: "指定版本 ID，不填自动选最新匹配版",
      },
      {
        name: "source",
        type: "string",
        description: "来源，默认 modrinth",
        enumValues: ["modrinth", "curseforge"],
      },
    ],
  },
  {
    name: "toggle_mod",
    description: "启用/禁用模组",
    params: [
      {
        name: "name",
        type: "string",
        description: "模组文件名或名称",
        required: true,
      },
      {
        name: "disable",
        type: "boolean",
        description: "true 禁用 / false 启用，默认 true",
      },
    ],
  },
  {
    name: "launch_instance",
    description: "启动当前选中的实例与账号",
    params: [],
  },
  { name: "stop_game", description: "停止正在运行的游戏进程", params: [] },
  {
    name: "analyze_mod_conflicts",
    description:
      "分析当前实例模组间的已知冲突与重复（依赖冲突、重复功能、版本不匹配），返回严重度与处置建议",
    params: [],
  },
  {
    name: "check_content_updates",
    description:
      "联网检查实例内模组/资源包/光影的更新（Modrinth/CurseForge），返回可更新清单与版本号",
    params: [],
  },
  {
    name: "switch_instance",
    description:
      "切换启动器当前选中的实例（get_instance_info 的 availableVersions 里有全部实例 ID）",
    params: [
      {
        name: "version_id",
        type: "string",
        description: "目标实例的版本 ID",
        required: true,
      },
    ],
  },
  {
    name: "search_resource",
    description: "搜索光影包 / 材质包 / 整合包；安装时必须使用返回的 projectId",
    params: [
      { name: "query", type: "string", description: "搜索词", required: true },
      {
        name: "project_type",
        type: "string",
        description: "资源类型，默认 shader（光影包）",
        enumValues: ["shader", "resourcepack", "modpack"],
      },
      {
        name: "source",
        type: "string",
        description: "搜索源，默认 modrinth",
        enumValues: ["modrinth", "curseforge"],
      },
    ],
  },
  {
    name: "install_resource",
    description:
      "安装光影包或材质包到实例对应目录（shaderpacks / resourcepacks）",
    params: [
      {
        name: "project_id",
        type: "string",
        description: "search_resource 返回的 projectId，不要编造",
        required: true,
      },
      {
        name: "project_type",
        type: "string",
        description: "资源类型：shader（光影包）或 resourcepack（材质包）",
        enumValues: ["shader", "resourcepack"],
      },
      {
        name: "version_id",
        type: "string",
        description: "指定版本 ID，不填自动选最新匹配版",
      },
      {
        name: "source",
        type: "string",
        description: "来源，默认 modrinth",
        enumValues: ["modrinth", "curseforge"],
      },
    ],
  },
];

export function jsonSchemaOf(spec: ToolSpec) {
  return {
    type: "object" as const,
    properties: Object.fromEntries(
      spec.params.map((p) => [
        p.name,
        {
          type: p.type,
          description: p.description,
          ...(p.enumValues ? { enum: p.enumValues } : {}),
        },
      ]),
    ),
    required: spec.params.filter((p) => p.required).map((p) => p.name),
  };
}

/** OpenAI function calling 格式 */
export const OPENAI_TOOLS = TOOL_SPECS.map((s) => ({
  type: "function" as const,
  function: {
    name: s.name,
    description: s.description,
    parameters: jsonSchemaOf(s),
  },
}));

/** Anthropic tool use 格式 */
export const ANTHROPIC_TOOLS = TOOL_SPECS.map((s) => ({
  name: s.name,
  description: s.description,
  input_schema: jsonSchemaOf(s),
}));
