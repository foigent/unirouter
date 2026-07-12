package gateway

// Named call-log INSERT positions keep route/evidence assertions readable as
// the durable evidence schema grows. The production statement is intentionally
// explicit and ordered; tests should refer to semantic names, not magic numbers.
const (
	callLogInsertArgCount        = 50
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
	callLogArgCacheUsageSchema   = 23
	callLogArgCacheUsageStatus   = 24
	callLogArgPricingVersion     = 25
	callLogArgEstimatedTokens    = 34
	callLogArgUsageConfidence    = 35
	callLogArgTraceID            = 36
	callLogArgSessionID          = 37
	callLogArgSchemaVersion      = 38
	callLogArgCaptureMode        = 39
	callLogArgUpstreamRequestID  = 41
	callLogArgWorkflowID         = 42
	callLogArgAgentID            = 43
	callLogArgAttemptID          = 44
	callLogArgActorType          = 45
	callLogArgActorRef           = 46
	callLogArgOnBehalfOf         = 47
	callLogArgPayloadCaptureMode = 48
	callLogArgSourceEventHash    = 49
)
