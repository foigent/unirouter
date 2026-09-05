package modelcapabilitypublication

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/uniroute/unirouter/internal/strictjson"
)

const PlanSchemaV2 = "unirouter.capability-publication-plan.v2"
const MaterializerContractV2 = "unirouter.capability-materializer.v2"

type PublicationHeadV2 struct {
	PublicationID    string
	ProjectionDigest string
}

// ReadPublicationHeadV2 verifies immutable ledger linkage and retained exact
// plan/bundle bytes. Callers supply one repeatable-read database transaction;
// an apply in another transaction cannot splice two projection generations.
// No historical Plan is copied into a new Plan or into a runtime lease.
func ReadPublicationHeadV2(ctx context.Context, q Querier) (PublicationHeadV2, error) {
	rows, err := q.Query(ctx, `SELECT publication_id,COALESCE(predecessor_id,''),kind,
expected_projection_digest,result_projection_digest,plan_digest,plan_bytes,
COALESCE(bundle_digest,''),bundle_bytes,materializer_contract
 ,COALESCE(legacy_through,''),COALESCE(legacy_projection_digest,''),target_identity::text,publisher_identity::text
FROM capability_publications ORDER BY sequence`)
	if err != nil {
		return PublicationHeadV2{}, err
	}
	defer rows.Close()
	var head PublicationHeadV2
	var chainTarget []byte
	for rows.Next() {
		var id, predecessor, kind, before, after, planDigest, bundleDigest, materializer, legacyHead, legacyDigest, target, publisher string
		var plan, bundle []byte
		if err := rows.Scan(&id, &predecessor, &kind, &before, &after, &planDigest, &plan, &bundleDigest, &bundle, &materializer,
			&legacyHead, &legacyDigest, &target, &publisher); err != nil {
			return PublicationHeadV2{}, err
		}
		if predecessor != head.PublicationID || (head.PublicationID == "") != (kind == "legacy_import") ||
			(head.PublicationID != "" && before != head.ProjectionDigest) || validateText("materializer", materializer) != nil {
			return PublicationHeadV2{}, fmt.Errorf("capability publication chain or materializer is inconsistent")
		}
		if kind == "legacy_import" && (!migrationIDPattern.MatchString(legacyHead) || !sha256Pattern.MatchString(legacyDigest) || before != after || bundleDigest != "") {
			return PublicationHeadV2{}, fmt.Errorf("capability import must retain the actual legacy identity without materializing changes")
		}
		canonical, err := strictjson.Canonicalize(plan)
		if err != nil || !bytes.Equal(canonical, plan) || sha256Hex(plan) != planDigest {
			return PublicationHeadV2{}, fmt.Errorf("capability publication retained plan bytes are invalid")
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(plan, &fields); err != nil || fields == nil {
			return PublicationHeadV2{}, fmt.Errorf("capability plan is not an object")
		}
		for key, want := range map[string]string{"schema_version": PlanSchemaV2, "publication_id": id,
			"kind": kind, "legacy_through": legacyHead, "legacy_projection_digest": legacyDigest,
			"projection_schema_version": ProjectionSchemaV2,
			"expected_predecessor":      predecessor, "expected_projection_digest": before,
			"result_projection_digest": after, "materializer_contract_version": materializer, "bundle_digest": bundleDigest} {
			var actual string
			if err := json.Unmarshal(fields[key], &actual); err != nil || actual != want {
				return PublicationHeadV2{}, fmt.Errorf("capability plan and ledger differ at %s", key)
			}
		}
		for key, stored := range map[string]string{"target_identity": target, "publisher_identity": publisher} {
			canonical, err := strictjson.Canonicalize([]byte(stored))
			if err != nil || bytes.Equal(canonical, []byte("{}")) || !bytes.Equal(fields[key], canonical) {
				return PublicationHeadV2{}, fmt.Errorf("capability plan and ledger differ at %s", key)
			}
			if key == "target_identity" {
				var identity TargetIdentity
				if err := decodeStrict(canonical, &identity); err != nil || identity.Environment == "" || identity.DatabaseName == "" || identity.SystemIdentifier == "" {
					return PublicationHeadV2{}, fmt.Errorf("capability target identity is incomplete")
				}
				if chainTarget != nil && !bytes.Equal(chainTarget, canonical) {
					return PublicationHeadV2{}, fmt.Errorf("capability ledger target identity changed")
				}
				chainTarget = canonical
			}
		}
		if bundleDigest != "" {
			canonical, err := strictjson.Canonicalize(bundle)
			if err != nil || !bytes.Equal(canonical, bundle) || sha256Hex(bundle) != bundleDigest {
				return PublicationHeadV2{}, fmt.Errorf("capability publication retained bundle bytes are invalid")
			}
		} else if len(bundle) != 0 || kind != "legacy_import" {
			return PublicationHeadV2{}, fmt.Errorf("capability publication bundle identity is incomplete")
		}
		head = PublicationHeadV2{PublicationID: id, ProjectionDigest: after}
	}
	if err := rows.Err(); err != nil {
		return PublicationHeadV2{}, err
	}
	return head, nil
}

func VerifyPublicationProjectionV2(ctx context.Context, q Querier) error {
	head, err := ReadPublicationHeadV2(ctx, q)
	if err != nil {
		return err
	}
	if head.PublicationID == "" {
		return fmt.Errorf("capability legacy import has not completed")
	}
	return verifyProjectionAtHeadV2(ctx, q, head)
}

func verifyProjectionAtHeadV2(ctx context.Context, q Querier, head PublicationHeadV2) error {
	projection, err := ReadStaticProjectionV2(ctx, q)
	if err != nil {
		return err
	}
	digest, err := projection.Digest()
	if err != nil {
		return err
	}
	if digest != head.ProjectionDigest {
		return fmt.Errorf("real capability projection differs from committed publication head")
	}
	return nil
}
