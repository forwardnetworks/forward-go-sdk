package forward

import (
	"context"
	"testing"
)

// A file of a predicted snapshot comes back with what the prediction changed. The file is named "<device>,<file>" in the path (the comma
// stays), the range is "start-end", and a malformed range or a missing part never reaches the wire.
func TestSnapshotFileExcerptsReadsPredictedFileInfo(t *testing.T) {
	t.Parallel()
	var seen []predictSeen
	snapshots := newTestClient(t, predictRecorder(t, 200, `{"lineCount":120,"excerpts":[{"start":0,"end":1,"lines":["a","b"]}],"predictedFileInfo":{"numModifiedLines":3,"changeSetId":"cs1","baseSnapshotId":"s1","commitId":"c1"}}`, &seen).URL).Snapshots
	ctx := context.Background()

	got, _, err := snapshots.FileExcerpts(ctx, "p9", "fw 1", "configuration.txt", []FileLines{{Start: 0, End: 40}})
	if err != nil || got.LineCount != 120 || len(got.Excerpts) != 1 || got.Excerpts[0].Lines[1] != "b" || got.PredictedFileInfo == nil || got.PredictedFileInfo.NumModifiedLines != 3 || got.PredictedFileInfo.CommitID != "c1" {
		t.Fatalf("got = %+v, %v", got, err)
	}
	if seen[0].method != "GET" || seen[0].uri != "/api/snapshots/p9/files/fw%201,configuration.txt?lines=0-40" {
		t.Fatalf("request = %+v", seen[0])
	}
	// Several ranges go in one comma-separated value, sorted; overlapping ones are refused.
	if _, _, err := snapshots.FileExcerpts(ctx, "p9", "fw1", "configuration.txt", []FileLines{{Start: 60, End: 80}, {Start: 7, End: 45}}); err != nil {
		t.Fatal(err)
	}
	if got := seen[len(seen)-1].uri; got != "/api/snapshots/p9/files/fw1,configuration.txt?lines=7-45%2C60-80" {
		t.Errorf("two-range request = %s", got)
	}
	seen = seen[:len(seen)-1]
	for name, err := range map[string]error{
		"overlap": func() error {
			_, _, err := snapshots.FileExcerpts(ctx, "p9", "d", "f", []FileLines{{Start: 0, End: 5}, {Start: 5, End: 9}})
			return err
		}(),
		"no ranges": func() error { _, _, err := snapshots.FileExcerpts(ctx, "p9", "d", "f", nil); return err }(),
		"reversed": func() error {
			_, _, err := snapshots.FileExcerpts(ctx, "p9", "d", "f", []FileLines{{Start: 5, End: 1}})
			return err
		}(),
		"negative": func() error {
			_, _, err := snapshots.FileExcerpts(ctx, "p9", "d", "f", []FileLines{{Start: -1, End: 1}})
			return err
		}(),
		"no device": func() error {
			_, _, err := snapshots.FileExcerpts(ctx, "p9", " ", "f", []FileLines{{Start: 0, End: 1}})
			return err
		}(),
		"no snapshot": func() error {
			_, _, err := snapshots.FileExcerpts(ctx, "", "d", "f", []FileLines{{Start: 0, End: 1}})
			return err
		}(),
	} {
		if err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if len(seen) != 1 {
		t.Fatalf("a refused call reached the wire: %+v", seen)
	}
}
