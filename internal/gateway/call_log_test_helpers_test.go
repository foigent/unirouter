package gateway

// Named call-log INSERT positions keep route/evidence assertions readable as
// the durable evidence schema grows. The production statement is intentionally
// explicit and ordered; tests should refer to semantic names, not magic numbers.
const (
	callLogInsertArgCount        = 55
	callLogArgProjectID          = 4
	callLogArgOwnerUserID        = 6
	callLogArgKeyType            = 7
	callLogArgProvider           = 10
	callLogArgChannel            = 11
	callLogArgPromptTokens       = 12
	callLogArgUncachedTokens     = 13
	callLogArgCacheReadTokens    = 14
	callLogArgCacheCreateTokens  = 15
	callLogArgCompletionTokens   = 16
	callLogArgTotalTokens        = 17
	callLogArgCostCents          = 18
	callLogArgStatusCode         = 19
	callLogArgLatencyMS          = 20
	callLogArgUsageSource        = 22
	callLogArgUsageAuthority     = 23
	callLogArgUsagePurpose       = 24
	callLogArgUsageFinality      = 25
	callLogArgContractVersion    = 26
	callLogArgEstimateVersion    = 27
	callLogArgCacheUsageSchema   = 28
	callLogArgCacheUsageStatus   = 29
	callLogArgPricingVersion     = 30
	callLogArgEstimatedTokens    = 39
	callLogArgUsageConfidence    = 40
	callLogArgTraceID            = 41
	callLogArgSessionID          = 42
	callLogArgSchemaVersion      = 43
	callLogArgCaptureMode        = 44
	callLogArgUpstreamRequestID  = 46
	callLogArgWorkflowID         = 47
	callLogArgAgentID            = 48
	callLogArgAttemptID          = 49
	callLogArgActorType          = 50
	callLogArgActorRef           = 51
	callLogArgOnBehalfOf         = 52
	callLogArgPayloadCaptureMode = 53
	callLogArgSourceEventHash    = 54
)
