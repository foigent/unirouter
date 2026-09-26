# Gateway pre-dispatch terminal v2

This profile reports only an authenticated Chat request that ended with `routing.no_route` and HTTP 503 before reservation, provider attempt, media admission or delivery. It has no financial terminal authority. The existing v1 observation schema, profile, writer, 4xx admission semantics and completed-execution Feed are unchanged. A consumer must explicitly subscribe to this separate profile; absence is a capture gap, not a successful request. A v2 capture failure preserves the original HTTP 503 response.
