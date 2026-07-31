package mediajob

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/uniroute/unirouter/internal/endpointprotocol"
	"github.com/uniroute/unirouter/internal/eventconnector"
)

const (
	MaxOutputBytes           = endpointprotocol.MaxOpenAIImageBinaryBytes
	maxReceiptBytes          = 64 * 1024
	maxRecoveryMetadataBytes = 1800
)

type s3API interface {
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	HeadObject(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

type S3Config struct {
	Region         string
	Bucket         string
	Endpoint       string
	Prefix         string
	ForcePathStyle bool
	KeySecret      []byte
}

type S3ObjectStore struct {
	client    s3API
	bucket    string
	prefix    string
	keySecret []byte
	now       func() time.Time
}

type recoveryReceipt struct {
	SchemaVersion string                   `json:"schema_version"`
	JobID         string                   `json:"job_id"`
	Scope         Scope                    `json:"scope"`
	ObjectKey     string                   `json:"object_key"`
	ReceiptKey    string                   `json:"receipt_key"`
	ContentSHA256 string                   `json:"content_sha256"`
	MediaType     string                   `json:"media_type"`
	SizeBytes     int64                    `json:"size_bytes"`
	PersistedAt   time.Time                `json:"persisted_at"`
	MappedModel   string                   `json:"mapped_model"`
	Outcome       endpointprotocol.Outcome `json:"outcome"`
}

func NewS3ObjectStore(ctx context.Context, config S3Config) (*S3ObjectStore, error) {
	config.Region = strings.TrimSpace(config.Region)
	config.Bucket = strings.TrimSpace(config.Bucket)
	config.Endpoint = strings.TrimRight(strings.TrimSpace(config.Endpoint), "/")
	config.Prefix = strings.Trim(strings.TrimSpace(config.Prefix), "/")
	if config.Region == "" || config.Bucket == "" || len(config.KeySecret) < 32 {
		return nil, domainError(ErrInvalid, "s3_configuration_invalid")
	}
	options := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(config.Region),
	}
	awsConfig, err := awsconfig.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("mediajob: load S3 configuration: %w", err)
	}
	client := s3.NewFromConfig(awsConfig, func(options *s3.Options) {
		options.UsePathStyle = config.ForcePathStyle
		if config.Endpoint != "" {
			options.BaseEndpoint = aws.String(config.Endpoint)
		}
	})
	return newS3ObjectStore(client, config)
}

func newS3ObjectStore(client s3API, config S3Config) (*S3ObjectStore, error) {
	if client == nil || strings.TrimSpace(config.Bucket) == "" || len(config.KeySecret) < 32 {
		return nil, domainError(ErrInvalid, "s3_configuration_invalid")
	}
	return &S3ObjectStore{
		client: client, bucket: strings.TrimSpace(config.Bucket),
		prefix:    strings.Trim(strings.TrimSpace(config.Prefix), "/"),
		keySecret: append([]byte(nil), config.KeySecret...),
		now:       time.Now,
	}, nil
}

func (s *S3ObjectStore) Persist(
	ctx context.Context,
	job Job,
	result ProviderResult,
) (PersistedResult, error) {
	if err := validateProviderResult(job, result); err != nil {
		return PersistedResult{}, err
	}
	contentSHA := sha256Digest(result.Bytes)
	objectKey, receiptKey, err := s.objectKeys(job, result.MediaType)
	if err != nil {
		return PersistedResult{}, err
	}
	if existing, recoverErr := s.Recover(ctx, job); recoverErr != nil {
		return PersistedResult{}, recoverErr
	} else if existing != nil {
		if existing.ContentSHA256 != contentSHA ||
			existing.MediaType != result.MediaType ||
			existing.SizeBytes != int64(len(result.Bytes)) ||
			existing.MappedModel != result.MappedModel {
			return PersistedResult{}, domainError(ErrConflict, "immutable_object_conflict")
		}
		return *existing, nil
	}

	persistedAt := s.now().UTC()
	receipt := recoveryReceipt{
		SchemaVersion: ContractVersion + ".recovery-receipt",
		JobID:         job.JobID, Scope: job.Scope,
		ObjectKey: objectKey, ReceiptKey: receiptKey,
		ContentSHA256: contentSHA, MediaType: result.MediaType,
		SizeBytes: int64(len(result.Bytes)), PersistedAt: persistedAt,
		MappedModel: result.MappedModel, Outcome: result.Outcome,
	}
	metadata, err := recoveryMetadata(receipt)
	if err != nil {
		return PersistedResult{}, err
	}
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(objectKey),
		Body: bytes.NewReader(result.Bytes), ContentLength: aws.Int64(int64(len(result.Bytes))),
		ContentType: aws.String(result.MediaType), Metadata: metadata,
		ServerSideEncryption: types.ServerSideEncryptionAes256,
		IfNoneMatch:          aws.String("*"),
	})
	if isS3PreconditionFailed(err) {
		existing, recoverErr := s.Recover(ctx, job)
		if recoverErr != nil {
			return PersistedResult{}, recoverErr
		}
		if existing == nil ||
			existing.ContentSHA256 != contentSHA ||
			existing.MediaType != result.MediaType ||
			existing.SizeBytes != int64(len(result.Bytes)) ||
			existing.MappedModel != result.MappedModel {
			return PersistedResult{}, domainError(ErrConflict, "immutable_object_conflict")
		}
		return *existing, nil
	}
	if err != nil {
		return PersistedResult{}, sanitizeObjectError("image_put_failed", err)
	}
	if err := s.writeReceipt(ctx, receipt, metadata); err != nil {
		return PersistedResult{}, err
	}
	return persistedFromReceipt(receipt), nil
}

func recoveryMetadata(receipt recoveryReceipt) (map[string]string, error) {
	outcome, err := json.Marshal(receipt.Outcome)
	if err != nil || len(outcome) == 0 || len(outcome) > 1536 {
		return nil, domainError(ErrInvalid, "recovery_outcome_invalid")
	}
	mappedModel := []byte(receipt.MappedModel)
	if len(mappedModel) == 0 || len(mappedModel) > 200 {
		return nil, domainError(ErrInvalid, "recovery_model_invalid")
	}
	metadata := map[string]string{
		"job-id":           receipt.JobID,
		"org-id":           strconv.FormatInt(receipt.Scope.OrgID, 10),
		"project-id":       strconv.FormatInt(receipt.Scope.ProjectID, 10),
		"content-sha256":   strings.TrimPrefix(receipt.ContentSHA256, "sha256:"),
		"recovery-version": ContractVersion + ".object-metadata",
		"persisted-at":     receipt.PersistedAt.UTC().Format(time.RFC3339Nano),
		"mapped-model-b64": base64.RawURLEncoding.EncodeToString(mappedModel),
		"outcome-b64":      base64.RawURLEncoding.EncodeToString(outcome),
	}
	size := 0
	for key, value := range metadata {
		size += len(key) + len(value)
	}
	if size > maxRecoveryMetadataBytes {
		return nil, domainError(ErrInvalid, "recovery_metadata_too_large")
	}
	return metadata, nil
}

func (s *S3ObjectStore) writeReceipt(
	ctx context.Context,
	receipt recoveryReceipt,
	metadata map[string]string,
) error {
	receiptBody, err := json.Marshal(receipt)
	if err != nil {
		return domainError(ErrUnavailable, "receipt_encoding_failed")
	}
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(receipt.ReceiptKey),
		Body: bytes.NewReader(receiptBody), ContentLength: aws.Int64(int64(len(receiptBody))),
		ContentType: aws.String("application/json"), Metadata: metadata,
		ServerSideEncryption: types.ServerSideEncryptionAes256,
		IfNoneMatch:          aws.String("*"),
	})
	if isS3PreconditionFailed(err) {
		existing, readErr := s.readReceipt(ctx, receipt.JobID, receipt.Scope)
		if readErr != nil {
			return readErr
		}
		if existing == nil || !recoveryReceiptsEqual(*existing, receipt) {
			return domainError(ErrConflict, "immutable_receipt_conflict")
		}
		return nil
	}
	if err != nil {
		return sanitizeObjectError("receipt_put_failed", err)
	}
	return nil
}

func (s *S3ObjectStore) Recover(
	ctx context.Context,
	job Job,
) (*PersistedResult, error) {
	receipt, err := s.readReceipt(ctx, job.JobID, job.Scope)
	if err != nil {
		return nil, err
	}
	if receipt == nil {
		receipt, err = s.recoverImageMetadata(ctx, job)
		if err != nil || receipt == nil {
			return nil, err
		}
		metadata, metadataErr := recoveryMetadata(*receipt)
		if metadataErr != nil {
			return nil, metadataErr
		}
		if err := s.writeReceipt(ctx, *receipt, metadata); err != nil {
			return nil, err
		}
	}
	if err := validateRecoveryReceipt(job, *receipt); err != nil {
		return nil, err
	}
	if err := s.validateReceiptObjectKey(job, *receipt); err != nil {
		return nil, err
	}
	head, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(receipt.ObjectKey),
	})
	if err != nil {
		return nil, sanitizeObjectError("image_head_failed", err)
	}
	if err := validateRecoveryHead(*receipt, head); err != nil {
		return nil, err
	}
	result := persistedFromReceipt(*receipt)
	return &result, nil
}

func (s *S3ObjectStore) readReceipt(
	ctx context.Context,
	jobID string,
	scope Scope,
) (*recoveryReceipt, error) {
	job := Job{JobID: jobID, Scope: scope}
	_, receiptKey, err := s.objectKeys(job, "image/png")
	if err != nil {
		return nil, err
	}
	output, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(receiptKey),
	})
	if isS3NotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, sanitizeObjectError("receipt_get_failed", err)
	}
	defer output.Body.Close()
	body, err := io.ReadAll(io.LimitReader(output.Body, maxReceiptBytes+1))
	if err != nil || len(body) == 0 || len(body) > maxReceiptBytes {
		return nil, domainError(ErrUnavailable, "receipt_read_failed")
	}
	if strictErr := eventconnector.ValidateStrictJSON(
		body,
		eventconnector.StrictJSONOptions{RejectNull: true},
	); strictErr != nil {
		return nil, domainError(ErrConflict, "receipt_invalid")
	}
	var receipt recoveryReceipt
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&receipt); err != nil {
		return nil, domainError(ErrConflict, "receipt_invalid")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, domainError(ErrConflict, "receipt_invalid")
	}
	if receipt.JobID != jobID || receipt.Scope != scope ||
		receipt.ReceiptKey != receiptKey {
		return nil, domainError(ErrConflict, "receipt_identity_conflict")
	}
	return &receipt, nil
}

func (s *S3ObjectStore) recoverImageMetadata(
	ctx context.Context,
	job Job,
) (*recoveryReceipt, error) {
	var recovered *recoveryReceipt
	for _, mediaType := range []string{"image/png", "image/jpeg", "image/webp"} {
		objectKey, receiptKey, err := s.objectKeys(job, mediaType)
		if err != nil {
			return nil, err
		}
		head, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
			Bucket: aws.String(s.bucket), Key: aws.String(objectKey),
		})
		if isS3NotFound(err) {
			continue
		}
		if err != nil {
			return nil, sanitizeObjectError("image_head_failed", err)
		}
		if recovered != nil {
			return nil, domainError(ErrConflict, "multiple_job_objects_found")
		}
		receipt, err := receiptFromObjectMetadata(
			job, objectKey, receiptKey, mediaType, head,
		)
		if err != nil {
			return nil, err
		}
		recovered = &receipt
	}
	return recovered, nil
}

func receiptFromObjectMetadata(
	job Job,
	objectKey string,
	receiptKey string,
	mediaType string,
	head *s3.HeadObjectOutput,
) (recoveryReceipt, error) {
	if head == nil || head.ContentLength == nil || head.ContentType == nil ||
		*head.ContentType != mediaType ||
		head.Metadata["recovery-version"] != ContractVersion+".object-metadata" ||
		head.Metadata["job-id"] != job.JobID ||
		head.Metadata["org-id"] != strconv.FormatInt(job.Scope.OrgID, 10) ||
		head.Metadata["project-id"] != strconv.FormatInt(job.Scope.ProjectID, 10) {
		return recoveryReceipt{}, domainError(ErrConflict, "stored_object_metadata_conflict")
	}
	persistedAt, err := time.Parse(time.RFC3339Nano, head.Metadata["persisted-at"])
	if err != nil {
		return recoveryReceipt{}, domainError(ErrConflict, "stored_object_metadata_conflict")
	}
	mappedModel, err := base64.RawURLEncoding.DecodeString(head.Metadata["mapped-model-b64"])
	if err != nil || len(mappedModel) == 0 || len(mappedModel) > 200 {
		return recoveryReceipt{}, domainError(ErrConflict, "stored_object_metadata_conflict")
	}
	outcomeBody, err := base64.RawURLEncoding.DecodeString(head.Metadata["outcome-b64"])
	if err != nil || len(outcomeBody) == 0 || len(outcomeBody) > 1536 {
		return recoveryReceipt{}, domainError(ErrConflict, "stored_object_metadata_conflict")
	}
	if strictErr := eventconnector.ValidateStrictJSON(
		outcomeBody,
		eventconnector.StrictJSONOptions{RejectNull: true},
	); strictErr != nil {
		return recoveryReceipt{}, domainError(ErrConflict, "stored_object_metadata_conflict")
	}
	var outcome endpointprotocol.Outcome
	decoder := json.NewDecoder(bytes.NewReader(outcomeBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&outcome); err != nil {
		return recoveryReceipt{}, domainError(ErrConflict, "stored_object_metadata_conflict")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return recoveryReceipt{}, domainError(ErrConflict, "stored_object_metadata_conflict")
	}
	return recoveryReceipt{
		SchemaVersion: ContractVersion + ".recovery-receipt",
		JobID:         job.JobID, Scope: job.Scope,
		ObjectKey: objectKey, ReceiptKey: receiptKey,
		ContentSHA256: "sha256:" + head.Metadata["content-sha256"],
		MediaType:     mediaType, SizeBytes: *head.ContentLength,
		PersistedAt: persistedAt.UTC(), MappedModel: string(mappedModel),
		Outcome: outcome,
	}, nil
}

func validateRecoveryReceipt(job Job, receipt recoveryReceipt) error {
	if _, err := imageExtension(receipt.MediaType); err != nil {
		return domainError(ErrConflict, "receipt_identity_conflict")
	}
	if receipt.SchemaVersion != ContractVersion+".recovery-receipt" ||
		receipt.JobID != job.JobID || receipt.Scope != job.Scope ||
		receipt.SizeBytes <= 0 || receipt.SizeBytes > MaxOutputBytes ||
		receipt.PersistedAt.IsZero() ||
		!validSHA256(receipt.ContentSHA256) ||
		receipt.MappedModel == "" || len(receipt.MappedModel) > 200 ||
		containsControl(receipt.MappedModel) ||
		validateImageOutcome(job.ProfileID, receipt.MappedModel, receipt.Outcome) != nil {
		return domainError(ErrConflict, "receipt_identity_conflict")
	}
	return nil
}

func recoveryReceiptsEqual(left, right recoveryReceipt) bool {
	leftBody, leftErr := json.Marshal(left)
	rightBody, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftBody, rightBody)
}

func validateRecoveryHead(
	receipt recoveryReceipt,
	head *s3.HeadObjectOutput,
) error {
	recovered, err := receiptFromObjectMetadata(
		Job{JobID: receipt.JobID, Scope: receipt.Scope},
		receipt.ObjectKey,
		receipt.ReceiptKey,
		receipt.MediaType,
		head,
	)
	if err != nil || !recoveryReceiptsEqual(receipt, recovered) {
		return domainError(ErrConflict, "stored_object_metadata_conflict")
	}
	return nil
}

func (s *S3ObjectStore) validateReceiptObjectKey(
	job Job,
	receipt recoveryReceipt,
) error {
	expectedObjectKey, _, err := s.objectKeys(job, receipt.MediaType)
	if err != nil || receipt.ObjectKey != expectedObjectKey {
		return domainError(ErrConflict, "receipt_object_conflict")
	}
	return nil
}

func (s *S3ObjectStore) Read(
	ctx context.Context,
	job Job,
) (OutputContent, error) {
	if job.Output == nil || job.ObjectKey == "" ||
		job.Lifecycle.Object != ObjectPersisted {
		return OutputContent{}, domainError(ErrConflict, "output_not_available")
	}
	expectedKey, _, err := s.objectKeys(job, job.Output.MediaType)
	if err != nil || expectedKey != job.ObjectKey {
		return OutputContent{}, domainError(ErrConflict, "stored_object_identity_conflict")
	}
	output, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(job.ObjectKey),
	})
	if err != nil {
		return OutputContent{}, sanitizeObjectError("image_get_failed", err)
	}
	defer output.Body.Close()
	body, err := io.ReadAll(io.LimitReader(output.Body, MaxOutputBytes+1))
	if err != nil || len(body) == 0 || len(body) > MaxOutputBytes {
		return OutputContent{}, domainError(ErrUnavailable, "image_read_failed")
	}
	if int64(len(body)) != job.Output.SizeBytes ||
		sha256Digest(body) != job.Output.ContentSHA256 ||
		detectImageMediaType(body) != job.Output.MediaType {
		return OutputContent{}, domainError(ErrConflict, "stored_object_bytes_conflict")
	}
	return OutputContent{MediaType: job.Output.MediaType, Bytes: body}, nil
}

func (s *S3ObjectStore) Delete(ctx context.Context, job Job) error {
	if job.Output == nil || job.ObjectKey == "" || job.ReceiptKey == "" {
		return domainError(ErrConflict, "purge_object_identity_missing")
	}
	expectedObjectKey, expectedReceiptKey, err := s.objectKeys(
		job,
		job.Output.MediaType,
	)
	if err != nil ||
		job.ObjectKey != expectedObjectKey ||
		job.ReceiptKey != expectedReceiptKey {
		return domainError(ErrConflict, "purge_object_identity_conflict")
	}
	for _, key := range []string{job.ReceiptKey, job.ObjectKey} {
		if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(s.bucket), Key: aws.String(key),
		}); err != nil {
			return sanitizeObjectError("object_delete_failed", err)
		}
	}
	return nil
}

func (s *S3ObjectStore) objectKeys(job Job, mediaType string) (string, string, error) {
	if !job.Scope.Valid() || !ValidJobID(job.JobID) {
		return "", "", domainError(ErrInvalid, "object_identity_invalid")
	}
	extension, err := imageExtension(mediaType)
	if err != nil {
		// Recover does not know the media type before reading the receipt. The
		// receipt key is extension-independent.
		extension = ".png"
	}
	mac := hmac.New(sha256.New, s.keySecret)
	_, _ = mac.Write([]byte("unirouter.async-media.object-key.v1\x00"))
	_, _ = mac.Write([]byte(strconv.FormatInt(job.Scope.OrgID, 10)))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(strconv.FormatInt(job.Scope.ProjectID, 10)))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(job.JobID))
	partition := hex.EncodeToString(mac.Sum(nil))
	base := path.Join(
		s.prefix, "v1",
		"org-"+strconv.FormatInt(job.Scope.OrgID, 10),
		"project-"+strconv.FormatInt(job.Scope.ProjectID, 10),
		partition,
	)
	return path.Join(base, "result"+extension), path.Join(base, "receipt.json"), nil
}

func validateProviderResult(job Job, result ProviderResult) error {
	if !job.Scope.Valid() || !ValidJobID(job.JobID) ||
		len(result.Bytes) == 0 || len(result.Bytes) > MaxOutputBytes ||
		result.MediaType != detectImageMediaType(result.Bytes) ||
		result.MappedModel == "" ||
		validateImageOutcome(job.ProfileID, result.MappedModel, result.Outcome) != nil {
		return domainError(ErrInvalid, "provider_result_invalid")
	}
	return nil
}

func validateImageOutcome(
	profileID string,
	mappedModel string,
	outcome endpointprotocol.Outcome,
) error {
	if profileID != endpointprotocol.ProfileOpenAIImageGPT ||
		mappedModel == "" || len(mappedModel) > 200 ||
		containsControl(mappedModel) ||
		outcome.ContractVersion != endpointprotocol.ContractVersion ||
		outcome.ProfileID != profileID ||
		outcome.ProviderState != "completed" ||
		outcome.MappedModel != mappedModel ||
		outcome.OutputCount != 1 ||
		len(outcome.Units) != 4 {
		return domainError(ErrInvalid, "image_outcome_invalid")
	}
	expected := map[endpointprotocol.UnitClass]bool{
		endpointprotocol.UnitInputTokens:       false,
		endpointprotocol.UnitInputTextTokens:   false,
		endpointprotocol.UnitInputImageTokens:  false,
		endpointprotocol.UnitOutputImageTokens: false,
	}
	quantities := make(map[endpointprotocol.UnitClass]int64, len(expected))
	for _, unit := range outcome.Units {
		seen, exists := expected[unit.Class]
		if !exists || seen ||
			unit.Authority != "provider_reported" ||
			unit.Finality != "final" ||
			unit.ChargeState != endpointprotocol.ChargeObservedOnly {
			return domainError(ErrInvalid, "image_usage_units_invalid")
		}
		if quantity, err := strconv.ParseInt(unit.Quantity, 10, 64); err != nil ||
			quantity < 0 || strconv.FormatInt(quantity, 10) != unit.Quantity {
			return domainError(ErrInvalid, "image_usage_quantity_invalid")
		} else {
			quantities[unit.Class] = quantity
		}
		expected[unit.Class] = true
	}
	textTokens := quantities[endpointprotocol.UnitInputTextTokens]
	imageTokens := quantities[endpointprotocol.UnitInputImageTokens]
	if textTokens > 9_223_372_036_854_775_807-imageTokens ||
		quantities[endpointprotocol.UnitInputTokens] != textTokens+imageTokens {
		return domainError(ErrInvalid, "image_usage_partition_invalid")
	}
	return nil
}

func validSHA256(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil
}

func persistedFromReceipt(receipt recoveryReceipt) PersistedResult {
	return PersistedResult{
		ObjectKey: receipt.ObjectKey, ReceiptKey: receipt.ReceiptKey,
		ContentSHA256: receipt.ContentSHA256, MediaType: receipt.MediaType,
		SizeBytes: receipt.SizeBytes, PersistedAt: receipt.PersistedAt.UTC(),
		Outcome: receipt.Outcome, MappedModel: receipt.MappedModel,
	}
}

func imageExtension(mediaType string) (string, error) {
	switch mediaType {
	case "image/png":
		return ".png", nil
	case "image/jpeg":
		return ".jpg", nil
	case "image/webp":
		return ".webp", nil
	default:
		return "", domainError(ErrInvalid, "media_type_invalid")
	}
}

func detectImageMediaType(body []byte) string {
	if len(body) < 12 {
		return ""
	}
	switch {
	case bytes.HasPrefix(body, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}):
		return "image/png"
	case bytes.HasPrefix(body, []byte{0xff, 0xd8, 0xff}):
		return "image/jpeg"
	case bytes.Equal(body[:4], []byte("RIFF")) && bytes.Equal(body[8:12], []byte("WEBP")):
		return "image/webp"
	default:
		detected := http.DetectContentType(body)
		if detected == "image/png" || detected == "image/jpeg" || detected == "image/webp" {
			return detected
		}
		return ""
	}
}

func sha256Digest(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func sanitizeObjectError(code string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return domainError(ErrUnavailable, code)
}

func isS3NotFound(err error) bool {
	if err == nil {
		return false
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NoSuchKey", "NotFound", "NoSuchObject":
			return true
		}
	}
	return false
}

func isS3PreconditionFailed(err error) bool {
	if err == nil {
		return false
	}
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) &&
		(apiErr.ErrorCode() == "PreconditionFailed" ||
			apiErr.ErrorCode() == "ConditionalRequestConflict")
}
