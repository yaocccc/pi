package render

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// ImageLink uses zero-based terminal rows/cells and a zero-based saved image index.
// Resolve ItemID and ImageIndex against the current session when clicked.
type ImageLink struct {
	ItemID          string
	ImageIndex      int
	Row, Start, End int
}

const imageReferenceColor = "#0087ff"

var imageReferencePattern = regexp.MustCompile(`\[image([1-9][0-9]*)\]`)

func imageRow(row string, count int) (string, []ImageLink) {
	plain := ansi.Strip(row)
	var links []ImageLink
	for _, match := range imageReferencePattern.FindAllStringSubmatchIndex(plain, -1) {
		number, err := strconv.Atoi(plain[match[2]:match[3]])
		if err != nil || number < 1 || number > count {
			continue
		}
		start := ansi.StringWidth(plain[:match[0]])
		links = append(links, ImageLink{ImageIndex: number - 1, Start: start, End: start + ansi.StringWidth(plain[match[0]:match[1]])})
	}
	if len(links) == 0 {
		return row, nil
	}
	var out strings.Builder
	cursor := 0
	for _, link := range links {
		out.WriteString(ansi.Cut(row, cursor, link.Start))
		token := ansi.Cut(row, link.Start, link.End)
		// Use link blue, without underlining or terminal-controlled OSC URLs.
		out.WriteString(paint(ansi.Strip(token), imageReferenceColor))
		cursor = link.End
	}
	out.WriteString(ansi.Cut(row, cursor, ansi.StringWidth(row)))
	return out.String(), links
}

// StyleImageReferences colors complete valid references in rendered textarea
// lines. Wrapped/incomplete references and surrounding styles are left untouched;
// cell geometry is unchanged.
func StyleImageReferences(text string, count int) string {
	if count <= 0 {
		return text
	}
	rows := strings.Split(text, "\n")
	for i, row := range rows {
		rows[i], _ = imageRow(row, count)
	}
	return strings.Join(rows, "\n")
}

func imageRows(rows []string, itemID string, count, rowOffset int) ([]string, []ImageLink) {
	if count <= 0 {
		return rows, nil
	}
	styled := append([]string(nil), rows...)
	var links []ImageLink
	for row, line := range rows {
		var found []ImageLink
		styled[row], found = imageRow(line, count)
		for _, link := range found {
			link.ItemID = itemID
			link.Row = rowOffset + row
			links = append(links, link)
		}
	}
	return styled, links
}
