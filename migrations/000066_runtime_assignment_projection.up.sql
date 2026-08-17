BEGIN;

-- Canonical read projection for the relationship between a runtime slot,
-- its retained release and the channel overrides carried by that release.
-- Runtime mutation remains owned by runtimechange; admin surfaces must consume
-- this projection instead of reimplementing JSON joins with subtly different
-- stage or admission predicates.
CREATE VIEW runtime_channel_assignments_v1 AS
SELECT slot.model_name,
       'stable'::TEXT AS assignment_kind,
       release.id AS release_id,
       release.release_ref,
       release.stage AS release_stage,
       release.version AS release_version,
       slot.slot_version,
       slot.canary_basis_points,
       (override_item.value->>'channel_id')::BIGINT AS channel_id,
       COALESCE((override_item.value->>'expected_routing_epoch')::BIGINT, 0) AS expected_routing_epoch,
       COALESCE(override_item.value->>'admission', '') AS admission,
       override_item.value AS override_payload
  FROM runtime_change_slots slot
  JOIN runtime_change_releases release ON release.id=slot.stable_release_id
  LEFT JOIN LATERAL jsonb_array_elements(COALESCE(release.candidate->'overrides', '[]'::jsonb)) AS override_item(value) ON TRUE
UNION ALL
SELECT slot.model_name,
       'canary'::TEXT AS assignment_kind,
       release.id AS release_id,
       release.release_ref,
       release.stage AS release_stage,
       release.version AS release_version,
       slot.slot_version,
       slot.canary_basis_points,
       (override_item.value->>'channel_id')::BIGINT AS channel_id,
       COALESCE((override_item.value->>'expected_routing_epoch')::BIGINT, 0) AS expected_routing_epoch,
       COALESCE(override_item.value->>'admission', '') AS admission,
       override_item.value AS override_payload
  FROM runtime_change_slots slot
  JOIN runtime_change_releases release ON release.id=slot.canary_release_id
  LEFT JOIN LATERAL jsonb_array_elements(COALESCE(release.candidate->'overrides', '[]'::jsonb)) AS override_item(value) ON TRUE;

COMMENT ON VIEW runtime_channel_assignments_v1 IS
    'Read-only canonical projection of stable/canary runtime release channel assignments.';

COMMIT;
