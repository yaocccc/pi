package render

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"pi-goui/internal/core/session"
)

func TestImageLinksFollowRenderedUnicodeGeometry(t *testing.T) {
	item := session.Item{ID: "u/0", Role: "user", Text: "你照这个图改 [image1]，然后参考这个图 [image2]", Images: []session.Image{{Data: "PRIVATE_ONE", MimeType: "image/png"}, {Data: "PRIVATE_TWO", MimeType: "image/jpeg"}}, MessageIndex: 2}
	for _, width := range []int{1, 8, 16, 30, 60, 100, 132} {
		items := []session.Item{{ID: "a", Role: "assistant", Text: "Earlier message", MessageIndex: 1}, item}
		opts := Options{Width: width, MessageCount: 2}
		doc := Transcript(items, testTheme(), opts)
		rows := strings.Split(doc.Text, "\n")
		checkWidth(t, doc.Text, width)
		for _, link := range doc.ImageLinks {
			if link.ItemID != item.ID || link.ImageIndex < 0 || link.ImageIndex >= 2 || link.Row < 0 || link.Row >= len(rows) {
				t.Fatalf("bad link %+v", link)
			}
			want := []string{"[image1]", "[image2]"}[link.ImageIndex]
			if got := ansi.Strip(ansi.Cut(rows[link.Row], link.Start, link.End)); got != want {
				t.Fatalf("width %d: link %+v hits %q", width, link, got)
			}
			span := doc.Content[link.Row]
			if !span.Content || link.Start < span.Start || link.End > span.End {
				t.Fatalf("link outside selectable body: %+v %+v", link, span)
			}
			if !strings.Contains(rows[link.Row], paint(want, imageReferenceColor)) || strings.Contains(rows[link.Row], "\x1b[4m") {
				t.Fatal("reference must be blue without underline")
			}
		}
		if width >= 30 && len(doc.ImageLinks) != 2 {
			t.Fatalf("width %d: missing links: %q %+v", width, ansi.Strip(doc.Text), doc.ImageLinks)
		}
		if strings.Contains(doc.Text, "PRIVATE_") || strings.Contains(doc.Text, "\x1b]8;") {
			t.Fatal("data or external OSC link leaked")
		}
		if len(doc.Blocks) != 2 {
			t.Fatal("images became separate bubbles")
		}
		var cache TranscriptCache
		for i := 0; i < 2; i++ {
			if got := cache.Transcript(items, testTheme(), opts); !reflect.DeepEqual(got, doc) {
				t.Fatalf("cached link offsets differ at width %d", width)
			}
		}
	}
}

func TestImageReferenceColorPreservesTextAndRejectsInvalidIndices(t *testing.T) {
	original := paint("中文 [image1] [image2] [image0] [image999999999999999999999999]", "#123456")
	styled := StyleImageReferences(original, 1)
	if ansi.Strip(styled) != ansi.Strip(original) || ansi.StringWidth(styled) != ansi.StringWidth(original) {
		t.Fatal("link color changed geometry")
	}
	if strings.Contains(styled, "\x1b[4m") || !strings.Contains(styled, paint("[image1]", imageReferenceColor)) || !strings.Contains(styled, "38;2;18;52;86") {
		t.Fatal("link color or surrounding foreground incorrect")
	}
	if StyleImageReferences(original, 0) != original {
		t.Fatal("unstyled fake marker changed")
	}
	item := session.Item{ID: "u", Role: "user", Text: "[image1] [image2] [image0] [image999999999999999999999999]"}
	if doc := Transcript([]session.Item{item}, testTheme(), Options{Width: 100}); len(doc.ImageLinks) != 0 {
		t.Fatal("plain text received image links")
	}
	item.Images = []session.Image{{Data: "one"}}
	var cache TranscriptCache
	doc := cache.Transcript([]session.Item{item}, testTheme(), Options{Width: 100})
	if len(doc.ImageLinks) != 1 || doc.ImageLinks[0].ImageIndex != 0 {
		t.Fatal("invalid references became links")
	}
	item.Images = append(item.Images, session.Image{Data: "two"})
	doc = cache.Transcript([]session.Item{item}, testTheme(), Options{Width: 100})
	if len(doc.ImageLinks) != 2 {
		t.Fatal("cache missed new image reference")
	}
	item.Images[0].Data = "replacement bytes"
	if got := cache.Transcript([]session.Item{item}, testTheme(), Options{Width: 100}); !reflect.DeepEqual(got, doc) {
		t.Fatal("image byte replacement unnecessarily changed display")
	}
}
