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
	"gradient-lavender": {
		{"a6e3a1", "94e2d5"}, {"89b4fa", "b4befe"}, {"f9e2af", "fab387"},
		{"94e2d5", "74c7ec"}, {"cba6f7", "f5c2e7"}, {"f2cdcd", "f5e0dc"},
		{"74c7ec", "a6e3a1"}, {"f5c2e7", "fab387"}, {"b4befe", "94e2d5"}, {"f38ba8", "fab387"},
	},
	"gradient-indigo": {
		{"9ece6a", "73daca"}, {"7aa2f7", "7dcfff"}, {"e0af68", "ff9e64"},
		{"73daca", "2ac3de"}, {"bb9af7", "7aa2f7"}, {"ff9e64", "db4b4b"},
		{"2ac3de", "9ece6a"}, {"9d7cd8", "ff9e64"}, {"7dcfff", "bb9af7"}, {"f7768e", "ff9e64"},
	},
	"gradient-frost": {
		{"a3be8c", "8fbcbb"}, {"81a1c1", "88c0d0"}, {"ebcb8b", "d08770"},
		{"8fbcbb", "5e81ac"}, {"b48ead", "81a1c1"}, {"d08770", "b48ead"},
		{"5e81ac", "a3be8c"}, {"88c0d0", "ebcb8b"}, {"81a1c1", "a3be8c"}, {"bf616a", "d08770"},
	},
	"gradient-lilac": {
		{"9ccfd8", "31748f"}, {"c4a7e7", "9ccfd8"}, {"f6c177", "ebbcba"},
		{"31748f", "c4a7e7"}, {"ebbcba", "c4a7e7"}, {"eb6f92", "f6c177"},
		{"9ccfd8", "908caa"}, {"c4a7e7", "f6c177"}, {"31748f", "ebbcba"}, {"eb6f92", "ebbcba"},
	},
	"gradient-clay": {
		{"a8c693", "91b7ac"}, {"95b5c6", "c6b6d4"}, {"e6bf81", "dc9972"},
		{"91b7ac", "c4c394"}, {"e59978", "efc2a2"}, {"d8a4a0", "e6bf81"},
		{"a2b7ab", "c9b394"}, {"c6a7bc", "e59978"}, {"95b5c6", "a8c693"}, {"ef958c", "dc9972"},
	},
	"gradient-mint": {
		{"77dba4", "88e1cd"}, {"85b8ed", "a1cbdf"}, {"ebc37c", "bca883"},
		{"65b9ae", "8bc9b2"}, {"88e1cd", "d4ebe7"}, {"c0aedc", "a8babc"},
		{"729dba", "8fb5a4"}, {"bba9cf", "88e1cd"}, {"89cbd0", "85b8ed"}, {"ff949f", "ebc37c"},
	},
	"gradient-azure": {
		{"3fb950", "56d364"}, {"58a6ff", "a5d6ff"}, {"d29922", "e3b341"},
		{"39c5cf", "56d364"}, {"58a6ff", "bc8cff"}, {"db61a2", "d2a8ff"},
		{"79c0ff", "7ee787"}, {"bc8cff", "ffa657"}, {"39c5cf", "79c0ff"}, {"f85149", "ff7b72"},
	},
	"gradient-violet": {
		{"50fa7b", "8be9fd"}, {"80bfff", "ff79c6"}, {"f1fa8c", "ffb86c"},
		{"8be9fd", "80bfff"}, {"bd93f9", "80bfff"}, {"ff79c6", "ffb86c"},
		{"8be9fd", "50fa7b"}, {"ff79c6", "f1fa8c"}, {"80bfff", "8be9fd"}, {"ff5555", "ffb86c"},
	},
	"gradient-copper": {
		{"b8bb26", "8ec07c"}, {"83a598", "d3869b"}, {"fabd2f", "d79921"},
		{"8ec07c", "83a598"}, {"fe8019", "83a598"}, {"d3869b", "d79921"},
		{"8ec07c", "b8bb26"}, {"d3869b", "fabd2f"}, {"83a598", "8ec07c"}, {"fb4934", "d79921"},
	},
	"gradient-raspberry": {
		{"a6e22e", "66d9ef"}, {"78b5ea", "ae81ff"}, {"e6db74", "fd971f"},
		{"66d9ef", "78b5ea"}, {"f92672", "78b5ea"}, {"ae81ff", "fd971f"},
		{"66d9ef", "a6e22e"}, {"ae81ff", "e6db74"}, {"78b5ea", "66d9ef"}, {"ff6188", "fd971f"},
	},
	"gradient-sky": {
		{"98c379", "56b6c2"}, {"82bfff", "c678dd"}, {"e5c07b", "d19a66"},
		{"56b6c2", "82bfff"}, {"61afef", "82bfff"}, {"c678dd", "d19a66"},
		{"56b6c2", "98c379"}, {"c678dd", "e5c07b"}, {"82bfff", "56b6c2"}, {"e06c75", "d19a66"},
	},
	"gradient-moss": {
		{"a7c080", "83c092"}, {"7fbbb3", "d699b6"}, {"dbbc7f", "e69875"},
		{"83c092", "7fbbb3"}, {"a7c080", "7fbbb3"}, {"d699b6", "e69875"},
		{"83c092", "a7c080"}, {"d699b6", "dbbc7f"}, {"7fbbb3", "83c092"}, {"e67e80", "e69875"},
	},
	"gradient-sand": {
		{"98bb6c", "7aa89f"}, {"7e9cd8", "957fb8"}, {"e6c384", "ffa066"},
		{"7aa89f", "7e9cd8"}, {"c0a36e", "7e9cd8"}, {"957fb8", "ffa066"},
		{"7aa89f", "98bb6c"}, {"957fb8", "e6c384"}, {"7e9cd8", "7aa89f"}, {"e46876", "ffa066"},
	},
	"gradient-teal": {
		{"92ae20", "61bdb2"}, {"429de3", "a695d3"}, {"d4a72c", "df8556"},
		{"61bdb2", "429de3"}, {"2aa198", "429de3"}, {"a695d3", "df8556"},
		{"61bdb2", "92ae20"}, {"a695d3", "d4a72c"}, {"429de3", "61bdb2"}, {"ff7771", "df8556"},
	},
	"gradient-amber": {
		{"bae67e", "95e6cb"}, {"73d0ff", "d4bfff"}, {"ffd580", "ffa759"},
		{"95e6cb", "73d0ff"}, {"ffcc66", "73d0ff"}, {"d4bfff", "ffa759"},
		{"95e6cb", "bae67e"}, {"d4bfff", "ffd580"}, {"73d0ff", "95e6cb"}, {"ff7373", "ffa759"},
	},
	"solid-slate": solidFrame([10]string{"93c6a3", "9bbfea", "e1bf82", "88bdbe", "a9b8dd", "c1abc9", "93aabe", "b4a5cc", "8fb8ca", "ef9c9b"}),
	"solid-amber": solidFrame([10]string{"adca88", "cabca0", "f3ca7d", "b5c09a", "edbc6c", "d6aa86", "b3a58e", "d9b58e", "c9b799", "ec9284"}),
	"solid-jade":  solidFrame([10]string{"9adba0", "95c7c5", "e1c477", "86b89a", "83c7a7", "c1b593", "96aa98", "b3c5a2", "a0bdca", "ea9792"}),
	"solid-plum":  solidFrame([10]string{"b4ce9e", "b3b6de", "ecc382", "abbcb9", "d5a4d9", "d9a4bb", "b6a4bf", "c5acd5", "a8b3d3", "f1a0ac"}),
}

// solidFrame gives each role its own uniform border.
func solidFrame(colors [10]string) (pairs [10][2]string) {
	for i, c := range colors {
		pairs[i] = [2]string{c, c}
	}
	return pairs
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
