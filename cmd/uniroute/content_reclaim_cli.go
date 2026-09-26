package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/uniroute/unirouter/internal/contentcapture"
	"github.com/uniroute/unirouter/internal/schemaauthority"
	"github.com/uniroute/unirouter/internal/sourcefacts"
)

const contentReclaimUsage = "usage: uniroute content-store reclaim-unpublished <check|apply> --root PATH --identity ID --token-file PATH --migrations-dir PATH --environment ID --database NAME --database-oid ID --database-owner ROLE --database-role ROLE --system-identifier ID --source-instance ID --org-id ID --project-id ID --content-ref REF --all-owners-stopped --restarts-prohibited --offline-evidence-sha256 sha256:HEX; DSN from UR_CONTENT_RECLAIM_DATABASE_URL"

type contentReclaimCLIInput struct {
	request                           sourcefacts.UnpublishedReclaimRequest
	root, identity, token, migrations string
}

func parseContentReclaimCLI(args []string) (contentReclaimCLIInput, error) {
	var in contentReclaimCLIInput
	fail := func() (contentReclaimCLIInput, error) { return in, errors.New(contentReclaimUsage) }
	if len(args) == 0 || (args[0] != "check" && args[0] != "apply") {
		return fail()
	}
	in.request.CheckOnly = args[0] == "check"
	fs := flag.NewFlagSet("reclaim-unpublished", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&in.root, "root", "", "existing private root")
	fs.StringVar(&in.identity, "identity", "", "exact storage identity")
	fs.StringVar(&in.token, "token-file", "", "existing private token file")
	fs.StringVar(&in.migrations, "migrations-dir", "", "installed trusted migration artifact")
	r := &in.request
	fs.StringVar(&r.Target.Environment, "environment", "", "exact environment")
	fs.StringVar(&r.Target.Database, "database", "", "exact database")
	fs.Int64Var(&r.Target.DatabaseOID, "database-oid", 0, "exact database OID")
	fs.StringVar(&r.Target.Owner, "database-owner", "", "exact owner")
	fs.StringVar(&r.Target.Role, "database-role", "", "exact connected role")
	fs.StringVar(&r.Target.SystemIdentifier, "system-identifier", "", "exact PostgreSQL cluster identity")
	fs.StringVar(&r.SourceInstance, "source-instance", "", "exact producer")
	fs.Int64Var(&r.OrgID, "org-id", 0, "exact organization")
	fs.Int64Var(&r.ProjectID, "project-id", 0, "exact project")
	fs.StringVar(&r.ContentRef, "content-ref", "", "one exact persisted reservation")
	fs.BoolVar(&r.AllOwnersStopped, "all-owners-stopped", false, "operator has verified all shared owners exited")
	fs.BoolVar(&r.RestartsProhibited, "restarts-prohibited", false, "operator has prevented owner restarts")
	fs.StringVar(&r.OfflineEvidenceSHA256, "offline-evidence-sha256", "", "digest of retained offline proof")
	if fs.Parse(args[1:]) != nil || fs.NArg() != 0 || in.root == "" || in.identity == "" || in.token == "" || in.migrations == "" || !r.AllOwnersStopped || !r.RestartsProhibited || r.Target.Environment == "" || r.Target.Database == "" || r.Target.DatabaseOID <= 0 || r.Target.Owner == "" || r.Target.Role == "" || r.Target.SystemIdentifier == "" || r.SourceInstance == "" || r.OrgID <= 0 || r.ProjectID <= 0 || r.ContentRef == "" || r.OfflineEvidenceSHA256 == "" {
		return fail()
	}
	return in, nil
}
func runContentReclaimCLI(ctx context.Context, args []string, out io.Writer) error {
	in, err := parseContentReclaimCLI(args)
	if err != nil {
		return err
	}
	refused := errors.New("offline content reclamation refused; check exact target, M120 artifact/schema, offline proof, reservation/history, quota and store binding; no credentials or raw DB errors are printed")
	if ctx == nil {
		return refused
	}
	in.request.ExpectedLedger, err = schemaauthority.ExpectedLedger(in.migrations)
	if err != nil {
		return refused
	}
	// Opening an already bound root cannot initialize/rotate a marker or key.
	files, err := contentcapture.OpenBoundFileStore(in.root, in.identity, in.token)
	if err != nil {
		return refused
	}
	defer files.Close()
	dsn := os.Getenv("UR_CONTENT_RECLAIM_DATABASE_URL")
	if dsn == "" {
		return refused
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return refused
	}
	cfg.MaxConns = 2
	cfg.MinConns = 0
	cfg.ConnConfig.RuntimeParams["application_name"] = "uniroute-offline-content-reclaim"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return refused
	}
	defer pool.Close()
	result, err := sourcefacts.ReclaimUnpublishedContent(ctx, pool, files, in.request)
	if err != nil {
		return refused
	}
	return json.NewEncoder(out).Encode(result)
}
