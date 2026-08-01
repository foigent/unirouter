BEGIN;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM schema_migrations
        WHERE version > '000048_prd8_enterprise_directory_people_accounts'
    ) THEN
        RAISE EXCEPTION 'PRD8 enterprise-directory people/accounts rollback refused: later migrations must be reversed first';
    END IF;
    IF EXISTS (SELECT 1 FROM enterprise_directory_person_snapshots)
       OR EXISTS (SELECT 1 FROM enterprise_directory_account_snapshots)
       OR EXISTS (SELECT 1 FROM enterprise_directory_iam_actions) THEN
        RAISE EXCEPTION 'PRD8 enterprise-directory people/accounts rollback refused: projection or IAM evidence exists';
    END IF;
END;
$$;

DROP TABLE enterprise_directory_iam_actions;
DROP TABLE enterprise_directory_account_snapshots;
DROP TABLE enterprise_directory_person_snapshots;

DELETE FROM schema_migrations
WHERE version = '000048_prd8_enterprise_directory_people_accounts';

COMMIT;
