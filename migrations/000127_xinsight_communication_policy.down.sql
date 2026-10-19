DO $$ BEGIN RAISE EXCEPTION 'M127 rollback requires restoring the exact pre-M127 backup after stopped-writer proof; never discard manual intent or run receipts'; END $$;
