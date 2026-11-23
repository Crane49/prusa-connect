package main

import (
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/routesapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// The largest file is the limit the operator approved for the upload route,
// and the wire's bound when the route was not approved.
func TestTheLargestFileIsTheGrantedUploadLimit(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		grants wirerpc.Grants
		want   int64
	}{
		{"approved as asked", wirerpc.Grants{Staged: []wirerpc.StagedGrant{
			{Method: uploadMethod, Path: uploadPath, MaxMiB: 1024},
		}}, 1024 << 20},
		{"changed by the operator", wirerpc.Grants{Staged: []wirerpc.StagedGrant{
			{Method: uploadMethod, Path: uploadPath, MaxMiB: 3000},
		}}, 3000 << 20},
		{"another route", wirerpc.Grants{Staged: []wirerpc.StagedGrant{
			{Method: "POST", Path: uploadPath, MaxMiB: 3000},
		}}, routesapi.MaxModuleBody},
		{"not approved", wirerpc.Grants{}, routesapi.MaxModuleBody},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := maxUploadBytes(tc.grants); got != tc.want {
				t.Errorf("maxUploadBytes = %d, want %d", got, tc.want)
			}
		})
	}
}
