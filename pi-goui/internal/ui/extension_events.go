package ui

import "pi-goui/internal/extensions"

const extensionEventPrefix = extensions.EventPrefix

func (t *tab) extensionEvent(message string) bool { return t.codex.Event(message, t.notify) }
