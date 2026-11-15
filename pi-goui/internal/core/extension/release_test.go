package extension

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestReleaseCombinesOnlySupportedCapabilitiesAndPreservesErrors(t *testing.T) {
	ctx := context.Background()
	status, err := New(nil).CheckRelease(ctx, nil)
	if err != nil || status.Known {
		t.Fatalf("empty runtime lost native fallback: %+v %v", status, err)
	}
	makeContribution := func(status ReleaseStatus, err error) contribution {
		return contribution{Capabilities{CheckRelease: func(context.Context, Client) (ReleaseStatus, error) { return status, err }}}
	}
	r := New([]Extension{
		makeContribution(ReleaseStatus{}, nil),
		makeContribution(ReleaseStatus{Known: true, Idle: true}, nil),
		makeContribution(ReleaseStatus{Known: true, Reasons: []string{"background task"}}, nil),
	})
	status, err = r.CheckRelease(ctx, nil)
	if err != nil || !status.Known || status.Idle || !reflect.DeepEqual(status.Reasons, []string{"background task"}) {
		t.Fatalf("unsafe combined result: %+v %v", status, err)
	}
	failure := errors.New("cannot confirm")
	r = New([]Extension{makeContribution(ReleaseStatus{Known: true}, failure)})
	status, err = r.CheckRelease(ctx, nil)
	if !status.Known || status.Idle || !errors.Is(err, failure) {
		t.Fatalf("confirmation error was lost: %+v %v", status, err)
	}
}
