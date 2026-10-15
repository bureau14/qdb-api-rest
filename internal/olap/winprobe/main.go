// Command winprobe is a throwaway diagnostic for the silent death of the
// olap test binaries on Windows. Without arguments it runs itself once
// per stage, each stage in its own process, and prints every child's
// output and full exit code. "run <exe> [args...]" runs any command the
// same way.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/duckdb/duckdb-go/v2"
)

const fullDSN = "?max_temp_directory_size=0b&memory_limit=1073741824b&temp_directory=&threads=4"

const secret = "CREATE TEMPORARY SECRET qdb_rest (TYPE quasar, HOST '127.0.0.1', PORT '2836')"

const attach = "ATTACH 'secret=qdb_rest' AS qdb (TYPE quasar)"

type stage struct {
	dsn   string
	stmts []string
	// lock runs the stage on a goroutine locked to a fresh OS thread.
	lock bool
}

var stages = map[string]stage{
	"bare-select":       {"", []string{"SELECT 42"}, false},
	"bare-error":        {"", []string{"SELECT * FROM no_such_table"}, false},
	"bare-parse-error":  {"", []string{"SELEC"}, false},
	"bare-extensions":   {"", []string{"SELECT extension_name, loaded, installed FROM duckdb_extensions() WHERE loaded"}, false},
	"dsn-select":        {fullDSN, []string{"SELECT 42"}, false},
	"dsn-notemp-select": {"?max_temp_directory_size=0b&memory_limit=1073741824b&threads=4", []string{"SELECT 42"}, false},
	"bare-secret":       {"", []string{secret}, false},
	"bare-attach":       {"", []string{secret, attach}, false},
	"bare-attach-show":  {"", []string{secret, attach, "SELECT database_name, type FROM duckdb_databases()"}, false},
	"dsn-attach":        {fullDSN, []string{secret, attach}, false},
	"t1-attach":         {"?threads=1", []string{secret, attach}, false},
	"bare-attach-lock":  {"", []string{secret, attach}, true},
}

var order = []string{
	"bare-select", "bare-error", "bare-parse-error", "bare-extensions",
	"dsn-select", "dsn-notemp-select", "bare-secret", "bare-attach",
	"bare-attach-show", "dsn-attach", "t1-attach", "bare-attach-lock",
}

func say(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "winprobe: "+format+"\n", a...)
}

func runStage(name string) error {
	s, ok := stages[name]
	if !ok {
		return fmt.Errorf("unknown stage %q", name)
	}
	if s.lock {
		done := make(chan error)
		go func() {
			runtime.LockOSThread()
			done <- exec1(s)
		}()
		return <-done
	}
	return exec1(s)
}

func exec1(s stage) error {
	say("open connector dsn=%q", s.dsn)
	c, err := duckdb.NewConnector(s.dsn, nil)
	if err != nil {
		return err
	}
	say("connector open")
	db := sql.OpenDB(c)
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)
	for _, q := range s.stmts {
		say("query %q", q)
		rows, err := db.QueryContext(context.Background(), q)
		if err != nil {
			say("query error: %v", err)
			continue
		}
		cols, _ := rows.Columns()
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		for rows.Next() {
			if err := rows.Scan(ptrs...); err != nil {
				say("scan error: %v", err)
				break
			}
			say("row %v", vals)
		}
		say("rows done, err=%v", rows.Err())
		_ = rows.Close()
	}
	say("closing")
	return nil
}

func child(args ...string) {
	say("=== RUN %v", args)
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "GOTRACEBACK=all")
	err := cmd.Run()
	code := uint32(0)
	if cmd.ProcessState != nil {
		code = uint32(cmd.ProcessState.ExitCode())
	}
	say("=== END %v exit=0x%08X err=%v", args, code, err)
}

func main() {
	switch {
	case len(os.Args) == 1:
		self, err := os.Executable()
		if err != nil {
			say("%v", err)
			os.Exit(1)
		}
		for _, name := range order {
			child(self, "stage", name)
		}
	case os.Args[1] == "run":
		child(os.Args[2:]...)
	case os.Args[1] == "stage":
		if err := runStage(os.Args[2]); err != nil {
			say("stage error: %v", err)
			os.Exit(3)
		}
		say("stage ok")
	}
}
