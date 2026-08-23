package gateway

// Named call-log INSERT positions keep route/evidence assertions readable as
// the durable evidence schema grows. The production statement is intentionally
// explicit and ordered; tests should refer to semantic names, not magic numbers.
const (
	callLogInsertArgCount        = 76
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
	callLogArgEffectiveUnitRates = 58
	callLogArgMeterUsage         = 59
	callLogArgNonTokenCost       = 60
	callLogArgRouteSource        = 61
	callLogArgRouteVersion       = 62
	callLogArgRouteAuthority     = 63
	callLogArgRouteEvidenceHash  = 64
	callLogArgRoutePublicModel   = 65
	callLogArgRouteDeployment    = 66
	callLogArgRouteConnection    = 67
	callLogArgRouteCredential    = 68
	callLogArgRouteOperation     = 69
	callLogArgRouteRelease       = 70
	callLogArgRouteSnapshot      = 71
	callLogArgRouteBinding       = 72
	callLogArgDeliveryContract   = 73
	callLogArgChargeContract     = 74
	callLogArgIdempotency        = 75
	traceInsertArgCount          = 57
)
