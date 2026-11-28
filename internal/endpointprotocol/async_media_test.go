package endpointprotocol

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestMediaTaskPollingLeavesReadsAfterShortWatch(t *testing.T) {
	start := time.Date(2026, time.November, 28, 0, 0, 0, 0, time.UTC)
	deadline := start.Add(MediaTaskReadWindow)
	first := NextMediaTaskStatusRead(start, deadline)
	if first.Sub(start) != 15*time.Second {
		t.Fatalf("first read interval = %s", first.Sub(start))
	}
	var reads int
	var afterTwelveMinutes bool
	var last time.Time
	for next := first; next.Before(deadline); next = NextMediaTaskStatusRead(next, deadline) {
		reads++
		if reads > MediaTaskMaxStatusReads {
			t.Fatalf("poll schedule exceeds the task read budget at %s", next)
		}
		afterTwelveMinutes = afterTwelveMinutes || next.Sub(start) == 12*time.Minute
		last = next
	}
	if !afterTwelveMinutes || deadline.Sub(last) != 30*time.Second {
		t.Fatalf("missing late recovery opportunities, reads=%d last=%s", reads, last)
	}
}

func TestWanProviderUnknownIsNotFailureOrApproval(t *testing.T) {
	const wire = `{"output":{"task_id":"wan-task","task_status":"UNKNOWN"}}`
	p := dashScopeWanVideoProfile{}
	out, err := p.ParseStatusResponse(DashScopeWanVideo, "wan-task", http.StatusOK, []byte(wire))
	if err != nil || out.ProviderState != MediaTaskUnavailable || out.OutputCount != 0 {
		t.Fatalf("unavailable status = %+v, %v", out, err)
	}
	if _, err := p.ParseStatusResponse(DashScopeWanVideo, "wrong-task", http.StatusOK, []byte(wire)); err == nil {
		t.Fatal("unknown status for another task was accepted")
	}
}

func TestH3VideoMeteringIsBoundToExactTask(t *testing.T) {
	const wire = `{"task":{"id":"h3-task","model":"MiniMax-H3","status":"succeeded","content":{"url":"https://cdn.hailuoai.com/sample.mp4"},"resolution":"768P","duration":4,"usage":{"total_seconds":4,"input_seconds":0,"output_seconds":4,"input_image_count":0,"input_audio_seconds":0}}}`
	p := miniMaxH3VideoProfile{}
	out, err := p.ParseStatusResponse(MiniMaxH3Video, "h3-task", http.StatusOK, []byte(wire))
	if err != nil || out.ProviderState != MediaTaskCompleted || len(out.Units) != 3 || ValidateMediaOutcome(ProfileMiniMaxH3Video, MiniMaxH3Video, out) != nil {
		t.Fatalf("terminal result = %+v, %v", out, err)
	}
	for _, pair := range [][2]string{
		{"h3-task", "other-task"}, {"MiniMax-H3", "MiniMax-H3-Max"},
		{`"output_seconds":4`, `"output_seconds":4.1`}, {`"input_image_count":0`, `"input_image_count":1`},
		{"768P", "480P"}, {"cdn.hailuoai.com", "example.invalid"},
	} {
		bad := strings.Replace(wire, pair[0], pair[1], 1)
		if _, err := p.ParseStatusResponse(MiniMaxH3Video, "h3-task", http.StatusOK, []byte(bad)); err == nil {
			t.Errorf("accepted wrong %s", pair[1])
		}
	}
}
