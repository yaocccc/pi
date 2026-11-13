package cards

// CodexModeLabel is presentation-only; persisted mode values remain unchanged.
func CodexModeLabel(mode string) string {
	switch mode {
	case "fast":
		return "fast x2.5"
	case "ultrafast":
		return "ultrafast x8"
	default:
		return "normal x1"
	}
}
