package mediajob

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
)

type memoryS3Object struct {
	body        []byte
	contentType string
	metadata    map[string]string
}

type memoryS3Client struct {
	mu      sync.Mutex
	objects map[string]memoryS3Object
}

func newMemoryS3Client() *memoryS3Client {
	return &memoryS3Client{objects: make(map[string]memoryS3Object)}
}

func (m *memoryS3Client) PutObject(
	_ context.Context,
	input *s3.PutObjectInput,
	_ ...func(*s3.Options),
) (*s3.PutObjectOutput, error) {
	body, err := io.ReadAll(input.Body)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if aws.ToString(input.IfNoneMatch) == "*" {
		if _, exists := m.objects[aws.ToString(input.Key)]; exists {
			return nil, &smithy.GenericAPIError{
				Code: "PreconditionFailed", Message: "already exists",
			}
		}
	}
	m.objects[aws.ToString(input.Key)] = memoryS3Object{
		body:        append([]byte(nil), body...),
		contentType: aws.ToString(input.ContentType),
		metadata:    cloneStringMap(input.Metadata),
	}
	return &s3.PutObjectOutput{}, nil
}

func (m *memoryS3Client) GetObject(
	_ context.Context,
	input *s3.GetObjectInput,
	_ ...func(*s3.Options),
) (*s3.GetObjectOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	object, exists := m.objects[aws.ToString(input.Key)]
	if !exists {
		return nil, &smithy.GenericAPIError{Code: "NoSuchKey", Message: "not found"}
	}
	return &s3.GetObjectOutput{
		Body:          io.NopCloser(bytes.NewReader(object.body)),
		ContentLength: aws.Int64(int64(len(object.body))),
		ContentType:   aws.String(object.contentType),
		Metadata:      cloneStringMap(object.metadata),
	}, nil
}

func (m *memoryS3Client) HeadObject(
	_ context.Context,
	input *s3.HeadObjectInput,
	_ ...func(*s3.Options),
) (*s3.HeadObjectOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	object, exists := m.objects[aws.ToString(input.Key)]
	if !exists {
		return nil, &smithy.GenericAPIError{Code: "NotFound", Message: "not found"}
	}
	return &s3.HeadObjectOutput{
		ContentLength: aws.Int64(int64(len(object.body))),
		ContentType:   aws.String(object.contentType),
		Metadata:      cloneStringMap(object.metadata),
	}, nil
}

func (m *memoryS3Client) DeleteObject(
	_ context.Context,
	input *s3.DeleteObjectInput,
	_ ...func(*s3.Options),
) (*s3.DeleteObjectOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, aws.ToString(input.Key))
	return &s3.DeleteObjectOutput{}, nil
}

func cloneStringMap(input map[string]string) map[string]string {
	result := make(map[string]string, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func TestS3ObjectStoreTenantIsolationRecoveryReadAndPurge(t *testing.T) {
	client := newMemoryS3Client()
	store, err := newS3ObjectStore(client, S3Config{
		Bucket:    "media-fixture",
		Prefix:    "unirouter-media",
		KeySecret: []byte(strings.Repeat("k", 32)),
	})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	now := time.Date(2026, 7, 31, 8, 9, 10, 0, time.UTC)
	store.now = func() time.Time { return now }
	job := imageTestJob(Scope{OrgID: 7, ProjectID: 11}, "mediajob_0123456789abcdef0123456789abcdef")
	image := fixturePNG()
	result := ProviderResult{
		MappedModel: "gpt-image-1", Bytes: image, MediaType: "image/png",
		Outcome: imageOutcome(),
	}
	persisted, err := store.Persist(context.Background(), job, result)
	if err != nil {
		t.Fatalf("persist: %v", err)
	}
	if persisted.PersistedAt != now ||
		persisted.ContentSHA256 != sha256Digest(image) ||
		strings.Contains(persisted.ObjectKey, job.JobID) {
		t.Fatalf("unexpected persisted result: %+v", persisted)
	}
	otherTenant := imageTestJob(
		Scope{OrgID: 8, ProjectID: 11},
		"mediajob_0123456789abcdef0123456789abcdef",
	)
	otherKey, _, err := store.objectKeys(otherTenant, "image/png")
	if err != nil || otherKey == persisted.ObjectKey {
		t.Fatalf("tenant object partition did not change: %s %s err=%v", persisted.ObjectKey, otherKey, err)
	}

	recovered, err := store.Recover(context.Background(), job)
	if err != nil || recovered == nil ||
		recovered.ObjectKey != persisted.ObjectKey ||
		recovered.ContentSHA256 != persisted.ContentSHA256 ||
		recovered.MappedModel != persisted.MappedModel ||
		len(recovered.Outcome.Units) != len(persisted.Outcome.Units) {
		t.Fatalf("recover = %+v err=%v, want %+v", recovered, err, persisted)
	}
	client.mu.Lock()
	object := client.objects[persisted.ObjectKey]
	delete(object.metadata, "recovery-version")
	client.objects[persisted.ObjectKey] = object
	client.mu.Unlock()
	if _, err := store.Recover(context.Background(), job); !errors.Is(err, ErrConflict) {
		t.Fatalf("incomplete recovery metadata error = %v", err)
	}
	client.mu.Lock()
	object = client.objects[persisted.ObjectKey]
	object.metadata["recovery-version"] = ContractVersion + ".object-metadata"
	client.objects[persisted.ObjectKey] = object
	client.mu.Unlock()

	client.mu.Lock()
	receiptObject := client.objects[persisted.ReceiptKey]
	originalReceiptBody := append([]byte(nil), receiptObject.body...)
	receiptObject.body = append(
		[]byte(`{"job_id":"mediajob_ffffffffffffffffffffffffffffffff",`),
		receiptObject.body[1:]...,
	)
	client.objects[persisted.ReceiptKey] = receiptObject
	client.mu.Unlock()
	if _, err := store.Recover(context.Background(), job); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate receipt key error = %v", err)
	}
	client.mu.Lock()
	receiptObject = client.objects[persisted.ReceiptKey]
	receiptObject.body = originalReceiptBody
	client.objects[persisted.ReceiptKey] = receiptObject
	client.mu.Unlock()

	client.mu.Lock()
	delete(client.objects, persisted.ReceiptKey)
	client.mu.Unlock()
	recovered, err = store.Recover(context.Background(), job)
	if err != nil || recovered == nil ||
		recovered.ContentSHA256 != persisted.ContentSHA256 {
		t.Fatalf("metadata-only recovery = %+v err=%v", recovered, err)
	}
	client.mu.Lock()
	_, receiptRebuilt := client.objects[persisted.ReceiptKey]
	client.mu.Unlock()
	if !receiptRebuilt {
		t.Fatal("metadata-only recovery did not rebuild the immutable receipt")
	}
	job.ObjectKey = persisted.ObjectKey
	job.ReceiptKey = persisted.ReceiptKey
	job.Output = &Output{
		ContentSHA256: persisted.ContentSHA256,
		MediaType:     persisted.MediaType, SizeBytes: persisted.SizeBytes,
		PersistedAt: &persisted.PersistedAt,
	}
	job.Lifecycle.Object = ObjectPersisted
	content, err := store.Read(context.Background(), job)
	if err != nil || !bytes.Equal(content.Bytes, image) || content.MediaType != "image/png" {
		t.Fatalf("read = %+v err=%v", content, err)
	}
	if err := store.Delete(context.Background(), job); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if recovered, err := store.Recover(context.Background(), job); err != nil || recovered != nil {
		t.Fatalf("recover after delete = %+v err=%v", recovered, err)
	}
}

func TestS3ObjectStoreRejectsCrossByteMutationAndSanitizesErrors(t *testing.T) {
	client := newMemoryS3Client()
	store, err := newS3ObjectStore(client, S3Config{
		Bucket:    "media-fixture",
		KeySecret: []byte(strings.Repeat("z", 32)),
	})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	job := imageTestJob(Scope{OrgID: 1, ProjectID: 2}, "mediajob_abcdefabcdefabcdefabcdefabcdefab")
	persisted, err := store.Persist(context.Background(), job, ProviderResult{
		MappedModel: "gpt-image-1", Bytes: fixturePNG(),
		MediaType: "image/png", Outcome: imageOutcome(),
	})
	if err != nil {
		t.Fatalf("persist: %v", err)
	}
	job.ObjectKey = persisted.ObjectKey
	job.ReceiptKey = persisted.ReceiptKey
	job.Output = &Output{
		ContentSHA256: persisted.ContentSHA256, MediaType: persisted.MediaType,
		SizeBytes: persisted.SizeBytes,
	}
	job.Lifecycle.Object = ObjectPersisted
	client.mu.Lock()
	object := client.objects[persisted.ObjectKey]
	object.body[len(object.body)-1] ^= 0xff
	client.objects[persisted.ObjectKey] = object
	client.mu.Unlock()
	_, err = store.Read(context.Background(), job)
	if !errors.Is(err, ErrConflict) || strings.Contains(err.Error(), persisted.ObjectKey) {
		t.Fatalf("mutation error = %v", err)
	}

	originalObjectKey := job.ObjectKey
	job.ObjectKey = "unrelated/private/object"
	err = store.Delete(context.Background(), job)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("tampered purge identity error = %v", err)
	}
	client.mu.Lock()
	_, originalStillPresent := client.objects[originalObjectKey]
	client.mu.Unlock()
	if !originalStillPresent {
		t.Fatal("tampered purge identity deleted the durable image")
	}
}

func TestRecoveryMetadataStaysWithinS3UserMetadataBudget(t *testing.T) {
	receipt := recoveryReceipt{
		SchemaVersion: ContractVersion + ".recovery-receipt",
		JobID:         "mediajob_0123456789abcdef0123456789abcdef",
		Scope:         Scope{OrgID: 9223372036854775807, ProjectID: 9223372036854775807},
		ContentSHA256: "sha256:" + strings.Repeat("f", 64),
		PersistedAt:   time.Date(2026, 7, 31, 8, 9, 10, 123456000, time.UTC),
		MappedModel:   strings.Repeat("m", 200),
		Outcome:       imageOutcome(),
	}
	for index := range receipt.Outcome.Units {
		receipt.Outcome.Units[index].Quantity = "9223372036854775807"
	}
	metadata, err := recoveryMetadata(receipt)
	if err != nil {
		t.Fatalf("metadata: %v", err)
	}
	size := 0
	for key, value := range metadata {
		size += len(key) + len(value)
	}
	if size > maxRecoveryMetadataBytes {
		t.Fatalf("metadata size=%d limit=%d", size, maxRecoveryMetadataBytes)
	}
}

func TestRecoveryReceiptRejectsInvalidMediaAndUsagePartition(t *testing.T) {
	job := imageTestJob(
		Scope{OrgID: 7, ProjectID: 11},
		"mediajob_0123456789abcdef0123456789abcdef",
	)
	receipt := recoveryReceipt{
		SchemaVersion: ContractVersion + ".recovery-receipt",
		JobID:         job.JobID,
		Scope:         job.Scope,
		ContentSHA256: "sha256:" + strings.Repeat("f", 64),
		MediaType:     "image/gif",
		SizeBytes:     128,
		PersistedAt:   time.Date(2026, 7, 31, 8, 9, 10, 0, time.UTC),
		MappedModel:   "gpt-image-1",
		Outcome:       imageOutcome(),
	}
	if err := validateRecoveryReceipt(job, receipt); !errors.Is(err, ErrConflict) {
		t.Fatalf("invalid recovery media type error = %v", err)
	}
	receipt.MediaType = "image/png"
	receipt.Outcome.Units[0].Quantity = "99"
	if err := validateRecoveryReceipt(job, receipt); !errors.Is(err, ErrConflict) {
		t.Fatalf("invalid recovery usage partition error = %v", err)
	}
}

func imageTestJob(scope Scope, jobID string) Job {
	return Job{
		ContractVersion: ContractVersion, JobID: jobID, Scope: scope,
		ProfileID: endpointprotocol.ProfileOpenAIImageGPT,
		Lifecycle: Lifecycle{Object: ObjectPending},
	}
}

func imageOutcome() endpointprotocol.Outcome {
	return endpointprotocol.Outcome{
		ContractVersion: endpointprotocol.ContractVersion,
		ProfileID:       endpointprotocol.ProfileOpenAIImageGPT,
		ProviderState:   "completed", MappedModel: "gpt-image-1", OutputCount: 1,
		Units: []endpointprotocol.UnitObservation{
			{
				Class: endpointprotocol.UnitInputTokens, Quantity: "1",
				Authority: "provider_reported", Finality: "final",
				ChargeState: endpointprotocol.ChargeObservedOnly,
			},
			{
				Class: endpointprotocol.UnitInputTextTokens, Quantity: "1",
				Authority: "provider_reported", Finality: "final",
				ChargeState: endpointprotocol.ChargeObservedOnly,
			},
			{
				Class: endpointprotocol.UnitInputImageTokens, Quantity: "0",
				Authority: "provider_reported", Finality: "final",
				ChargeState: endpointprotocol.ChargeObservedOnly,
			},
			{
				Class: endpointprotocol.UnitOutputImageTokens, Quantity: "2",
				Authority: "provider_reported", Finality: "final",
				ChargeState: endpointprotocol.ChargeObservedOnly,
			},
		},
	}
}

func fixturePNG() []byte {
	return []byte{
		0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n',
		0, 0, 0, 0, 'I', 'E', 'N', 'D',
	}
}
