// SPDX-License-Identifier: Apache-2.0

package s0probe

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestAuthorityMechanismMatrix records narrowly falsifiable hypotheses. A green
// test means the stated limitation was reproduced, not that the SQL gate passed.
// It never reclassifies TestSQLViewBoundary's known escape as a passing design.
func TestAuthorityMechanismMatrix(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	f := openFixture(t, ctx)
	r := boundaryReport{
		Kind: "s0-sql-authority-mechanism-matrix", RunID: f.id,
		Started: time.Now().UTC(), CriteriaSHA256: criteriaSHA256,
		BaselineCommit: baselineCommit, Database: f.database, FixtureID: f.id,
		Classification: "incompatible-mechanisms-reproduced",
		Roles:          map[string]string{"runtime": f.login, "caller": f.caller, "jobs": f.jobs, "owner": f.owner},
		Limitations:    []string{"hypothesis diagnostics, not SQL-boundary acceptance", "no River insertion or performance measured", "all fixture names unique; objects retained"},
	}

	t.Run("revoke_set_does_not_freeze_userset_placeholder", func(t *testing.T) {
		// PostgreSQL parameter ACLs do not restrict ordinary USERSET/custom
		// placeholders; record this rather than treating REVOKE as protection.
		_, err := f.admin.Exec(ctx, "REVOKE SET ON PARAMETER \"s0.tenant\" FROM PUBLIC, "+ident(f.login)+", "+ident(f.caller))
		if err != nil {
			t.Fatalf("parameter privilege precondition: SQLSTATE %s", sqlState(err))
		}
		tx, err := f.beginCaller(ctx, "scope-a")
		if err != nil {
			t.Fatalf("begin caller: SQLSTATE %s", sqlState(err))
		}
		defer tx.Rollback(context.Background())
		var scope string
		if err := tx.QueryRow(ctx, "SELECT set_config('s0.tenant','scope-b',true)").Scan(&scope); err != nil {
			t.Fatalf("unexpected setting refusal: SQLSTATE %s", sqlState(err))
		}
		var foreign int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM "+qualified(f.callerSchema, "state")+" WHERE tenant='scope-b'").Scan(&foreign); err != nil {
			t.Fatalf("follow-up query invalid: SQLSTATE %s", sqlState(err))
		}
		if scope != "scope-b" || foreign != 1 {
			t.Fatalf("hypothesis not reproduced: changed=%v foreign=%d", scope == "scope-b", foreign)
		}
		r.Checks = append(r.Checks, boundaryCheck{Name: "revoke_set_userset", Escaped: true, Effect: "REVOKE SET does not prevent custom USERSET scope tampering; foreign row remains readable"})
	})

	t.Run("definer_cannot_elevate_set_role", func(t *testing.T) {
		name := qualified(f.callerSchema, "try_jobs_role")
		ddl := "CREATE FUNCTION " + name + "() RETURNS void LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog AS $s0$ BEGIN EXECUTE " + quoteLiteral("SET LOCAL ROLE "+ident(f.jobs)) + "; END $s0$"
		for _, statement := range []string{
			ddl,
			"ALTER FUNCTION " + name + "() OWNER TO " + ident(f.owner),
			"REVOKE ALL ON FUNCTION " + name + "() FROM PUBLIC",
			"GRANT EXECUTE ON FUNCTION " + name + "() TO " + ident(f.caller),
		} {
			if _, err := f.admin.Exec(ctx, statement); err != nil {
				t.Fatalf("definer fixture setup: SQLSTATE %s", sqlState(err))
			}
		}
		tx, err := f.beginCaller(ctx, "scope-a")
		if err != nil {
			t.Fatalf("begin caller: SQLSTATE %s", sqlState(err))
		}
		defer tx.Rollback(context.Background())
		_, err = tx.Exec(ctx, "SELECT "+name+"()")
		if state := sqlState(err); state != "42501" {
			t.Fatalf("expected permission refusal 42501, got %s", state)
		}
		r.Checks = append(r.Checks, boundaryCheck{Name: "definer_set_role", Effect: "SECURITY DEFINER helper cannot perform SET ROLE", SQLState: "42501"})
	})

	t.Run("removing_set_membership_denies_trusted_role_switch_too", func(t *testing.T) {
		if _, err := f.admin.Exec(ctx, "REVOKE "+ident(f.jobs)+" FROM "+ident(f.login)); err != nil {
			t.Fatalf("remove SET membership precondition: SQLSTATE %s", sqlState(err))
		}
		for _, site := range []string{"caller_raw_exec", "provider_direct_exec_same_tx"} {
			tx, err := f.beginCaller(ctx, "scope-a")
			if err != nil {
				t.Fatalf("begin %s: SQLSTATE %s", site, sqlState(err))
			}
			_, err = tx.Exec(ctx, "SET LOCAL ROLE "+ident(f.jobs))
			state := sqlState(err)
			rollbackErr := tx.Rollback(context.Background())
			if state != "42501" || rollbackErr != nil {
				t.Fatalf("%s expected 42501 with successful rollback, got %s rollback=%s", site, state, sqlState(rollbackErr))
			}
			r.Checks = append(r.Checks, boundaryCheck{Name: site + "_no_membership", Effect: "ordinary same-session SET ROLE denied regardless of Go call site", SQLState: state})
		}
	})

	if t.Failed() {
		r.Classification = "inconclusive-fixture-or-hypothesis-error"
	}
	r.Ended = time.Now().UTC()
	writeReport(t, r)
}

func quoteLiteral(value string) string {
	// The input is generated identifiers/DDL, but still quote SQL literals fully.
	out := "'"
	for _, c := range value {
		if c == '\'' {
			out += "''"
		} else {
			out += string(c)
		}
	}
	return fmt.Sprint(out, "'")
}
