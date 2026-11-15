package hostui

import (
	"context"
	"encoding/json"
	"errors"
	"pi-goui/internal/core/extension"
	"pi-goui/internal/runtime"
	"strings"
)

func checkRelease(ctx context.Context, client extension.Client) (extension.ReleaseStatus, error) {
	if client == nil {
		return extension.ReleaseStatus{}, nil
	}
	raw, err := client.CallExtension(ctx, "session.activity", nil)
	if err != nil {
		var backend *runtime.ExtensionError
		if errors.As(err, &backend) {
			// A bridge exists but cannot certify its extensions. Falling back to
			// native idle here would hide detached work in an older installation.
			return extension.ReleaseStatus{Known: true, Reasons: []string{"扩展后台活动尚无法确认：" + backend.Message}}, err
		}
		if errors.Is(err, runtime.ErrExtensionUnavailable) {
			return extension.ReleaseStatus{}, nil
		}
		return extension.ReleaseStatus{}, err
	}
	var result struct {
		Version int      `json:"version"`
		Idle    *bool    `json:"idle"`
		Reasons []string `json:"reasons"`
	}
	invalid := func() (extension.ReleaseStatus, error) {
		err := errors.New("扩展后台活动响应无效，继续占用会话")
		return extension.ReleaseStatus{Known: true, Reasons: []string{err.Error()}}, err
	}
	if json.Unmarshal(raw, &result) != nil || result.Version != 1 || result.Idle == nil || result.Reasons == nil || len(result.Reasons) > 64 {
		return invalid()
	}
	for _, reason := range result.Reasons {
		if strings.TrimSpace(reason) == "" || len(reason) > 2048 {
			return invalid()
		}
	}
	if *result.Idle != (len(result.Reasons) == 0) {
		return invalid()
	}
	return extension.ReleaseStatus{Known: true, Idle: *result.Idle, Reasons: result.Reasons}, nil
}
