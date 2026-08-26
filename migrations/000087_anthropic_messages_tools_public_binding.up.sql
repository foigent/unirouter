BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE model_operation_execution_support_bindings,
    model_operation_public_execution_contract_extensions,
    model_operation_public_execution_bindings,
    runtime_route_revision_items, schema_migrations
    IN SHARE ROW EXCLUSIVE MODE;

DROP TRIGGER model_operation_execution_support_bindings_immutable
    ON model_operation_execution_support_bindings;

INSERT INTO model_operation_execution_support_bindings (
    operation_id,public_path,protocol_profile_id,protocol_profile_version,
    executor_contract_version,introduced_migration)
VALUES (
    'anthropic.messages.v1','/v1/messages','unirouter.anthropic.messages.text-tools.v3','1.0.0',
    'unirouter.endpoint-profile-messages-tools-executor.v1',
    '000087_anthropic_messages_tools_public_binding'
);

CREATE TRIGGER model_operation_execution_support_bindings_immutable
BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE
ON model_operation_execution_support_bindings
FOR EACH STATEMENT EXECUTE FUNCTION reject_model_operation_execution_support_mutation();

DROP TRIGGER model_operation_public_execution_contract_extensions_immutable
    ON model_operation_public_execution_contract_extensions;

ALTER TABLE model_operation_public_execution_contract_extensions
    DROP CONSTRAINT model_operation_public_execution_contract_extensions_identity_check,
    ADD CONSTRAINT model_operation_public_execution_contract_extensions_identity_check CHECK (
        charge_contract_version='unirouter.provider-final-token-charge.v1'
        AND idempotency_contract_version='unirouter.public-at-most-once.v1'
        AND (
            (
                introduced_migration='000077_public_model_execution_authority'
                AND delivery_contract_version='unirouter.public-nonstream-delivery.v1'
                AND executor_contract_version='unirouter.endpoint-profile-nonstream-executor.v1'
                AND (
                    (operation_id='openai.responses.v1'
                     AND protocol_profile_id='unirouter.openai.responses.text.v1'
                     AND protocol_profile_version='1.0.0')
                    OR
                    (operation_id='anthropic.messages.v1'
                     AND protocol_profile_id='unirouter.anthropic.messages.text.v1'
                     AND protocol_profile_version='1.0.0')
                )
            )
            OR
            (
                introduced_migration='000086_exact_public_execution_bindings'
                AND protocol_profile_version='1.0.0'
                AND (
                    (
                        operation_id='openai.responses.v1'
                        AND protocol_profile_id='unirouter.openai.responses.text.v2'
                        AND executor_contract_version='unirouter.endpoint-profile-responses-dual-executor.v1'
                        AND delivery_contract_version='unirouter.public-responses-delivery.v2'
                    )
                    OR
                    (
                        operation_id='anthropic.messages.v1'
                        AND protocol_profile_id='unirouter.anthropic.messages.text.v2'
                        AND executor_contract_version='unirouter.endpoint-profile-messages-dual-executor.v1'
                        AND delivery_contract_version='unirouter.public-anthropic-messages-delivery.v2'
                    )
                )
            )
            OR
            (
                introduced_migration='000087_anthropic_messages_tools_public_binding'
                AND operation_id='anthropic.messages.v1'
                AND protocol_profile_id='unirouter.anthropic.messages.text-tools.v3'
                AND protocol_profile_version='1.0.0'
                AND executor_contract_version='unirouter.endpoint-profile-messages-tools-executor.v1'
                AND delivery_contract_version='unirouter.public-anthropic-messages-tools-delivery.v1'
            )
        )
    );

INSERT INTO model_operation_public_execution_contract_extensions (
    operation_id,public_path,protocol_profile_id,protocol_profile_version,
    executor_contract_version,delivery_contract_version,
    charge_contract_version,idempotency_contract_version,introduced_migration)
VALUES (
    'anthropic.messages.v1','/v1/messages','unirouter.anthropic.messages.text-tools.v3','1.0.0',
    'unirouter.endpoint-profile-messages-tools-executor.v1',
    'unirouter.public-anthropic-messages-tools-delivery.v1',
    'unirouter.provider-final-token-charge.v1',
    'unirouter.public-at-most-once.v1',
    '000087_anthropic_messages_tools_public_binding'
);

CREATE TRIGGER model_operation_public_execution_contract_extensions_immutable
BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE
ON model_operation_public_execution_contract_extensions
FOR EACH STATEMENT
EXECUTE FUNCTION reject_model_operation_public_execution_contract_mutation();

COMMIT;
