package frame

import "strconv"

// Each role has its own diagonal endpoints. Palettes follow the named theme,
// while keeping user greens, assistant blues and error reds recognizable.
var framePresetColors = map[string][10][2]string{
	"pi": {
		{"68d3ae", "80bff5"}, {"78b5ef", "c4b0f2"}, {"e5b567", "c78bba"},
		{"9bd28f", "6ebbc9"}, {"b6a0e8", "78c9e8"}, {"e6a0aa", "d6b978"},
		{"6faabb", "a6c787"}, {"d49de0", "e6ba8b"}, {"8eade8", "78cbb8"}, {"ef7f88", "e9af70"},
	},
	"catppuccin": {
		{"a6e3a1", "94e2d5"}, {"89b4fa", "b4befe"}, {"f9e2af", "fab387"},
		{"94e2d5", "74c7ec"}, {"cba6f7", "f5c2e7"}, {"f2cdcd", "f5e0dc"},
		{"74c7ec", "a6e3a1"}, {"f5c2e7", "fab387"}, {"b4befe", "94e2d5"}, {"f38ba8", "fab387"},
	},
	"tokyo-night": {
		{"9ece6a", "73daca"}, {"7aa2f7", "7dcfff"}, {"e0af68", "ff9e64"},
		{"73daca", "2ac3de"}, {"bb9af7", "7aa2f7"}, {"ff9e64", "db4b4b"},
		{"2ac3de", "9ece6a"}, {"9d7cd8", "ff9e64"}, {"7dcfff", "bb9af7"}, {"f7768e", "ff9e64"},
	},
	"nord": {
		{"a3be8c", "8fbcbb"}, {"81a1c1", "88c0d0"}, {"ebcb8b", "d08770"},
		{"8fbcbb", "5e81ac"}, {"b48ead", "81a1c1"}, {"d08770", "b48ead"},
		{"5e81ac", "a3be8c"}, {"88c0d0", "ebcb8b"}, {"81a1c1", "a3be8c"}, {"bf616a", "d08770"},
	},
	"rose-pine": {
		{"9ccfd8", "31748f"}, {"c4a7e7", "9ccfd8"}, {"f6c177", "ebbcba"},
		{"31748f", "c4a7e7"}, {"ebbcba", "c4a7e7"}, {"eb6f92", "f6c177"},
		{"9ccfd8", "908caa"}, {"c4a7e7", "f6c177"}, {"31748f", "ebbcba"}, {"eb6f92", "ebbcba"},
	},
}

// Palette returns RGB corners (top-left, top-right, bottom-left, bottom-right)
// for borders and matching activity highlights. Unknown presets use Pi defaults;
// unknown roles return false.
func Palette(preset, kind string) ([4][3]int, bool) {
	roles := map[string]int{"user": 0, "assistant": 1, "tool": 2, "worker": 3, "input": 4, "system": 5, "sidebar": 6, "popup": 7, "codemode": 8, "error": 9}
	role, ok := roles[kind]
	if !ok {
		return [4][3]int{}, false
	}
	palette, ok := framePresetColors[preset]
	if !ok {
		palette = framePresetColors["pi"]
	}
	rgb := func(hex string) [3]int {
		n, _ := strconv.ParseUint(hex, 16, 24)
		return [3]int{int(n >> 16), int(n >> 8 & 255), int(n & 255)}
	}
	a, b := rgb(palette[role][0]), rgb(palette[role][1])
	return [4][3]int{a, b, b, a}, true
}
