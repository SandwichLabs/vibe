// Command macosprobe is spike #43 only (branch spike/43-macos, never main). It loads DuckDB core
// extensions through the same code path the product uses (session.OpenDB + EnsureExtension), so the
// macOS spike can sign this binary three ways and compare.
//
//	macosprobe ext <extension_directory> <httpfs-test-url>
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/SandwichLabs/vibe/internal/session"
)

func main() {
	if len(os.Args) < 4 || os.Args[1] != "ext" {
		fmt.Fprintln(os.Stderr, "usage: macosprobe ext <dir> <csv-url>")
		os.Exit(2)
	}
	dir, url := os.Args[2], os.Args[3]
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	db, err := session.OpenDB("", session.WithExtensionDirectory(dir))
	if err != nil {
		fmt.Println("open failed:", err)
		os.Exit(1)
	}
	defer db.Close() //nolint:errcheck
	db.SetMaxOpenConns(1)
	bad := 0
	for _, e := range []string{"excel", "httpfs", "postgres"} {
		if err := session.EnsureExtension(ctx, db, e, "spike"); err != nil {
			fmt.Printf("EXT %s: FAIL %v\n", e, err)
			bad++
			continue
		}
		var loaded bool
		_ = db.QueryRowContext(ctx, "SELECT loaded FROM duckdb_extensions() WHERE extension_name = ?", e).Scan(&loaded)
		fmt.Printf("EXT %s: ok loaded=%v\n", e, loaded)
	}
	var n int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM read_csv('"+url+"')").Scan(&n); err != nil {
		fmt.Printf("HTTPFS read_csv(%s): FAIL %v\n", url, err)
		bad++
	} else {
		fmt.Printf("HTTPFS read_csv: ok rows=%d\n", n)
	}
	if bad > 0 {
		os.Exit(1)
	}
}
