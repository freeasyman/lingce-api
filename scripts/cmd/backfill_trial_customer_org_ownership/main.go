package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/freeasyman/lingce-api/internal/scriptutil"
	istore "github.com/freeasyman/lingce-api/internal/store"
)

func main() {
	configPath := flag.String("config", "./configs/dev.toml", "path to TOML config file")
	flag.Parse()

	cfg, pool, err := scriptutil.OpenPool(context.Background(), *configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open pool: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := istore.ApplyCompatMigrations(context.Background(), pool); err != nil {
		fmt.Fprintf(os.Stderr, "apply compat migrations: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("trial customer org ownership backfill completed on %s\n", cfg.Database.Name)
}
