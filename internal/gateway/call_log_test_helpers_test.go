package gateway

// Named call-log INSERT positions keep route/evidence assertions readable as
// the durable evidence schema grows. The production statement is intentionally
// explicit and ordered; tests should refer to semantic names, not magic numbers.
const (
	callLogInsertArgCount        = 73
	callLogArgProjectID          = 4
	callLogArgOwnerUserID        = 6
	callLogArgKeyType            = 7
	callLogArgProvider           = 10
	callLogArgChannel            = 11
	callLogArgChannelID          = 12
	callLogArgPromptTokens       = 13
	callLogArgUncachedTokens     = 14
	callLogArgCacheReadTokens    = 15
	callLogArgCacheCreateTokens  = 16
	callLogArgCompletionTokens   = 17
	callLogArgTotalTokens        = 18
	callLogArgCostCents          = 19
	callLogArgStatusCode         = 20
	callLogArgLatencyMS          = 21
	callLogArgUsageSource        = 23
	callLogArgUsageAuthority     = 24
	callLogArgUsagePurpose       = 25
	callLogArgUsageFinality      = 26
	callLogArgContractVersion    = 27
	callLogArgEstimateVersion    = 28
	callLogArgCacheUsageSchema   = 29
	callLogArgCacheUsageStatus   = 30
	callLogArgPricingVersion     = 31
	callLogArgEstimatedTokens    = 40
	callLogArgUsageConfidence    = 41
	callLogArgTraceID            = 42
	callLogArgSessionID          = 43
	callLogArgSchemaVersion      = 44
	callLogArgCaptureMode        = 45
	callLogArgUpstreamRequestID  = 47
	callLogArgWorkflowID         = 48
	callLogArgAgentID            = 49
	callLogArgAttemptID          = 50
	callLogArgActorType          = 51
	callLogArgActorRef           = 52
	callLogArgOnBehalfOf         = 53
	callLogArgPayloadCaptureMode = 54
	callLogArgSourceEventHash    = 55
	callLogArgOutboxEventKey     = 56
	callLogArgOccurredAt         = 57
	callLogArgRouteSource        = 58
	callLogArgRouteVersion       = 59
	callLogArgRouteAuthority     = 60
	callLogArgRouteEvidenceHash  = 61
	callLogArgRoutePublicModel   = 62
	callLogArgRouteDeployment    = 63
	callLogArgRouteConnection    = 64
	callLogArgRouteCredential    = 65
	callLogArgRouteOperation     = 66
	callLogArgRouteRelease       = 67
	callLogArgRouteSnapshot      = 68
	callLogArgRouteBinding       = 69
	callLogArgDeliveryContract   = 70
	callLogArgChargeContract     = 71
	callLogArgIdempotency        = 72
	traceInsertArgCount          = 57
)
