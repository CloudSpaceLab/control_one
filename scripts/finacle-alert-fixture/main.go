package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	action := flag.String("action", "", "seed or delete")
	databaseURL := flag.String("database-url", "", "PostgreSQL connection URL")
	tenantID := flag.String("tenant-id", "", "tenant UUID")
	shiftID := flag.String("shift-id", "", "Finacle shift UUID")
	finacleUID := flag.String("finacle-uid", "", "temporary Finacle user ID")
	flag.Parse()

	if *action == "" || *databaseURL == "" || *tenantID == "" || *finacleUID == "" {
		flag.Usage()
		os.Exit(2)
	}
	if *action == "seed" && *shiftID == "" {
		log.Fatal("-shift-id is required for seed")
	}

	db, err := sql.Open("pgx", *databaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	switch *action {
	case "seed":
		var id string
		err = db.QueryRowContext(ctx, `
			INSERT INTO finacle_profiles (
				id, tenant_id, finacle_uid, branch_id, role, shift_id, status
			) VALUES (gen_random_uuid(), $1, $2, 'LOCAL-TEST', 'teller', $3, 'revoked')
			ON CONFLICT (tenant_id, finacle_uid) DO UPDATE SET
				branch_id = EXCLUDED.branch_id,
				role = EXCLUDED.role,
				shift_id = EXCLUDED.shift_id,
				status = EXCLUDED.status
			RETURNING id
		`, *tenantID, *finacleUID, *shiftID).Scan(&id)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(id)
	case "delete":
		_, err = db.ExecContext(ctx,
			`DELETE FROM finacle_profiles WHERE tenant_id = $1 AND finacle_uid = $2`,
			*tenantID, *finacleUID)
		if err != nil {
			log.Fatal(err)
		}
	default:
		log.Fatalf("unknown action %q", *action)
	}
}
