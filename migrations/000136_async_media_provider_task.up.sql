BEGIN;

SELECT pg_advisory_xact_lock(86420517831702491);

LOCK TABLE runtime_generations, runtime_ingress_changes IN SHARE ROW EXCLUSIVE MODE NOWAIT;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM runtime_generations WHERE state <> 'stopped' OR ingress_state <> 'absent')
       OR EXISTS (SELECT 1 FROM runtime_ingress_changes WHERE status NOT IN ('applied', 'cancelled'))
       OR EXISTS (
           SELECT 1 FROM runtime_ingress_changes c
           WHERE c.status='cancelled' AND NOT EXISTS (
               SELECT 1 FROM runtime_ingress_changes n
               WHERE n.predecessor_id=c.change_id AND n.change_id=c.change_id || '-withdrawn'
                 AND n.status='applied' AND n.membership_after='[]'::jsonb
           )
       ) OR EXISTS (
           SELECT 1 FROM runtime_ingress_changes c
           WHERE NOT EXISTS (SELECT 1 FROM runtime_ingress_changes n WHERE n.predecessor_id=c.change_id)
             AND c.membership_after <> '[]'::jsonb
       ) THEN
        RAISE EXCEPTION 'M136 requires stopped writers and completed empty ingress';
    END IF;
    IF (SELECT max(version) FROM schema_migrations) IS DISTINCT FROM '000135_canonical_media_job_authority' THEN
        RAISE EXCEPTION 'M136 requires exact M135 predecessor';
    END IF;
END $$;

LOCK TABLE async_media_jobs, async_media_charge_facts, model_operation_execution_support_bindings,
    model_operation_public_execution_contract_extensions, schema_migrations IN SHARE ROW EXCLUSIVE MODE NOWAIT;

DROP TRIGGER model_operation_execution_support_bindings_immutable ON model_operation_execution_support_bindings;
DROP TRIGGER model_operation_public_execution_contract_extensions_immutable ON model_operation_public_execution_contract_extensions;


ALTER TABLE model_operation_public_execution_contract_extensions
    DROP CONSTRAINT model_operation_public_execution_contract_extensions_identity_check,
    ADD CONSTRAINT model_operation_public_execution_contract_extensions_identity_check CHECK (
        (introduced_migration='000136_async_media_provider_task'
            AND operation_id='unirouter.media_jobs.v1' AND public_path='/v1/media/jobs'
            AND protocol_profile_version='1.0.0'
            AND (protocol_profile_id,executor_contract_version) IN (
                ('unirouter.dashscope.video.wan-3.v1','unirouter.endpoint-profile-async-video-job-executor.v1'),
                ('unirouter.minimax.video.h3.v1','unirouter.endpoint-profile-async-video-job-executor.v1'),
                ('unirouter.glm.video.cogvideox-3.v1','unirouter.endpoint-profile-async-video-job-executor.v1'),
                ('unirouter.glm.images.glm-image.v1','unirouter.endpoint-profile-glm-image-job-executor.v1'),
                ('unirouter.deepseek.tools.ocr.v1','unirouter.endpoint-profile-ocr-job-executor.v1'),
                ('unirouter.glm.ocr.layout.v1','unirouter.endpoint-profile-ocr-job-executor.v1'),
                ('unirouter.deepseek.tools.asr.v1','unirouter.endpoint-profile-speech-job-executor.v1'),
                ('unirouter.deepseek.tools.tts.v1','unirouter.endpoint-profile-speech-job-executor.v1'),
                ('unirouter.minimax.speech.asr-1.v1','unirouter.endpoint-profile-speech-job-executor.v1'),
                ('unirouter.minimax.speech.2-8-turbo.v1','unirouter.endpoint-profile-speech-job-executor.v1'),
                ('unirouter.glm.speech.asr-2512.v1','unirouter.endpoint-profile-speech-job-executor.v1'),
                ('unirouter.glm.speech.tts.v1','unirouter.endpoint-profile-speech-job-executor.v1'),
                ('unirouter.openai.audio.transcribe.v1','unirouter.endpoint-profile-speech-job-executor.v1'),
                ('unirouter.mimo.audio.asr.v1','unirouter.endpoint-profile-speech-job-executor.v1'),
                ('unirouter.mimo.audio.tts.v1','unirouter.endpoint-profile-speech-job-executor.v1'),
                ('unirouter.dashscope.speech.asr-3-1.v1','unirouter.endpoint-profile-speech-job-executor.v1'),
                ('unirouter.dashscope.speech.tts-3-1.v1','unirouter.endpoint-profile-speech-job-executor.v1')
            )
            AND delivery_contract_version='unirouter.project-media-object-delivery.v1'
            AND charge_contract_version='unirouter.provider-media-charge-fact.v1'
            AND idempotency_contract_version='unirouter.project-media-job-idempotency.v1')
        OR
        (
            introduced_migration='000135_canonical_media_job_authority'
            AND operation_id='unirouter.media_jobs.v1'
            AND public_path='/v1/media/jobs'
            AND protocol_profile_version='1.0.0'
            AND (protocol_profile_id,executor_contract_version) IN (
                ('unirouter.openai.images.gpt-image.v1','unirouter.endpoint-profile-gpt-image-job-executor.v1'),
                ('unirouter.dashscope.images.qwen-3.v1','unirouter.endpoint-profile-qwen-image-job-executor.v1'),
                ('unirouter.minimax.images.h3.v1','unirouter.endpoint-profile-minimax-image-job-executor.v1')
            )
            AND delivery_contract_version='unirouter.project-media-object-delivery.v1'
            AND charge_contract_version='unirouter.provider-media-charge-fact.v1'
            AND idempotency_contract_version='unirouter.project-media-job-idempotency.v1'
        )
        OR (
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
            OR
            (
                introduced_migration='000132_openai_responses_client_tools'
                AND operation_id='openai.responses.v1'
                AND protocol_profile_id='unirouter.openai.responses.text-tools.v3'
                AND protocol_profile_version='1.0.0'
                AND executor_contract_version='unirouter.endpoint-profile-responses-tools-executor.v1'
                AND delivery_contract_version='unirouter.public-responses-tools-delivery.v1'
            )
            OR
            (
                introduced_migration='000134_native_inline_jpeg_public_bindings'
                AND operation_id='openai.responses.v1'
                AND public_path='/v1/responses'
                AND protocol_profile_id='unirouter.openai.responses.inline-jpeg.v1'
                AND protocol_profile_version='1.0.0'
                AND executor_contract_version='unirouter.endpoint-profile-responses-inline-jpeg-executor.v1'
                AND delivery_contract_version='unirouter.public-responses-tools-delivery.v1'
            )
            OR
            (
                introduced_migration='000134_native_inline_jpeg_public_bindings'
                AND operation_id='anthropic.messages.v1'
                AND public_path='/v1/messages'
                AND protocol_profile_id='unirouter.anthropic.messages.inline-jpeg.v1'
                AND protocol_profile_version='1.0.0'
                AND executor_contract_version='unirouter.endpoint-profile-messages-inline-jpeg-executor.v1'
                AND delivery_contract_version='unirouter.public-anthropic-messages-tools-delivery.v1'
            )
        )
        )
    );

INSERT INTO model_operation_execution_support_bindings
(operation_id,public_path,protocol_profile_id,protocol_profile_version,executor_contract_version,introduced_migration)
VALUES ('unirouter.media_jobs.v1','/v1/media/jobs','unirouter.dashscope.video.wan-3.v1','1.0.0','unirouter.endpoint-profile-async-video-job-executor.v1','000136_async_media_provider_task'),
('unirouter.media_jobs.v1','/v1/media/jobs','unirouter.minimax.video.h3.v1','1.0.0','unirouter.endpoint-profile-async-video-job-executor.v1','000136_async_media_provider_task'),
('unirouter.media_jobs.v1','/v1/media/jobs','unirouter.glm.video.cogvideox-3.v1','1.0.0','unirouter.endpoint-profile-async-video-job-executor.v1','000136_async_media_provider_task'),
('unirouter.media_jobs.v1','/v1/media/jobs','unirouter.glm.images.glm-image.v1','1.0.0','unirouter.endpoint-profile-glm-image-job-executor.v1','000136_async_media_provider_task'),
('unirouter.media_jobs.v1','/v1/media/jobs','unirouter.deepseek.tools.ocr.v1','1.0.0','unirouter.endpoint-profile-ocr-job-executor.v1','000136_async_media_provider_task'),
('unirouter.media_jobs.v1','/v1/media/jobs','unirouter.glm.ocr.layout.v1','1.0.0','unirouter.endpoint-profile-ocr-job-executor.v1','000136_async_media_provider_task'),
('unirouter.media_jobs.v1','/v1/media/jobs','unirouter.deepseek.tools.asr.v1','1.0.0','unirouter.endpoint-profile-speech-job-executor.v1','000136_async_media_provider_task'),
('unirouter.media_jobs.v1','/v1/media/jobs','unirouter.deepseek.tools.tts.v1','1.0.0','unirouter.endpoint-profile-speech-job-executor.v1','000136_async_media_provider_task'),
('unirouter.media_jobs.v1','/v1/media/jobs','unirouter.minimax.speech.asr-1.v1','1.0.0','unirouter.endpoint-profile-speech-job-executor.v1','000136_async_media_provider_task'),
('unirouter.media_jobs.v1','/v1/media/jobs','unirouter.minimax.speech.2-8-turbo.v1','1.0.0','unirouter.endpoint-profile-speech-job-executor.v1','000136_async_media_provider_task'),
('unirouter.media_jobs.v1','/v1/media/jobs','unirouter.glm.speech.asr-2512.v1','1.0.0','unirouter.endpoint-profile-speech-job-executor.v1','000136_async_media_provider_task'),
('unirouter.media_jobs.v1','/v1/media/jobs','unirouter.glm.speech.tts.v1','1.0.0','unirouter.endpoint-profile-speech-job-executor.v1','000136_async_media_provider_task'),
('unirouter.media_jobs.v1','/v1/media/jobs','unirouter.openai.audio.transcribe.v1','1.0.0','unirouter.endpoint-profile-speech-job-executor.v1','000136_async_media_provider_task'),
('unirouter.media_jobs.v1','/v1/media/jobs','unirouter.mimo.audio.asr.v1','1.0.0','unirouter.endpoint-profile-speech-job-executor.v1','000136_async_media_provider_task'),
('unirouter.media_jobs.v1','/v1/media/jobs','unirouter.mimo.audio.tts.v1','1.0.0','unirouter.endpoint-profile-speech-job-executor.v1','000136_async_media_provider_task'),
('unirouter.media_jobs.v1','/v1/media/jobs','unirouter.dashscope.speech.asr-3-1.v1','1.0.0','unirouter.endpoint-profile-speech-job-executor.v1','000136_async_media_provider_task'),
('unirouter.media_jobs.v1','/v1/media/jobs','unirouter.dashscope.speech.tts-3-1.v1','1.0.0','unirouter.endpoint-profile-speech-job-executor.v1','000136_async_media_provider_task');

INSERT INTO model_operation_public_execution_contract_extensions
(operation_id,public_path,protocol_profile_id,protocol_profile_version,executor_contract_version,delivery_contract_version,charge_contract_version,idempotency_contract_version,introduced_migration)
VALUES ('unirouter.media_jobs.v1','/v1/media/jobs','unirouter.dashscope.video.wan-3.v1','1.0.0','unirouter.endpoint-profile-async-video-job-executor.v1',
'unirouter.project-media-object-delivery.v1','unirouter.provider-media-charge-fact.v1','unirouter.project-media-job-idempotency.v1','000136_async_media_provider_task'),
('unirouter.media_jobs.v1','/v1/media/jobs','unirouter.minimax.video.h3.v1','1.0.0','unirouter.endpoint-profile-async-video-job-executor.v1',
'unirouter.project-media-object-delivery.v1','unirouter.provider-media-charge-fact.v1','unirouter.project-media-job-idempotency.v1','000136_async_media_provider_task'),
('unirouter.media_jobs.v1','/v1/media/jobs','unirouter.glm.video.cogvideox-3.v1','1.0.0','unirouter.endpoint-profile-async-video-job-executor.v1',
'unirouter.project-media-object-delivery.v1','unirouter.provider-media-charge-fact.v1','unirouter.project-media-job-idempotency.v1','000136_async_media_provider_task'),
('unirouter.media_jobs.v1','/v1/media/jobs','unirouter.glm.images.glm-image.v1','1.0.0','unirouter.endpoint-profile-glm-image-job-executor.v1',
'unirouter.project-media-object-delivery.v1','unirouter.provider-media-charge-fact.v1','unirouter.project-media-job-idempotency.v1','000136_async_media_provider_task'),
('unirouter.media_jobs.v1','/v1/media/jobs','unirouter.deepseek.tools.ocr.v1','1.0.0','unirouter.endpoint-profile-ocr-job-executor.v1',
'unirouter.project-media-object-delivery.v1','unirouter.provider-media-charge-fact.v1','unirouter.project-media-job-idempotency.v1','000136_async_media_provider_task'),
('unirouter.media_jobs.v1','/v1/media/jobs','unirouter.glm.ocr.layout.v1','1.0.0','unirouter.endpoint-profile-ocr-job-executor.v1',
'unirouter.project-media-object-delivery.v1','unirouter.provider-media-charge-fact.v1','unirouter.project-media-job-idempotency.v1','000136_async_media_provider_task'),
('unirouter.media_jobs.v1','/v1/media/jobs','unirouter.deepseek.tools.asr.v1','1.0.0','unirouter.endpoint-profile-speech-job-executor.v1',
'unirouter.project-media-object-delivery.v1','unirouter.provider-media-charge-fact.v1','unirouter.project-media-job-idempotency.v1','000136_async_media_provider_task'),
('unirouter.media_jobs.v1','/v1/media/jobs','unirouter.deepseek.tools.tts.v1','1.0.0','unirouter.endpoint-profile-speech-job-executor.v1',
'unirouter.project-media-object-delivery.v1','unirouter.provider-media-charge-fact.v1','unirouter.project-media-job-idempotency.v1','000136_async_media_provider_task'),
('unirouter.media_jobs.v1','/v1/media/jobs','unirouter.minimax.speech.asr-1.v1','1.0.0','unirouter.endpoint-profile-speech-job-executor.v1',
'unirouter.project-media-object-delivery.v1','unirouter.provider-media-charge-fact.v1','unirouter.project-media-job-idempotency.v1','000136_async_media_provider_task'),
('unirouter.media_jobs.v1','/v1/media/jobs','unirouter.minimax.speech.2-8-turbo.v1','1.0.0','unirouter.endpoint-profile-speech-job-executor.v1',
'unirouter.project-media-object-delivery.v1','unirouter.provider-media-charge-fact.v1','unirouter.project-media-job-idempotency.v1','000136_async_media_provider_task'),
('unirouter.media_jobs.v1','/v1/media/jobs','unirouter.glm.speech.asr-2512.v1','1.0.0','unirouter.endpoint-profile-speech-job-executor.v1',
'unirouter.project-media-object-delivery.v1','unirouter.provider-media-charge-fact.v1','unirouter.project-media-job-idempotency.v1','000136_async_media_provider_task'),
('unirouter.media_jobs.v1','/v1/media/jobs','unirouter.glm.speech.tts.v1','1.0.0','unirouter.endpoint-profile-speech-job-executor.v1',
'unirouter.project-media-object-delivery.v1','unirouter.provider-media-charge-fact.v1','unirouter.project-media-job-idempotency.v1','000136_async_media_provider_task'),
('unirouter.media_jobs.v1','/v1/media/jobs','unirouter.openai.audio.transcribe.v1','1.0.0','unirouter.endpoint-profile-speech-job-executor.v1',
'unirouter.project-media-object-delivery.v1','unirouter.provider-media-charge-fact.v1','unirouter.project-media-job-idempotency.v1','000136_async_media_provider_task'),
('unirouter.media_jobs.v1','/v1/media/jobs','unirouter.mimo.audio.asr.v1','1.0.0','unirouter.endpoint-profile-speech-job-executor.v1',
'unirouter.project-media-object-delivery.v1','unirouter.provider-media-charge-fact.v1','unirouter.project-media-job-idempotency.v1','000136_async_media_provider_task'),
('unirouter.media_jobs.v1','/v1/media/jobs','unirouter.mimo.audio.tts.v1','1.0.0','unirouter.endpoint-profile-speech-job-executor.v1',
'unirouter.project-media-object-delivery.v1','unirouter.provider-media-charge-fact.v1','unirouter.project-media-job-idempotency.v1','000136_async_media_provider_task'),
('unirouter.media_jobs.v1','/v1/media/jobs','unirouter.dashscope.speech.asr-3-1.v1','1.0.0','unirouter.endpoint-profile-speech-job-executor.v1',
'unirouter.project-media-object-delivery.v1','unirouter.provider-media-charge-fact.v1','unirouter.project-media-job-idempotency.v1','000136_async_media_provider_task'),
('unirouter.media_jobs.v1','/v1/media/jobs','unirouter.dashscope.speech.tts-3-1.v1','1.0.0','unirouter.endpoint-profile-speech-job-executor.v1',
'unirouter.project-media-object-delivery.v1','unirouter.provider-media-charge-fact.v1','unirouter.project-media-job-idempotency.v1','000136_async_media_provider_task');

CREATE TRIGGER model_operation_execution_support_bindings_immutable
BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON model_operation_execution_support_bindings
FOR EACH STATEMENT EXECUTE FUNCTION reject_model_operation_execution_support_mutation();
CREATE TRIGGER model_operation_public_execution_contract_extensions_immutable
BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON model_operation_public_execution_contract_extensions
FOR EACH STATEMENT EXECUTE FUNCTION reject_model_operation_public_execution_contract_mutation();


ALTER TABLE async_media_jobs
    ADD COLUMN provider_task_id text,
    ADD COLUMN provider_task_read_until timestamptz,
    ADD COLUMN provider_status_reads smallint NOT NULL DEFAULT 0 CHECK (provider_status_reads BETWEEN 0 AND 40),
    DROP CONSTRAINT async_media_jobs_operation_check,
    DROP CONSTRAINT async_media_jobs_profile_check,
    DROP CONSTRAINT async_media_jobs_output_shape_check,
    ADD CONSTRAINT async_media_jobs_operation_check CHECK (
        (operation='text_to_image' AND (profile_id IN ('unirouter.openai.images.gpt-image.v1','unirouter.dashscope.images.qwen-3.v1','unirouter.minimax.images.h3.v1') OR (profile_id='unirouter.glm.images.glm-image.v1' AND canonical_route IS NOT NULL)))
        OR (operation='text_to_video' AND profile_id IN ('unirouter.dashscope.video.wan-3.v1','unirouter.minimax.video.h3.v1','unirouter.glm.video.cogvideox-3.v1') AND canonical_route IS NOT NULL)
        OR (operation='image_to_text' AND profile_id IN ('unirouter.deepseek.tools.ocr.v1','unirouter.glm.ocr.layout.v1') AND canonical_route IS NOT NULL)
        OR (operation='speech_to_text' AND profile_id IN ('unirouter.deepseek.tools.asr.v1','unirouter.minimax.speech.asr-1.v1','unirouter.glm.speech.asr-2512.v1','unirouter.openai.audio.transcribe.v1','unirouter.mimo.audio.asr.v1','unirouter.dashscope.speech.asr-3-1.v1') AND canonical_route IS NOT NULL)
        OR (operation='text_to_speech' AND profile_id IN ('unirouter.deepseek.tools.tts.v1','unirouter.minimax.speech.2-8-turbo.v1','unirouter.glm.speech.tts.v1','unirouter.mimo.audio.tts.v1','unirouter.dashscope.speech.tts-3-1.v1') AND canonical_route IS NOT NULL)
    ),
    ADD CONSTRAINT async_media_jobs_provider_task_check CHECK (
        (provider_task_id IS NULL AND provider_task_read_until IS NULL AND provider_status_reads=0)
        OR (provider_task_id IS NOT NULL AND provider_task_id ~ '^[A-Za-z0-9_-]{1,128}$'
            AND provider_task_read_until IS NOT NULL AND started_at IS NOT NULL AND provider_task_read_until>started_at
            AND profile_id IN ('unirouter.dashscope.video.wan-3.v1','unirouter.minimax.video.h3.v1','unirouter.glm.video.cogvideox-3.v1') AND attempt=1 AND single_upstream_attempt
            AND canonical_dispatch_route IS NOT NULL
            AND (execution_state='terminal' OR provider_outcome='succeeded' OR execution_state='running'))
    ),
    ADD CONSTRAINT async_media_jobs_output_shape_check CHECK (
        (
            object_state IN ('pending', 'not_created')
            AND object_key = ''
            AND receipt_key = ''
            AND output_sha256 = ''
            AND output_media_type = ''
            AND output_size_bytes = 0
            AND output_persisted_at IS NULL
            AND output_expires_at IS NULL
            AND output_purged_at IS NULL
        )
        OR (
            object_state IN ('persisted', 'purge_pending')
            AND object_key <> ''
            AND receipt_key <> ''
            AND output_sha256 ~ '^sha256:[a-f0-9]{64}$'
            AND ((operation='text_to_image' AND output_media_type IN ('image/png','image/jpeg','image/webp')) OR (operation='text_to_video' AND output_media_type='video/mp4') OR (operation IN ('image_to_text','speech_to_text') AND output_media_type='text/plain; charset=utf-8') OR (operation='text_to_speech' AND output_media_type='audio/wav'))
            AND output_size_bytes > 0
            AND output_persisted_at IS NOT NULL
            AND output_expires_at = output_persisted_at + INTERVAL '7 days'
            AND output_purged_at IS NULL
        )
        OR (
            object_state = 'purged'
            AND object_key = ''
            AND receipt_key = ''
            AND output_sha256 ~ '^sha256:[a-f0-9]{64}$'
            AND ((operation='text_to_image' AND output_media_type IN ('image/png','image/jpeg','image/webp')) OR (operation='text_to_video' AND output_media_type='video/mp4') OR (operation IN ('image_to_text','speech_to_text') AND output_media_type='text/plain; charset=utf-8') OR (operation='text_to_speech' AND output_media_type='audio/wav'))
            AND output_size_bytes > 0
            AND output_persisted_at IS NOT NULL
            AND output_expires_at = output_persisted_at + INTERVAL '7 days'
            AND output_purged_at IS NOT NULL
        )
    );

ALTER TABLE async_media_charge_facts DROP CONSTRAINT async_media_charge_units_check,
    ADD CONSTRAINT async_media_charge_units_check CHECK (
        units = '[]'::jsonb
        OR (jsonb_typeof(units)='array' AND jsonb_array_length(units)=1
            AND units @> '[{"class":"input_audio_milliseconds","authority":"provider_reported","finality":"final","charge_state":"provider_observed_not_customer_chargeable"}]'::jsonb
            AND units->0->>'quantity' ~ '^([1-9][0-9]{0,2}|[12][0-9]{3}|3[0-9]{3}|4000)$')
        OR (jsonb_typeof(units)='array' AND jsonb_array_length(units)=1
            AND units @> '[{"class":"input_billable_characters","authority":"provider_reported","finality":"final","charge_state":"provider_observed_not_customer_chargeable"}]'::jsonb
            AND units->0->>'quantity' ~ '^(0|[1-9][0-9]?|[1-4][0-9]{2}|50[0-9]|51[0-2])$')
        OR (jsonb_typeof(units)='array'
            AND jsonb_array_length(units) = 2
                + (units @> '[{"class":"input_text_tokens","authority":"provider_reported","finality":"final","charge_state":"provider_observed_not_customer_chargeable"}]'::jsonb)::integer
                + (units @> '[{"class":"input_audio_tokens","authority":"provider_reported","finality":"final","charge_state":"provider_observed_not_customer_chargeable"}]'::jsonb)::integer
            AND units @> '[{"class":"input_tokens","authority":"provider_reported","finality":"final","charge_state":"provider_observed_not_customer_chargeable"},
                            {"class":"output_tokens","authority":"provider_reported","finality":"final","charge_state":"provider_observed_not_customer_chargeable"}]'::jsonb)

        OR (jsonb_typeof(units)='array' AND jsonb_array_length(units) BETWEEN 1 AND 3
            AND jsonb_array_length(units) =
                  (units @> '[{"class":"input_tokens","authority":"provider_reported","finality":"final","charge_state":"provider_observed_not_customer_chargeable"}]'::jsonb)::integer
                + (units @> '[{"class":"output_tokens","authority":"provider_reported","finality":"final","charge_state":"provider_observed_not_customer_chargeable"}]'::jsonb)::integer
                + (units @> '[{"class":"input_audio_milliseconds","authority":"provider_reported","finality":"final","charge_state":"provider_observed_not_customer_chargeable"}]'::jsonb)::integer
            AND NOT jsonb_path_exists(units, '$[*] ? (!(exists(@.quantity)) || @.quantity.type() != "string" || !(@.quantity like_regex "^(0|[1-9][0-9]{0,18})$"))')
            AND NOT jsonb_path_exists(units, '$[*] ? (@.class == "input_audio_milliseconds" && !(@.quantity like_regex "^([1-9][0-9]{0,2}|[1-3][0-9]{3}|4000)$"))'))
        OR (jsonb_typeof(units)='array' AND jsonb_array_length(units)=1
            AND units @> '[{"class":"recognized_pages","quantity":"1","authority":"provider_reported","finality":"final","charge_state":"provider_observed_not_customer_chargeable"}]'::jsonb)
        OR (jsonb_typeof(units)='array'
         AND (jsonb_array_length(units)=1 OR (jsonb_array_length(units)=2
             AND units @> '[{"class":"input_video_milliseconds","quantity":"0","authority":"provider_reported","finality":"final","charge_state":"provider_observed_not_customer_chargeable"}]'::jsonb))
         AND units @> '[{"class":"output_video_milliseconds","quantity":"2000","authority":"provider_reported","finality":"final","charge_state":"provider_observed_not_customer_chargeable"}]'::jsonb)
        OR
        (jsonb_typeof(units)='array'
         AND units @> '[{"class":"output_video_milliseconds","quantity":"4000","authority":"provider_reported","finality":"final","charge_state":"provider_observed_not_customer_chargeable"}]'::jsonb
         AND jsonb_array_length(units) = 1
             + (units @> '[{"class":"input_video_milliseconds","quantity":"0","authority":"provider_reported","finality":"final","charge_state":"provider_observed_not_customer_chargeable"}]'::jsonb)::integer
             + (units @> '[{"class":"input_images","quantity":"0","authority":"provider_reported","finality":"final","charge_state":"provider_observed_not_customer_chargeable"}]'::jsonb)::integer)
        OR
        (
            jsonb_typeof(units) = 'array'
            AND (jsonb_array_length(units) = 4 OR (
                jsonb_array_length(units) = 5
                AND units @> '[{"class":"output_images","quantity":"1","authority":"provider_reported","finality":"final","charge_state":"provider_observed_not_customer_chargeable"}]'::jsonb
            ))
            AND units @> '[{"class":"input_tokens","authority":"provider_reported","finality":"final","charge_state":"provider_observed_not_customer_chargeable"}]'::jsonb
            AND units @> '[{"class":"input_text_tokens","authority":"provider_reported","finality":"final","charge_state":"provider_observed_not_customer_chargeable"}]'::jsonb
            AND units @> '[{"class":"input_image_tokens","authority":"provider_reported","finality":"final","charge_state":"provider_observed_not_customer_chargeable"}]'::jsonb
            AND units @> '[{"class":"output_image_tokens","authority":"provider_reported","finality":"final","charge_state":"provider_observed_not_customer_chargeable"}]'::jsonb
        )
        OR (
            jsonb_typeof(units) = 'array'
            AND jsonb_array_length(units) = 2
            AND units @> '[{"class":"input_images","quantity":"0","authority":"provider_reported","finality":"final","charge_state":"provider_observed_not_customer_chargeable"},
                          {"class":"output_images","quantity":"1","authority":"provider_reported","finality":"final","charge_state":"provider_observed_not_customer_chargeable"}]'::jsonb
        )
    );

CREATE FUNCTION require_metered_media_charge_units() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.units = '[]'::jsonb AND NOT EXISTS (
        SELECT 1 FROM async_media_jobs
         WHERE job_id=NEW.job_id AND org_id=NEW.org_id AND project_id=NEW.project_id
           AND profile_id IN ('unirouter.glm.video.cogvideox-3.v1','unirouter.glm.images.glm-image.v1','unirouter.deepseek.tools.tts.v1','unirouter.minimax.speech.2-8-turbo.v1','unirouter.glm.speech.asr-2512.v1','unirouter.glm.speech.tts.v1','unirouter.openai.audio.transcribe.v1','unirouter.mimo.audio.tts.v1','unirouter.dashscope.speech.asr-3-1.v1','unirouter.dashscope.speech.tts-3-1.v1')
    ) THEN
        RAISE EXCEPTION 'metered media charge requires reported units' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER async_media_charge_unmetered_profile_guard BEFORE INSERT OR UPDATE OF units,job_id,org_id,project_id
ON async_media_charge_facts FOR EACH ROW EXECUTE FUNCTION require_metered_media_charge_units();

CREATE FUNCTION preserve_media_job_provider_task() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.provider_task_id IS NOT NULL AND (NEW.provider_task_id IS DISTINCT FROM OLD.provider_task_id
       OR NEW.provider_task_read_until IS DISTINCT FROM OLD.provider_task_read_until) THEN
        RAISE EXCEPTION 'media provider task receipt is immutable' USING ERRCODE='55000';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER async_media_jobs_task_immutable BEFORE UPDATE OF provider_task_id,provider_task_read_until
ON async_media_jobs FOR EACH ROW EXECUTE FUNCTION preserve_media_job_provider_task();

CREATE TABLE model_deployment_async_probe_tasks (
    org_id bigint NOT NULL REFERENCES orgs(id) ON DELETE RESTRICT,
    deployment_id bigint NOT NULL,
    operation_id varchar(80) NOT NULL,
    operation_revision bigint NOT NULL CHECK (operation_revision>0),
    source_ref varchar(64) NOT NULL UNIQUE,
    identity_digest text NOT NULL CHECK (identity_digest ~ '^[0-9a-f]{64}$'),
    task_id text CHECK (task_id ~ '^[A-Za-z0-9_-]{1,128}$'),
    read_until timestamptz NOT NULL,
    next_read_at timestamptz NOT NULL,
    read_fence bigint NOT NULL DEFAULT 0 CHECK (read_fence BETWEEN 0 AND 40),
    result jsonb CHECK (jsonb_typeof(result)='object' AND octet_length(result::text) BETWEEN 2 AND 8192),
    PRIMARY KEY (org_id,deployment_id,operation_id,operation_revision),
    FOREIGN KEY (deployment_id,operation_id) REFERENCES model_deployment_operations(deployment_id,operation_id) ON DELETE RESTRICT,
    CHECK (result IS NULL OR (task_id IS NOT NULL AND read_fence>0))
);

CREATE OR REPLACE FUNCTION public.verify_source_budget_coverage_commit() RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE c execution_source_budget_coverage%ROWTYPE; m JSONB; ledger TEXT[]; certified_ledger TEXT[];
BEGIN
    SELECT * INTO c FROM execution_source_budget_coverage WHERE coverage_epoch=NEW.coverage_epoch;
    SELECT array_agg(version::text ORDER BY version) INTO ledger FROM schema_migrations;
    IF ledger[cardinality(ledger)]='000136_async_media_provider_task' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000135_canonical_media_job_authority' THEN
        RAISE EXCEPTION 'budget coverage requires exact M136 predecessor'; END IF;
      ledger := ledger[1:cardinality(ledger)-1];
    END IF;
    IF ledger[cardinality(ledger)]='000135_canonical_media_job_authority' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000134_native_inline_jpeg_public_bindings' THEN
        RAISE EXCEPTION 'budget coverage requires exact M135 predecessor'; END IF;
      ledger := ledger[1:cardinality(ledger)-1];
    END IF;
    IF ledger[cardinality(ledger)]='000134_native_inline_jpeg_public_bindings' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000133_media_charge_source_facts' THEN
        RAISE EXCEPTION 'budget coverage requires exact M134 predecessor'; END IF;
      ledger := ledger[1:cardinality(ledger)-1];
    END IF;
    IF ledger[cardinality(ledger)]='000133_media_charge_source_facts' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000132_openai_responses_client_tools' THEN
        RAISE EXCEPTION 'budget coverage requires exact M133 predecessor'; END IF;
      ledger := ledger[1:cardinality(ledger)-1];
    END IF;
    IF ledger[cardinality(ledger)]='000132_openai_responses_client_tools' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000131_customer_standard_pricing' THEN
        RAISE EXCEPTION 'budget coverage requires exact M132 predecessor'; END IF;
      ledger := ledger[1:cardinality(ledger)-1];
    END IF;
    IF ledger[cardinality(ledger)]='000131_customer_standard_pricing' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000130_upgrade_admission_recovery' THEN
        RAISE EXCEPTION 'budget coverage requires exact M131 predecessor'; END IF;
      ledger := ledger[1:cardinality(ledger)-1];
    END IF;
    IF ledger[cardinality(ledger)]='000130_upgrade_admission_recovery' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000129_upstream_observation_restore_namespace' THEN
        RAISE EXCEPTION 'budget coverage requires exact M130 predecessor'; END IF;
      ledger := ledger[1:cardinality(ledger)-1];
    END IF;
    IF ledger[cardinality(ledger)]='000129_upstream_observation_restore_namespace' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000128_upstream_release_observation' THEN
        RAISE EXCEPTION 'budget coverage requires exact M129 predecessor'; END IF;
      ledger := ledger[1:cardinality(ledger)-1];
    END IF;
    IF ledger[cardinality(ledger)]='000128_upstream_release_observation' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000127_xinsight_communication_policy' THEN
        RAISE EXCEPTION 'budget coverage requires exact M128 predecessor'; END IF;
      ledger := ledger[1:cardinality(ledger)-1];
    END IF;
    certified_ledger := ledger;
    IF ledger[cardinality(ledger)]='000127_xinsight_communication_policy' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000126_commercial_license_retirement' THEN
        RAISE EXCEPTION 'budget coverage requires exact M127 predecessor'; END IF;
      ledger := ledger[1:cardinality(ledger)-1];
      certified_ledger := ledger;
    END IF;
    IF ledger[cardinality(ledger)]='000126_commercial_license_retirement' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000125_xinsight_control_credential_lifecycle' THEN
        RAISE EXCEPTION 'budget coverage requires exact M126 predecessor'; END IF;
      ledger := ledger[1:cardinality(ledger)-1];
      certified_ledger := ledger;
    END IF;
    IF ledger[cardinality(ledger)]='000125_xinsight_control_credential_lifecycle' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000124_xinsight_receiver_adoption' THEN
        RAISE EXCEPTION 'budget coverage requires exact M125 predecessor'; END IF;
      ledger := ledger[1:cardinality(ledger)-1];
      certified_ledger := ledger;
    END IF;
    IF ledger[cardinality(ledger)]='000124_xinsight_receiver_adoption' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000123_xinsight_policy_delegation' THEN
        RAISE EXCEPTION 'budget coverage requires exact M124 predecessor'; END IF;
      ledger := ledger[1:cardinality(ledger)-1];
      certified_ledger := ledger;
    END IF;
    IF ledger[cardinality(ledger)]='000123_xinsight_policy_delegation' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000122_scoped_content_policy'
         OR ledger[cardinality(ledger)-2] IS DISTINCT FROM '000121_gateway_pre_dispatch_terminal'
         OR ledger[cardinality(ledger)-3] IS DISTINCT FROM '000120_unpublished_content_reclamation'
         OR ledger[cardinality(ledger)-4] IS DISTINCT FROM '000119_execution_content_capture' THEN
        RAISE EXCEPTION 'budget coverage requires exact M123 predecessor'; END IF;
      certified_ledger := ledger[:cardinality(ledger)-5];
    ELSIF ledger[cardinality(ledger)]='000122_scoped_content_policy' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000121_gateway_pre_dispatch_terminal'
         OR ledger[cardinality(ledger)-2] IS DISTINCT FROM '000120_unpublished_content_reclamation'
         OR ledger[cardinality(ledger)-3] IS DISTINCT FROM '000119_execution_content_capture' THEN
        RAISE EXCEPTION 'budget coverage requires exact M122 predecessor'; END IF;
      certified_ledger := ledger[:cardinality(ledger)-4];
    ELSIF ledger[cardinality(ledger)]='000121_gateway_pre_dispatch_terminal' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000120_unpublished_content_reclamation'
         OR ledger[cardinality(ledger)-2] IS DISTINCT FROM '000119_execution_content_capture' THEN
        RAISE EXCEPTION 'budget coverage requires exact M121 predecessor'; END IF;
      certified_ledger := ledger[:cardinality(ledger)-3];
    ELSIF ledger[cardinality(ledger)]='000120_unpublished_content_reclamation' THEN
      IF ledger[cardinality(ledger)-1] IS DISTINCT FROM '000119_execution_content_capture' THEN
        RAISE EXCEPTION 'budget coverage requires exact M120 predecessor'; END IF;
      certified_ledger := ledger[:cardinality(ledger)-2];
    ELSIF ledger[cardinality(ledger)]='000119_execution_content_capture' THEN
      certified_ledger := ledger[:cardinality(ledger)-1];
    END IF;
    IF c.state IS DISTINCT FROM 'certified' OR c.support_manifest_sha256 IS DISTINCT FROM encode(sha256(c.support_manifest_bytes),'hex')
       OR certified_ledger[cardinality(certified_ledger)] IS DISTINCT FROM '000118_source_budget_snapshots'
       OR c.schema_ledger_sha256 IS DISTINCT FROM encode(sha256(convert_to(array_to_string(certified_ledger,E'\n'),'UTF8')),'hex') THEN
      RAISE EXCEPTION 'budget coverage requires verified support proof in the migration transaction'; END IF;
    m := convert_from(c.support_manifest_bytes,'UTF8')::jsonb;
    IF COALESCE((m->'schema_heads' ? '000118_source_budget_snapshots'),false)=false OR COALESCE((m->'implementations' @>
       '[{"kind":"source_budgets","id":"unirouter.budget-snapshot.v1"},{"kind":"financial_journal","id":"unirouter.gateway-financial-journal.v2"}]'::jsonb),false)=false THEN
      RAISE EXCEPTION 'budget coverage support proof lacks the writer or snapshot contract'; END IF;
    RETURN NEW;
END $$;

COMMIT;
