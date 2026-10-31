-- Never reintroduce the known inability to restore valid nonempty observations.
DO $$ BEGIN
 RAISE EXCEPTION 'M129 namespace correction has no supported automatic downgrade';
END $$;
