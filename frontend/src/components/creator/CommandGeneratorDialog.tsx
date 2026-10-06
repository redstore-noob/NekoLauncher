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
 * 指令生成器（创作中心的二级界面）：图形化点选命令类型与参数，
 * 实时拼出可直接粘贴进聊天栏的 Minecraft 指令（/give、/effect、/tp…）。
 * 只做"点一点"的常用命令覆盖，复杂场景留给命令方块。
 */
import React, { useMemo, useState } from "react";
import {
  Autocomplete,
  AutocompleteItem,
  Button,
  Input,
  Modal,
  ModalContent,
  Select,
  SelectItem,
  Slider,
  Switch,
} from "@heroui/react";
import {
  Checkmark20Regular,
  Copy20Regular,
  Options20Regular,
} from "@fluentui/react-icons";

import { selectPopoverProps } from "../../lib/motion";
import { ModalShell, modalBehaviorProps } from "../modal-shell";
import { notify } from "../overlay/dialog";
import { t } from "../../i18n";

import CreatorToolShell from "./CreatorToolShell";

interface CommandGeneratorDialogProps {
  isOpen: boolean;
  onClose: () => void;
  embedded?: boolean;
}

/** 命令类型：每个类型定义自己的参数表单与拼接逻辑 */
type CommandKind =
  | "give"
  | "effect"
  | "enchant"
  | "tp"
  | "gamemode"
  | "time"
  | "weather"
  | "difficulty"
  | "xp"
  | "summon";

const KIND_OPTIONS: Array<{ key: CommandKind; label: string }> = [
  { key: "give", label: t("给物品 /give") },
  { key: "effect", label: t("给效果 /effect") },
  { key: "enchant", label: t("附魔 /enchant") },
  { key: "tp", label: t("传送 /tp") },
  { key: "gamemode", label: t("游戏模式 /gamemode") },
  { key: "time", label: t("时间 /time") },
  { key: "weather", label: t("天气 /weather") },
  { key: "difficulty", label: t("难度 /difficulty") },
  { key: "xp", label: t("经验 /xp") },
  { key: "summon", label: t("召唤 /summon") },
];

/** 目标选择器常用项 */
const TARGETS = ["@s", "@p", "@a", "@e", "@r"];

/** 常用物品（中文名 + 英文 id，搜索框里两个都能搜；列表外的 id 可直接手输） */
const ITEMS = [
  ["diamond_sword", t("钻石剑")],
  ["diamond_pickaxe", t("钻石镐")],
  ["diamond_axe", t("钻石斧")],
  ["diamond_shovel", t("钻石锹")],
  ["diamond_hoe", t("钻石锄")],
  ["diamond_helmet", t("钻石头盔")],
  ["diamond_chestplate", t("钻石胸甲")],
  ["diamond_leggings", t("钻石护腿")],
  ["diamond_boots", t("钻石靴子")],
  ["netherite_sword", t("下界合金剑")],
  ["netherite_pickaxe", t("下界合金镐")],
  ["netherite_axe", t("下界合金斧")],
  ["netherite_helmet", t("下界合金头盔")],
  ["netherite_chestplate", t("下界合金胸甲")],
  ["netherite_leggings", t("下界合金护腿")],
  ["netherite_boots", t("下界合金靴子")],
  ["iron_sword", t("铁剑")],
  ["iron_pickaxe", t("铁镐")],
  ["iron_helmet", t("铁头盔")],
  ["golden_sword", t("金剑")],
  ["stone_sword", t("石剑")],
  ["wooden_sword", t("木剑")],
  ["bow", t("弓")],
  ["crossbow", t("弩")],
  ["arrow", t("箭")],
  ["trident", t("三叉戟")],
  ["mace", t("重锤")],
  ["shield", t("盾牌")],
  ["shears", t("剪刀")],
  ["flint_and_steel", t("打火石")],
  ["fishing_rod", t("钓鱼竿")],
  ["elytra", t("鞘翅")],
  ["totem_of_undying", t("不死图腾")],
  ["golden_apple", t("金苹果")],
  ["enchanted_golden_apple", t("附魔金苹果")],
  ["apple", t("苹果")],
  ["bread", t("面包")],
  ["cooked_beef", t("牛排")],
  ["cake", t("蛋糕")],
  ["diamond", t("钻石")],
  ["emerald", t("绿宝石")],
  ["iron_ingot", t("铁锭")],
  ["gold_ingot", t("金锭")],
  ["netherite_ingot", t("下界合金锭")],
  ["netherite_scrap", t("下界合金碎片")],
  ["coal", t("煤炭")],
  ["redstone", t("红石粉")],
  ["lapis_lazuli", t("青金石")],
  ["quartz", t("下界石英")],
  ["amethyst_shard", t("紫水晶碎片")],
  ["stick", t("木棍")],
  ["string", t("线")],
  ["gunpowder", t("火药")],
  ["blaze_rod", t("烈焰棒")],
  ["ender_pearl", t("末影珍珠")],
  ["ender_eye", t("末影之眼")],
  ["experience_bottle", t("附魔之瓶")],
  ["book", t("书")],
  ["enchanted_book", t("附魔书")],
  ["name_tag", t("命名牌")],
  ["saddle", t("鞍")],
  ["lead", t("拴绳")],
  ["compass", t("指南针")],
  ["clock", t("钟")],
  ["map", t("地图")],
  ["filled_map", t("已填充地图")],
  ["spyglass", t("望远镜")],
  ["bucket", t("桶")],
  ["water_bucket", t("水桶")],
  ["lava_bucket", t("熔岩桶")],
  ["milk_bucket", t("牛奶桶")],
  ["potion", t("药水")],
  ["splash_potion", t("喷溅药水")],
  ["lingering_potion", t("滞留药水")],
  ["minecart", t("矿车")],
  ["oak_boat", t("橡木船")],
  ["rail", t("铁轨")],
  ["powered_rail", t("充能铁轨")],
  ["tnt", t("炸药")],
  ["torch", t("火把")],
  ["lantern", t("灯笼")],
  ["crafting_table", t("工作台")],
  ["furnace", t("熔炉")],
  ["enchanting_table", t("附魔台")],
  ["anvil", t("铁砧")],
  ["beacon", t("信标")],
  ["shulker_box", t("潜影盒")],
  ["ender_chest", t("末影箱")],
  ["respawn_anchor", t("重生锚")],
  ["lodestone", t("磁石")],
  ["conduit", t("潮涌核心")],
  ["command_block", t("命令方块")],
  ["chain_command_block", t("连锁命令方块")],
  ["repeating_command_block", t("循环命令方块")],
  ["structure_block", t("结构方块")],
  ["jigsaw", t("拼图方块")],
  ["barrier", t("屏障")],
  ["light", t("光源方块")],
  ["debug_stick", t("调试棒")],
  ["written_book", t("成书")],
  ["writable_book", t("书与笔")],
] as const;

const EFFECTS = [
  ["speed", t("速度")],
  ["slowness", t("缓慢")],
  ["haste", t("急迫")],
  ["mining_fatigue", t("挖掘疲劳")],
  ["strength", t("力量")],
  ["instant_health", t("瞬间治疗")],
  ["instant_damage", t("瞬间伤害")],
  ["jump_boost", t("跳跃提升")],
  ["regeneration", t("生命恢复")],
  ["resistance", t("抗性提升")],
  ["fire_resistance", t("抗火")],
  ["water_breathing", t("水下呼吸")],
  ["invisibility", t("隐身")],
  ["night_vision", t("夜视")],
  ["levitation", t("漂浮")],
  ["glowing", t("发光")],
] as const;

const ENCHANTS = [
  ["protection", t("保护")],
  ["sharpness", t("锋利")],
  ["efficiency", t("效率")],
  ["unbreaking", t("耐久")],
  ["fortune", t("时运")],
  ["silk_touch", t("精准采集")],
  ["looting", t("抢夺")],
  ["mending", t("经验修补")],
] as const;

const GAMEMODES = [
  ["survival", t("生存")],
  ["creative", t("创造")],
  ["adventure", t("冒险")],
  ["spectator", t("旁观")],
] as const;

const TIMES = [
  ["day", t("白天")],
  ["noon", t("正午")],
  ["night", t("黑夜")],
  ["midnight", t("午夜")],
] as const;

const WEATHERS = [
  ["clear", t("晴天")],
  ["rain", t("下雨")],
  ["thunder", t("雷暴")],
] as const;

const DIFFICULTIES = [
  ["peaceful", t("和平")],
  ["easy", t("简单")],
  ["normal", t("普通")],
  ["hard", t("困难")],
] as const;

const ENTITIES = [
  ["creeper", t("苦力怕")],
  ["zombie", t("僵尸")],
  ["skeleton", t("骷髅")],
  ["enderman", t("末影人")],
  ["cow", t("牛")],
  ["pig", t("猪")],
  ["sheep", t("羊")],
  ["villager", t("村民")],
  ["lightning_bolt", t("闪电")],
  ["tnt", t("点燃的 TNT")],
] as const;

async function copyText(value: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(value);

    return true;
  } catch {
    return false;
  }
}

const firstKey = (
  options: ReadonlyArray<readonly [string, string] | string>,
): string => (typeof options[0] === "string" ? options[0] : options[0][0]);

const OptionSelect: React.FC<{
  ariaLabel: string;
  options: ReadonlyArray<readonly [string, string] | string>;
  value: string;
  onChange: (value: string) => void;
  className?: string;
}> = ({ ariaLabel, options, value, onChange, className }) => {
  // 缓存 selectedKeys 数组，避免每次渲染都创建新数组导致 Select 闪烁
  const selectedKeys = useMemo(() => [value], [value]);

  return (
    <Select
      aria-label={ariaLabel}
      className={className ?? "w-full"}
      classNames={{ trigger: "h-9 min-h-9" }}
      popoverProps={selectPopoverProps}
      selectedKeys={selectedKeys}
      size="sm"
      onSelectionChange={(keys) =>
        onChange((Array.from(keys)[0] as string) ?? firstKey(options))
      }
    >
      {options.map((option) => {
        const [key, label] =
          typeof option === "string" ? [option, t(option)] : option;

        return <SelectItem key={key}>{label}</SelectItem>;
      })}
    </Select>
  );
};

/**
 * 可搜索选择框：输入名字（中文或英文 id）即可筛选选中；
 * 开启 allowsCustomValue 后列表外的自定义 id（如 mod 物品）也可直接输入使用。
 */
const SearchSelect: React.FC<{
  ariaLabel: string;
  options: ReadonlyArray<readonly [string, string]>;
  value: string;
  onChange: (value: string) => void;
}> = ({ ariaLabel, options, value, onChange }) => {
  const labelOf = (key: string) =>
    options.find(([id]) => id === key)?.[1] ?? key;
  const [inputValue, setInputValue] = useState(() => labelOf(value));

  return (
    <Autocomplete
      allowsCustomValue
      aria-label={ariaLabel}
      className="w-full"
      classNames={{ selectorButton: "h-9 min-h-9" }}
      inputValue={inputValue}
      menuTrigger="input"
      popoverProps={selectPopoverProps}
      selectedKey={value}
      size="sm"
      onInputChange={(next) => {
        // 输入未匹配任何条目时，视为手输的自定义 id（去掉 minecraft: 前缀统一存储）；
        // 只接受合法 id 字符（字母/数字/_ : . / -），随手输入的中文等不会污染指令
        const raw = next.trim().replace(/^minecraft:/, "");
        const known = options.some(
          ([id, label]) => id === raw || label === next.trim(),
        );
        const looksLikeId = /^[a-zA-Z0-9_./:-]*$/.test(raw);

        if (!known && raw !== "" && looksLikeId) onChange(raw);
        setInputValue(next);
      }}
      onSelectionChange={(key) => {
        if (key === null) return;
        const keyText = String(key).replace(/^minecraft:/, "");

        onChange(keyText);
        setInputValue(labelOf(keyText));
      }}
    >
      {options.map(([id, label]) => (
        <AutocompleteItem key={id} textValue={`${label} ${id}`}>
          {label}
        </AutocompleteItem>
      ))}
    </Autocomplete>
  );
};

const CommandGeneratorDialog: React.FC<CommandGeneratorDialogProps> = ({
  isOpen,
  onClose,
  embedded,
}) => {
  const [kind, setKind] = useState<CommandKind>("give");
  const [copied, setCopied] = useState(false);

  // give / enchant
  const [target, setTarget] = useState("@s");
  const [item, setItem] = useState("diamond_sword");
  const [count, setCount] = useState(1);
  const [enchant, setEnchant] = useState("sharpness");
  const [enchantLevel, setEnchantLevel] = useState(5);

  const [effect, setEffect] = useState("speed");
  const [seconds, setSeconds] = useState(60);
  const [amplifier, setAmplifier] = useState(1);
  const [hideParticles, setHideParticles] = useState(false);

  // tp / summon
  const [x, setX] = useState("0");
  const [y, setY] = useState("100");
  const [z, setZ] = useState("0");
  const [entity, setEntity] = useState("creeper");

  // gamemode / time / weather / difficulty / xp
  const [gamemode, setGamemode] = useState("creative");
  const [time, setTime] = useState("day");
  const [timeValue, setTimeValue] = useState("6000");
  const [weather, setWeather] = useState("clear");
  const [difficulty, setDifficulty] = useState("normal");
  const [xpAmount, setXpAmount] = useState(10);
  const [xpLevels, setXpLevels] = useState(false);

  const command = useMemo(() => {
    const coords = `${x || "0"} ${y || "0"} ${z || "0"}`;

    switch (kind) {
      case "give":
        return `/give ${target} minecraft:${item}${count > 1 ? ` ${count}` : ""}`;
      case "effect":
        return `/effect give ${target} minecraft:${effect} ${seconds} ${Math.max(0, amplifier - 1)}${hideParticles ? " true" : ""}`;
      case "enchant":
        return `/enchant ${target} minecraft:${enchant} ${enchantLevel}`;
      case "tp":
        return `/tp ${target} ${coords}`;
      case "gamemode": {
        const mode =
          GAMEMODES.find(([key]) => key === gamemode)?.[0] ?? "survival";

        return `/gamemode ${mode}${target === "@s" ? "" : ` ${target}`}`;
      }
      case "time": {
        const value = time === "custom" ? timeValue : (time ?? "day");

        return `/time set ${value}`;
      }
      case "weather":
        return `/weather ${weather}`;
      case "difficulty":
        return `/difficulty ${difficulty}`;
      case "xp":
        return `/xp add ${target} ${xpAmount} ${xpLevels ? "levels" : "points"}`;
      case "summon":
        return `/summon minecraft:${entity} ${coords}`;
      default:
        return "";
    }
  }, [
    kind,
    target,
    item,
    count,
    enchant,
    enchantLevel,
    effect,
    seconds,
    amplifier,
    hideParticles,
    x,
    y,
    z,
    entity,
    gamemode,
    time,
    timeValue,
    weather,
    difficulty,
    xpAmount,
    xpLevels,
  ]);

  const copyCommand = async () => {
    if (!command) return;
    const ok = await copyText(command);

    if (ok) {
      setCopied(true);
      notify.success(t("已复制到剪贴板"));
      window.setTimeout(() => setCopied(false), 1500);
    } else {
      notify.error(t("复制失败"));
    }
  };

  /** 表单里反复用到的行容器 */
  const field = (label: string, control: React.ReactNode) => (
    <label className="flex items-center justify-between gap-3">
      <span className="flex-none text-xs text-gray-500 dark:text-gray-400">
        {label}
      </span>
      <div className="w-56">{control}</div>
    </label>
  );

  const renderBody = () => (
    <div className="flex h-full min-h-0 gap-4">
      {/* 左：命令类型磁贴 */}
      <div className="nya-panel-inner nya-border grid w-56 flex-none grid-cols-1 content-start gap-1.5 overflow-y-auto rounded-medium border p-2">
        {KIND_OPTIONS.map((option) => (
          <button
            key={option.key}
            className={`cursor-pointer rounded-lg border px-3 py-2 text-left transition-colors ${
              kind === option.key
                ? "border-primary/50 bg-primary/10"
                : "border-transparent hover:bg-default-100 dark:hover:bg-gray-800"
            }`}
            type="button"
            onClick={() => setKind(option.key)}
          >
            <span
              className={`block text-[13px] font-medium ${
                kind === option.key
                  ? "text-primary"
                  : "text-gray-700 dark:text-gray-200"
              }`}
            >
              {option.label}
            </span>
          </button>
        ))}
      </div>

      {/* 中：参数表单 */}
      <div className="nya-panel-inner nya-border flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto rounded-medium border p-4">
        <span className="text-[12px] font-semibold text-gray-600 dark:text-gray-300">
          {t("参数")}
        </span>

        {field(
          t("目标"),
          <OptionSelect
            ariaLabel={t("目标")}
            options={TARGETS}
            value={target}
            onChange={setTarget}
          />,
        )}

        {kind === "give" && (
          <>
            {field(
              t("物品"),
              <SearchSelect
                ariaLabel={t("物品")}
                options={ITEMS}
                value={item}
                onChange={setItem}
              />,
            )}
            <div>
              <div className="mb-1 flex justify-between text-xs text-gray-500 dark:text-gray-400">
                <span>{t("数量")}</span>
                <span>{count}</span>
              </div>
              <Slider
                aria-label={t("数量")}
                maxValue={64}
                minValue={1}
                size="sm"
                step={1}
                value={count}
                onChange={(value) =>
                  setCount(Math.round(Array.isArray(value) ? value[0] : value))
                }
              />
            </div>
          </>
        )}

        {kind === "effect" && (
          <>
            {field(
              t("效果"),
              <SearchSelect
                ariaLabel={t("效果")}
                options={EFFECTS}
                value={effect}
                onChange={setEffect}
              />,
            )}
            <div>
              <div className="mb-1 flex justify-between text-xs text-gray-500 dark:text-gray-400">
                <span>{t("时长（秒）")}</span>
                <span>{seconds}</span>
              </div>
              <Slider
                aria-label={t("时长（秒）")}
                maxValue={3600}
                minValue={1}
                size="sm"
                step={1}
                value={seconds}
                onChange={(value) =>
                  setSeconds(
                    Math.round(Array.isArray(value) ? value[0] : value),
                  )
                }
              />
            </div>
            <div>
              <div className="mb-1 flex justify-between text-xs text-gray-500 dark:text-gray-400">
                <span>{t("等级")}</span>
                <span>{amplifier}</span>
              </div>
              <Slider
                aria-label={t("等级")}
                maxValue={10}
                minValue={1}
                size="sm"
                step={1}
                value={amplifier}
                onChange={(value) =>
                  setAmplifier(
                    Math.round(Array.isArray(value) ? value[0] : value),
                  )
                }
              />
            </div>
            <Switch
              isSelected={hideParticles}
              size="sm"
              onValueChange={setHideParticles}
            >
              {t("隐藏粒子特效")}
            </Switch>
          </>
        )}

        {kind === "enchant" && (
          <>
            {field(
              t("附魔"),
              <SearchSelect
                ariaLabel={t("附魔")}
                options={ENCHANTS}
                value={enchant}
                onChange={setEnchant}
              />,
            )}
            <div>
              <div className="mb-1 flex justify-between text-xs text-gray-500 dark:text-gray-400">
                <span>{t("等级")}</span>
                <span>{enchantLevel}</span>
              </div>
              <Slider
                aria-label={t("等级")}
                maxValue={10}
                minValue={1}
                size="sm"
                step={1}
                value={enchantLevel}
                onChange={(value) =>
                  setEnchantLevel(
                    Math.round(Array.isArray(value) ? value[0] : value),
                  )
                }
              />
            </div>
          </>
        )}

        {(kind === "tp" || kind === "summon") && (
          <div className="flex items-end gap-2">
            {(
              [
                ["X", x, setX],
                ["Y", y, setY],
                ["Z", z, setZ],
              ] as const
            ).map(([axis, value, setValue]) => (
              <Input
                key={axis}
                aria-label={axis}
                classNames={{ inputWrapper: "h-9" }}
                label={axis}
                placeholder="0"
                size="sm"
                value={value}
                onValueChange={setValue}
              />
            ))}
          </div>
        )}

        {kind === "summon" &&
          field(
            t("实体"),
            <SearchSelect
              ariaLabel={t("实体")}
              options={ENTITIES}
              value={entity}
              onChange={setEntity}
            />,
          )}

        {kind === "gamemode" &&
          field(
            t("模式"),
            <OptionSelect
              ariaLabel={t("模式")}
              options={GAMEMODES}
              value={gamemode}
              onChange={setGamemode}
            />,
          )}

        {kind === "time" && (
          <>
            {field(
              t("时间"),
              <OptionSelect
                ariaLabel={t("时间")}
                options={[...TIMES, "custom" as const]}
                value={time}
                onChange={setTime}
              />,
            )}
            {time === "custom" &&
              field(
                t("刻数（0-24000）"),
                <Input
                  aria-label={t("刻数")}
                  classNames={{ inputWrapper: "h-9" }}
                  placeholder="6000"
                  size="sm"
                  value={timeValue}
                  onValueChange={setTimeValue}
                />,
              )}
          </>
        )}

        {kind === "weather" &&
          field(
            t("天气"),
            <OptionSelect
              ariaLabel={t("天气")}
              options={WEATHERS}
              value={weather}
              onChange={setWeather}
            />,
          )}

        {kind === "difficulty" &&
          field(
            t("难度"),
            <OptionSelect
              ariaLabel={t("难度")}
              options={DIFFICULTIES}
              value={difficulty}
              onChange={setDifficulty}
            />,
          )}

        {kind === "xp" && (
          <>
            <div>
              <div className="mb-1 flex justify-between text-xs text-gray-500 dark:text-gray-400">
                <span>{t("经验量")}</span>
                <span>{xpAmount}</span>
              </div>
              <Slider
                aria-label={t("经验量")}
                maxValue={1000}
                minValue={1}
                size="sm"
                step={1}
                value={xpAmount}
                onChange={(value) =>
                  setXpAmount(
                    Math.round(Array.isArray(value) ? value[0] : value),
                  )
                }
              />
            </div>
            <Switch isSelected={xpLevels} size="sm" onValueChange={setXpLevels}>
              {t("按等级（否则按点数）")}
            </Switch>
          </>
        )}
      </div>

      {/* 右：实时指令预览 */}
      <div className="nya-panel-inner nya-border flex w-72 flex-none flex-col gap-2 rounded-medium border p-3">
        <span className="text-[12px] font-semibold text-gray-600 dark:text-gray-300">
          {t("指令预览")}
        </span>
        <code className="nya-border min-h-0 flex-1 overflow-y-auto break-all rounded-lg border bg-default-100 p-3 font-mono text-[12px] text-gray-800 dark:text-gray-100">
          {command}
        </code>
        <Button
          color="primary"
          size="sm"
          startContent={copied ? <Checkmark20Regular /> : <Copy20Regular />}
          onPress={() => void copyCommand()}
        >
          {t("复制指令")}
        </Button>
        <p className="text-[10px] leading-relaxed text-gray-400">
          {t("需要相应权限")}
        </p>
      </div>
    </div>
  );

  if (embedded) {
    return (
      <CreatorToolShell
        icon={<Options20Regular />}
        title={t("指令生成器")}
        onBack={onClose}
      >
        {renderBody()}
      </CreatorToolShell>
    );
  }

  return (
    <Modal isOpen={isOpen} size="4xl" onClose={onClose} {...modalBehaviorProps}>
      <ModalContent className="h-[72vh] max-h-[72vh]">
        <ModalShell
          icon={<Options20Regular />}
          title={t("指令生成器")}
          onClose={onClose}
        >
          {renderBody()}
        </ModalShell>
      </ModalContent>
    </Modal>
  );
};

export default CommandGeneratorDialog;
