//go:build !unix

package runtime

func captureDescendants(pid int) (map[int]string, error) { return nil, nil }
func finishDescendants(children map[int]string) error    { return nil }
