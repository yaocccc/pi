package session

// CompactionMetrics are content estimates, not provider usage or billed tokens.
// They describe the same committed history immediately before and after its cut.
type CompactionMetrics struct {
	BeforeTokens int
	AfterTokens  int
}
