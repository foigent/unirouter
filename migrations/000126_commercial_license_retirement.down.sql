-- No generic downgrade owner exists for this maintenance boundary.
-- Recover the verified pre-M126 backup with its original compatible Artifact.
DO $$ BEGIN
 RAISE EXCEPTION 'M126 downgrade is unsupported: restore the pre-M126 backup and its original Artifact using the approved recovery runbook';
END $$;
