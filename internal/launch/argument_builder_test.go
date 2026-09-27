package launch

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"nekolauncher/internal/auth"
)

// gameArgumentsFixture 1.18+ 版本档案里账号参数的写法：
// --clientId / --xuid 各自是"标志 + 占位符"两个**独立元素**。
func gameArgumentsFixture() []json.RawMessage {
	elements := []string{
		`"--username"`, `"${auth_player_name}"`,
		`"--clientId"`, `"${clientid}"`,
		`"--xuid"`, `"${auth_xuid}"`,
		`"--userType"`, `"${user_type}"`,
		`"--versionType"`, `"${version_type}"`,
	}
	arguments := make([]json.RawMessage, 0, len(elements))
	for _, element := range elements {
		arguments = append(arguments, json.RawMessage(element))
	}
	return arguments
}

func versionProfileFixture() *MinecraftVersionProfile {
	return &MinecraftVersionProfile{
		Id:            "test",
		MainClass:     "net.minecraft.client.main.Main",
		AssetsId:      "5",
		VersionType:   "release",
		GameArguments: gameArgumentsFixture(),
	}
}

func buildFixtureArguments(t *testing.T, profile *MinecraftVersionProfile, account MinecraftAccount) []string {
	t.Helper()

	return buildFixtureArgumentsAt(t, profile, t.TempDir(), account)
}

// buildFixtureArgumentsAt 同上，但指定 .minecraft 目录（日志配置等落在里面）。
func buildFixtureArgumentsAt(
	t *testing.T,
	profile *MinecraftVersionProfile,
	minecraftDirectory string,
	account MinecraftAccount,
) []string {
	t.Helper()

	arguments, err := MinecraftArgumentBuilder{}.Build(
		profile,
		MinecraftLaunchOptions{
			MinecraftDirectory: minecraftDirectory,
			VersionId:          profile.Id,
			Account:            account,
			MinimumMemoryMb:    1024,
			MaximumMemoryMb:    4096,
			LauncherName:       "NekoLauncher",
			LauncherVersion:    "test",
		},
		filepath.Join(t.TempDir(), "natives"),
		[]string{"lib.jar"},
		profile.MainClass,
		nil, nil, nil, nil,
	)
	if err != nil {
		t.Fatalf("构建参数失败：%v", err)
	}

	return arguments
}

func indexOfArgument(arguments []string, value string) int {
	for index, argument := range arguments {
		if argument == value {
			return index
		}
	}
	return -1
}

// TestBuildDropsEmptyAccountPlaceholders 离线账号没有 clientId / xuid，
// 这两个参数必须整对省略：留下空值会触发空参数校验、直接拒绝启动。
func TestBuildDropsEmptyAccountPlaceholders(t *testing.T) {
	arguments := buildFixtureArguments(t, versionProfileFixture(), MustOfflineAccount("Tester"))

	for index, argument := range arguments {
		if argument == "" {
			t.Errorf("第 %d 项是空参数：前后为 %q", index, arguments[max(index-2, 0):min(index+2, len(arguments))])
		}
	}
	// 整对消失：标志与取值都不该留下，避免出现 "--clientId --xuid" 这种错位
	for _, dropped := range []string{"--clientId", "--xuid"} {
		if index := indexOfArgument(arguments, dropped); index >= 0 {
			t.Errorf("%s 应被省略，实际在第 %d 项（下一项 %q）", dropped, index, arguments[index+1])
		}
	}
	// 该保留的账号参数照常下发
	if index := indexOfArgument(arguments, "--username"); index < 0 || arguments[index+1] != "Tester" {
		t.Errorf("--username 应保留，实际 %v", arguments)
	}
	if index := indexOfArgument(arguments, "--versionType"); index < 0 || arguments[index+1] != "release" {
		t.Errorf("--versionType 应保留，实际 %v", arguments)
	}
	// user_type 对离线账号是 "legacy"（非空），因此 --userType 应保留
	if index := indexOfArgument(arguments, "--userType"); index < 0 || arguments[index+1] != "legacy" {
		t.Errorf("--userType 应保留为 legacy，实际 %v", arguments)
	}
	if err := validateFinalArguments(arguments); err != nil {
		t.Errorf("最终命令不应再被判非法：%v", err)
	}
}

// TestBuildKeepsAccountPlaceholdersWhenPresent 正版账号有 clientId / xuid 时应照常下发。
func TestBuildKeepsAccountPlaceholdersWhenPresent(t *testing.T) {
	account := auth.MicrosoftAccount{
		Username:    "Steve",
		Uuid:        "00000000000000000000000000000001",
		AccessToken: "token",
		ClientId:    "client-abc",
		XboxUserId:  "1234567890123456",
	}
	arguments := buildFixtureArguments(t, versionProfileFixture(), account)

	if index := indexOfArgument(arguments, "--clientId"); index < 0 || arguments[index+1] != "client-abc" {
		t.Errorf("--clientId 应下发，实际 %v", arguments)
	}
	if index := indexOfArgument(arguments, "--xuid"); index < 0 || arguments[index+1] != "1234567890123456" {
		t.Errorf("--xuid 应下发，实际 %v", arguments)
	}
	if index := indexOfArgument(arguments, "--userType"); index < 0 || arguments[index+1] != "msa" {
		t.Errorf("--userType 应为 msa，实际 %v", arguments)
	}
}

// TestBuildStillRejectsOtherEmptyPlaceholders 只有"可空"占位符允许解析为空；
// 其它占位符解析为空（如资源索引缺失）仍要报错，不能被静默吞掉。
func TestBuildStillRejectsOtherEmptyPlaceholders(t *testing.T) {
	profile := versionProfileFixture()
	profile.AssetsId = ""
	profile.GameArguments = append(
		[]json.RawMessage{json.RawMessage(`"--assetIndex"`), json.RawMessage(`"${assets_index_name}"`)},
		profile.GameArguments...,
	)

	arguments := buildFixtureArguments(t, profile, MustOfflineAccount("Tester"))
	if err := validateFinalArguments(arguments); err == nil {
		t.Error("资源索引为空应仍然报错，而不是被当成可空占位符省略")
	}
}

// TestIsNullablePlaceholder 可空占位符的识别：必须整段就是占位符本身。
func TestIsNullablePlaceholder(t *testing.T) {
	nullable := []string{"${clientid}", "${auth_xuid}", "${user_type}", " ${clientid} "}
	for _, source := range nullable {
		if !isNullablePlaceholder(source) {
			t.Errorf("%q 应判为可空占位符", source)
		}
	}
	notNullable := []string{
		"${assets_index_name}", "${auth_player_name}",
		"${clientid}-suffix", "prefix-${clientid}", "",
		"${clientid}${auth_xuid}",
	}
	for _, source := range notNullable {
		if isNullablePlaceholder(source) {
			t.Errorf("%q 不应判为可空占位符", source)
		}
	}
}

// TestAppendResolvedArgument 成对省略的行为：只摘刚追加的标志，不误删别的参数。
func TestAppendResolvedArgument(t *testing.T) {
	cases := []struct {
		name     string
		initial  []string
		source   string
		resolved string
		expected []string
	}{
		{"空值摘掉标志", []string{"--clientId"}, "${clientid}", "", []string{}},
		{"非空值正常追加", []string{"--clientId"}, "${clientid}", "abc", []string{"--clientId", "abc"}},
		{"前一项不是标志时不误删", []string{"value"}, "${clientid}", "", []string{"value"}},
		{"没有前一项也不出错", nil, "${clientid}", "", []string{}},
		{"非可空占位符照旧保留空值", []string{"--assetIndex"}, "${assets_index_name}", "", []string{"--assetIndex", ""}},
		{"带前缀的占位符不算可空，取值照常保留", []string{"--x"}, "prefix-${clientid}", "prefix-", []string{"--x", "prefix-"}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			target := append([]string{}, testCase.initial...)
			appendResolvedArgument(&target, testCase.source, testCase.resolved)
			if len(target) != len(testCase.expected) {
				t.Fatalf("期望 %q，实际 %q", testCase.expected, target)
			}
			for index := range target {
				if target[index] != testCase.expected[index] {
					t.Fatalf("期望 %q，实际 %q", testCase.expected, target)
				}
			}
		})
	}
}
