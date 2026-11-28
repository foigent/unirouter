package endpointprotocol

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWanStatusTrustsExactTaskAndReportedMilliseconds(t *testing.T) {
	profile := dashScopeWanVideoProfile{}
	const good = `{"output":{"task_id":"task-a","task_status":"SUCCEEDED","video_url":"https://dashscope-result-bj.oss-cn-beijing.aliyuncs.com/fixture.mp4"},"usage":{"video_count":1,"duration":2.000,"input_video_duration":0,"output_video_duration":2,"SR":480,"served_model":"wan3.0-video"}}`
	outcome, err := profile.ParseStatusResponse(DashScopeWanVideo, "task-a", 200, []byte(good))
	require.NoError(t, err)
	require.Equal(t, MediaTaskCompleted, outcome.ProviderState)
	require.Equal(t, []UnitObservation{observedUnit(UnitInputVideoDurationMS, "0"), observedUnit(UnitOutputVideoDurationMS, "2000")}, outcome.Units)
	require.NoError(t, ValidateMediaOutcome(ProfileDashScopeWanVideo, DashScopeWanVideo, outcome))
	for name, bad := range map[string]string{
		"wrong task":            strings.Replace(good, "task-a", "task-b", 1),
		"wrong resolution":      strings.Replace(good, `"SR":480`, `"SR":720`, 1),
		"string resolution":     strings.Replace(good, `"SR":480`, `"SR":"480P"`, 1),
		"wrong served model":    strings.Replace(good, `"wan3.0-video"`, `"wan3.0-video-prime"`, 1),
		"inconsistent duration": strings.Replace(good, `"duration":2.000`, `"duration":2.001`, 1),
		"asset host mismatch":   strings.Replace(good, "dashscope-result-bj.oss-cn-beijing.aliyuncs.com", "fixture.invalid", 1),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := profile.ParseStatusResponse(DashScopeWanVideo, "task-a", 200, []byte(bad))
			require.Error(t, err)
		})
	}
}

func TestWanOutputRequiresVideoTrackAndExpectedResolutionAndDuration(t *testing.T) {
	profile := dashScopeWanVideoProfile{}
	for _, tc := range []struct {
		name string
		ok   bool
	}{
		{"dashscope-wan-synthetic-480p.mp4", true},
		{"dashscope-wan-synthetic-audio-only.mp4", false},
		{"dashscope-wan-synthetic-other-video.mp4", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := os.ReadFile("testdata/" + tc.name)
			require.NoError(t, err)
			require.Equal(t, tc.ok, ValidateMP4(data, profile.VideoRequirements()))
			require.False(t, ValidateMP4(data[:len(data)-1], profile.VideoRequirements()))
		})
	}
}
