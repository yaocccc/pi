// Package hostui implements optional third-party UI views using host callbacks.
// It never imports the host ui package or owns extension business operations.
package hostui

type Rect struct{ X, Y, Width, Height int }

func (r Rect) Contains(x, y int) bool {
	h := r.Height
	if h == 0 {
		h = 1
	}
	return x >= r.X && x < r.X+r.Width && y >= r.Y && y < r.Y+h
}
