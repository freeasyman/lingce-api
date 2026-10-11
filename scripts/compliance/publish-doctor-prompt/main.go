// Explicit manual release tool, deliberately outside automatic migrations.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/freeasyman/lingce-api/internal/config"
	"github.com/jackc/pgx/v5"
)

const code = "compliance_doctor_check_v1"
const version = "v4-safety-20261011"

type prompt struct {
	ID      int64           `json:"id"`
	System  string          `json:"system_prompt"`
	User    string          `json:"user_prompt_template"`
	Schema  json.RawMessage `json:"output_schema"`
	Version string          `json:"version"`
	Active  bool            `json:"is_active"`
}
type snapshot struct {
	Database      string   `json:"database"`
	Code          string   `json:"code"`
	CandidateHash string   `json:"candidate_sha256"`
	Previous      []prompt `json:"previous"`
}

func hash(p prompt) string { b, _ := json.Marshal(p); return fmt.Sprintf("%x", sha256.Sum256(b)) }
func read(ctx context.Context, tx pgx.Tx, code string) ([]prompt, error) {
	rows, err := tx.Query(ctx, `SELECT id,system_prompt,user_prompt_template,output_schema,version,is_active FROM recording_analysis_prompts WHERE code=$1 ORDER BY id FOR UPDATE`, code)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []prompt{}
	for rows.Next() {
		var p prompt
		if err := rows.Scan(&p.ID, &p.System, &p.User, &p.Schema, &p.Version, &p.Active); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	mode := flag.String("mode", "plan", "plan, apply, rollback")
	configPath := flag.String("config", "configs/dev.toml", "project development configuration")
	candidatePath := flag.String("candidate", "docs/compliance/候选回放/医生风险线索提示词-v3.json", "reviewed candidate")
	backupPath := flag.String("backup", "", "snapshot file outside repository, mandatory for apply/rollback")
	flag.Parse()
	if *mode != "plan" && *mode != "apply" && *mode != "rollback" {
		return fmt.Errorf("invalid mode")
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	if cfg.App.Env != "dev" || cfg.Database.Name != "lingce_dev" {
		return fmt.Errorf("this tool only supports lingce_dev")
	}
	var candidate prompt
	b, err := os.ReadFile(*candidatePath)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(b, &candidate); err != nil {
		return err
	}
	if candidate.System == "" || candidate.User == "" || !json.Valid(candidate.Schema) {
		return fmt.Errorf("incomplete candidate")
	}
	candidate.Version, candidate.Active = version, true
	if *mode != "plan" {
		if *backupPath == "" {
			return fmt.Errorf("backup outside repository required")
		}
		root, _ := filepath.Abs(".")
		path, _ := filepath.Abs(*backupPath)
		rel, err := filepath.Rel(root, path)
		if err != nil || (rel != ".." && len(rel) >= 3 && rel[:3] != "../") || rel == "." {
			return fmt.Errorf("backup must be outside repository")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, cfg.Database.DSN())
	if err != nil {
		return fmt.Errorf("connect development database: %w", err)
	}
	defer conn.Close(ctx)
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('compliance-doctor-prompt-release'))`); err != nil {
		return err
	}
	shared, err := read(ctx, tx, "compliance_check_v1")
	if err != nil {
		return err
	}
	if len(shared) != 1 || !shared[0].Active {
		return fmt.Errorf("expected one active shared prompt")
	}
	previous, err := read(ctx, tx, code)
	if err != nil {
		return err
	}
	if len(previous) > 1 {
		return fmt.Errorf("multiple doctor configurations; abort")
	}
	if *mode == "plan" {
		fmt.Printf("database=%s shared=%s/%s unchanged; doctor=%s/%s candidate_sha256=%s existing=%d\n", cfg.Database.Name, "compliance_check_v1", shared[0].Version, code, version, hash(candidate), len(previous))
		return nil
	}
	if *mode == "apply" {
		snap := snapshot{Database: cfg.Database.Name, Code: code, CandidateHash: hash(candidate), Previous: previous}
		b, err := json.MarshalIndent(snap, "", "  ")
		if err != nil {
			return err
		}
		f, err := os.OpenFile(*backupPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, err = f.Write(b)
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if len(previous) == 0 {
			_, err = tx.Exec(ctx, `INSERT INTO recording_analysis_prompts(code,name,description,category,system_prompt,user_prompt_template,output_schema,version,is_active,usage_count,created_at,updated_at) VALUES($1,'医生沟通核查','医生独立输出契约；咨询师继续使用共享配置','analysis',$2,$3,$4::json,$5,true,0,NOW(),NOW())`, code, candidate.System, candidate.User, string(candidate.Schema), version)
		} else {
			_, err = tx.Exec(ctx, `UPDATE recording_analysis_prompts SET system_prompt=$2,user_prompt_template=$3,output_schema=$4::json,version=$5,is_active=true,updated_at=NOW() WHERE id=$1`, previous[0].ID, candidate.System, candidate.User, string(candidate.Schema), version)
		}
		if err != nil {
			return err
		}
	} else {
		var snap snapshot
		b, err := os.ReadFile(*backupPath)
		if err != nil {
			return err
		}
		if err = json.Unmarshal(b, &snap); err != nil {
			return err
		}
		if snap.Database != cfg.Database.Name || snap.Code != code || len(snap.Previous) > 1 || len(previous) != 1 {
			return fmt.Errorf("invalid rollback snapshot/state")
		}
		current := previous[0]
		current.ID = 0
		if hash(current) != snap.CandidateHash {
			return fmt.Errorf("doctor configuration changed since release; abort rollback")
		}
		if len(snap.Previous) == 0 {
			_, err = tx.Exec(ctx, `UPDATE recording_analysis_prompts SET is_active=false,updated_at=NOW() WHERE id=$1`, previous[0].ID)
		} else {
			p := snap.Previous[0]
			_, err = tx.Exec(ctx, `UPDATE recording_analysis_prompts SET system_prompt=$2,user_prompt_template=$3,output_schema=$4::json,version=$5,is_active=$6,updated_at=NOW() WHERE id=$1`, previous[0].ID, p.System, p.User, string(p.Schema), p.Version, p.Active)
		}
		if err != nil {
			return err
		}
	}
	check, err := read(ctx, tx, "compliance_check_v1")
	if err != nil {
		return err
	}
	if len(check) != 1 || hash(check[0]) != hash(shared[0]) {
		return fmt.Errorf("shared configuration changed; rolling back")
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	fmt.Printf("%s complete: %s; shared prompt unchanged; snapshot=%s\n", *mode, code, *backupPath)
	return nil
}
