/*
 * Copyright 2026 烟花
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
/*
 * 资源包文本文件编辑器：模型 / blockstate / lang / pack.mcmeta 等直接编辑。
 * JSON / mcmeta 实时校验、一键格式化，并带语法高亮与字段补全（见 CodeEditor）。
 */
import type { PackFile } from "./types";

import React, { useMemo } from "react";
import { Button } from "@heroui/react";
import {
  CheckmarkCircle20Regular,
  Code20Regular,
  Warning20Regular,
} from "@fluentui/react-icons";

import { t } from "../../../i18n";

import CodeEditor from "./CodeEditor";
import { isJsonPath } from "./types";

interface TextFileEditorProps {
  file: PackFile;
  onChange: (text: string) => void;
}

function validateJson(text: string): string {
  if (!text.trim()) return "";
  try {
    JSON.parse(text);

    return "";
  } catch (error) {
    return error instanceof Error ? error.message : t("JSON 解析失败");
  }
}

const TextFileEditor: React.FC<TextFileEditorProps> = ({ file, onChange }) => {
  const text = file.text ?? "";
  const json = isJsonPath(file.path);
  const jsonError = useMemo(
    () => (json ? validateJson(text) : ""),
    [json, text],
  );
  const lineCount = useMemo(() => text.split("\n").length, [text]);

  const format = () => {
    try {
      onChange(`${JSON.stringify(JSON.parse(text), null, 2)}\n`);
    } catch {
      /* 非法 JSON：按钮已禁用，这里只是兜底 */
    }
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex flex-wrap items-center gap-2 px-3 pt-3">
        <span className="min-w-0 flex-1 truncate text-[12px] font-medium text-gray-600 dark:text-gray-300">
          {file.path}
        </span>
        {json ? (
          jsonError ? (
            <span className="flex items-center gap-1 text-[11px] text-danger">
              <Warning20Regular />

              {t("JSON 语法错误")}
            </span>
          ) : (
            <span className="flex items-center gap-1 text-[11px] text-success">
              <CheckmarkCircle20Regular />

              {t("JSON 合法")}
            </span>
          )
        ) : null}
        <Button
          isDisabled={!json || !!jsonError}
          size="sm"
          startContent={<Code20Regular />}
          variant="flat"
          onPress={format}
        >
          {t("格式化")}
        </Button>
      </div>

      {jsonError ? (
        <div className="mx-3 mt-2 rounded-lg bg-danger/10 px-2 py-1 text-[11px] text-danger">
          {jsonError}
        </div>
      ) : null}

      <div className="flex min-h-0 flex-1 flex-col p-3">
        <CodeEditor
          language={json ? "json" : "plain"}
          value={text}
          onChange={onChange}
        />
        <div className="pt-1.5 text-right text-[10px] text-gray-400">
          {lineCount} {t("行 ·")} {text.length} {t("字符")}
        </div>
      </div>
    </div>
  );
};

export default TextFileEditor;
