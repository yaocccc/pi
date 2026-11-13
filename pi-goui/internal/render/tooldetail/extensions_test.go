package tooldetail

import "pi-goui/internal/session"

// Legacy host integration tests invoke the extracted views through host hooks.
func (v *renderer) worker(item session.Item, args, details map[string]any) {
	v.extensions().Worker(item, args, details)
}
func (v *renderer) obsRecall(item session.Item, args, details map[string]any) {
	v.extensions().ObsRecall(item, args, details)
}
func (v *renderer) askQuestion(item session.Item, args, details map[string]any) {
	v.extensions().AskQuestion(item, args, details)
}
func (v *renderer) updatePlan(item session.Item, args, details map[string]any) {
	v.extensions().UpdatePlan(item, args, details)
}
func (v *renderer) webSearch(item session.Item, args, details map[string]any) {
	v.extensions().WebSearch(item, args, details)
}
func (v *renderer) fetchContent(item session.Item, args, details map[string]any) {
	v.extensions().FetchContent(item, args, details)
}
func (v *renderer) getSearchContent(item session.Item, args, details map[string]any) {
	v.extensions().GetSearchContent(item, args, details)
}
func (v *renderer) sourceCheck(item session.Item, args, details map[string]any) {
	v.extensions().SourceCheck(item, args, details)
}
func (v *renderer) webEnable(item session.Item, args, details map[string]any) {
	v.extensions().WebEnable(item, args, details)
}
func (v *renderer) webEntries(m map[string]any, key, label, title, url string) {
	v.extensions().WebEntries(m, key, label, title, url)
}
