package sourcefacts

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/uniroute/unirouter/internal/eventconnector"
	"github.com/uniroute/unirouter/internal/executioncontext"
	"github.com/uniroute/unirouter/internal/executionmeasurement"
	"github.com/uniroute/unirouter/internal/testutil"
)

// Real encoders supply two A profiles and three B records sharing one profile.
func groupedPublicationFixture(t *testing.T, s *Service, org, project int64, request string) []sourcePublication {
	t.Helper()
	snapshot := testSnapshot()
	snapshot.Core.OrgID, snapshot.Core.ProjectID = executioncontext.ID(org), executioncontext.ID(project)
	snapshot.Core.RequestID = request
	records := []sourcePublication{}
	for _, d := range s.contexts(org, project, request, "authenticated", snapshot.Core.CapturedAt, snapshot.Core.CapturedAt, &snapshot) {
		p, err := publicationA(d)
		require.NoError(t, err)
		records = append(records, p)
	}
	at, err := time.Parse(time.RFC3339Nano, snapshot.Core.CapturedAt)
	require.NoError(t, err)
	for i, kind := range []string{executionmeasurement.ResponseComplete, executionmeasurement.FirstOutput, executionmeasurement.StreamComplete} {
		d := s.observation(org, project, request, RuntimeProfile, "request", request, []string{"response_measurement", "first_output_measurement", "stream_measurement"}[i], at)
		bounds := measurementBoundaries[kind]
		d.Coverage = "not_observed"
		d.Measurement = &Measurement{Kind: kind, Version: "1", Unit: "nanoseconds", State: "not_observed", StartBoundary: bounds[0], EndBoundary: bounds[1], Resolution: "nanosecond_representation", Source: "process_monotonic_clock", EvidenceRef: request}
		p, err := publicationB(d)
		require.NoError(t, err)
		records = append(records, p)
	}
	return records
}

type groupedPublicationTx struct {
	*testutil.MockTx
	batches    []*pgx.Batch
	retained   []sourcePublication
	shortWrite bool
}

func (tx *groupedPublicationTx) SendBatch(_ context.Context, batch *pgx.Batch) pgx.BatchResults {
	tx.batches = append(tx.batches, batch)
	return &groupedPublicationResults{batch: batch, write: len(tx.batches) == 2, retained: tx.retained, shortWrite: tx.shortWrite}
}

type groupedPublicationResults struct {
	batch             *pgx.Batch
	write, shortWrite bool
	retained          []sourcePublication
	index             int
}

func (b *groupedPublicationResults) Exec() (pgconn.CommandTag, error) {
	q := b.batch.QueuedQueries[b.index]
	b.index++
	n := 1
	if b.write {
		n = len(q.Arguments[5].([]string))
		if b.shortWrite {
			n--
		}
	}
	return pgconn.NewCommandTag(fmt.Sprintf("INSERT 0 %d", n)), nil
}
func (b *groupedPublicationResults) QueryRow() pgx.Row {
	b.index++
	return &testutil.MockRow{ScanFn: func(dest ...any) error {
		switch d := dest[0].(type) {
		case *int64:
			*d = 17
		case *time.Time:
			*d = time.Unix(1, 0).UTC()
		}
		return nil
	}}
}
func (b *groupedPublicationResults) Query() (pgx.Rows, error) {
	b.index++
	rows := &testutil.MockRows{}
	for _, p := range b.retained {
		rows.Rows = append(rows.Rows, func(dest ...any) error {
			*dest[0].(*string), *dest[1].(*string) = p.EventID, p.SourceFactID
			*dest[2].(*int64), _ = strconv.ParseInt(p.Revision, 10, 64)
			*dest[3].(*string) = eventconnector.HashSourceBytes(p.Wire)
			return nil
		})
	}
	return rows, nil
}
func (b *groupedPublicationResults) Close() error { return nil }

func TestGroupedPublicationPlanOffline(t *testing.T) {
	s := &Service{options: Options{SourceInstance: "synthetic-ur"}}
	records := groupedPublicationFixture(t, s, 1, 2, "grouped-request")
	require.Len(t, records, 5)
	for _, mode := range []string{"new", "partial_replay", "all_replay"} {
		t.Run(mode, func(t *testing.T) {
			tx := &groupedPublicationTx{MockTx: &testutil.MockTx{}}
			if mode == "partial_replay" {
				tx.retained = []sourcePublication{records[0], records[2]}
			}
			if mode == "all_replay" {
				tx.retained = records
			}
			input := slices.Clone(records)
			slices.Reverse(input)
			require.NoError(t, s.publishRecordsTx(t.Context(), tx, input))
			require.Equal(t, records[len(records)-1], input[0], "caller slice must not be sorted in place")
			read := tx.batches[0]
			require.Equal(t, 8, read.Len(), "three unique profiles, clock and predecessor snapshot")
			locks := []string{}
			for i := 0; i < 6; i += 2 {
				require.Equal(t, read.QueuedQueries[i].Arguments, read.QueuedQueries[i+1].Arguments)
				locks = append(locks, read.QueuedQueries[i].Arguments[3].(string))
			}
			require.True(t, slices.IsSorted(locks))
			require.Len(t, slices.Compact(slices.Clone(locks)), 3)
			if mode == "all_replay" {
				require.Len(t, tx.batches, 1)
				return
			}
			require.Len(t, tx.batches, 2)
			expected := map[string]sourcePublication{}
			for _, p := range records {
				expected[p.EventID] = p
			}
			for _, p := range tx.retained {
				delete(expected, p.EventID)
			}
			total := 0
			for _, q := range tx.batches[1].QueuedQueries {
				events := q.Arguments[5].([]string)
				require.True(t, slices.IsSorted(events))
				wires, hashes := q.Arguments[9].([][]byte), q.Arguments[10].([]string)
				for i, event := range events {
					p, ok := expected[event]
					require.True(t, ok)
					require.Equal(t, p.Profile, q.Arguments[3])
					require.Equal(t, p.RequestID, q.Arguments[4])
					require.Equal(t, p.Wire, wires[i])
					require.Equal(t, eventconnector.HashSourceBytes(p.Wire), hashes[i])
					require.Equal(t, p.SourceFactID, q.Arguments[6].([]string)[i])
					require.Equal(t, p.Stage, q.Arguments[8].([]string)[i])
					total++
				}
			}
			require.Equal(t, len(expected), total)
			require.Equal(t, map[string]int{"new": 3, "partial_replay": 2}[mode], tx.batches[1].Len())
		})
	}
}

func TestGroupedPublicationRejectsBeforeAllocationOffline(t *testing.T) {
	s := &Service{options: Options{SourceInstance: "synthetic-ur"}}
	for _, mode := range []string{"conflict", "predecessor_missing", "predecessor_hash", "same_batch_predecessor", "scope", "duplicate", "short_write"} {
		t.Run(mode, func(t *testing.T) {
			records := groupedPublicationFixture(t, s, 1, 2, "grouped-request")
			tx := &groupedPublicationTx{MockTx: &testutil.MockTx{}}
			switch mode {
			case "conflict":
				tx.retained = slices.Clone(records[:1])
				records[0].Wire = []byte("different")
			case "predecessor_missing", "predecessor_hash":
				tx.retained = slices.Clone(records[:1])
				records[0].EventID += "-revision2"
				records[0].Revision = "2"
				records[0].PreviousRevision = "1"
				records[0].PreviousHash = "wrong"
				if mode == "predecessor_missing" {
					tx.retained = nil
				}
			case "same_batch_predecessor":
				revision := records[0]
				revision.EventID += "-revision2"
				revision.Revision = "2"
				revision.PreviousRevision = "1"
				revision.PreviousHash = eventconnector.HashSourceBytes(records[0].Wire)
				records = append(records, revision)
			case "scope":
				records[4].ProjectID = "99"
			case "duplicate":
				records = append(records, records[0])
			case "short_write":
				tx.shortWrite = true
			}
			require.Error(t, s.publishRecordsTx(t.Context(), tx, records))
			if mode == "short_write" {
				require.Len(t, tx.batches, 2)
			} else {
				require.LessOrEqual(t, len(tx.batches), 1)
			}
		})
	}
}
