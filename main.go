// Command task029-hll runs the HyperLogLog cardinality estimation service.
//
// Use --smoke-test to run the built-in self-check, which exits the process on
// completion. Otherwise it serves the HTTP API with `server --addr :8080`.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"task029-hll/internal/httpapi"
	"task029-hll/internal/selfcheck"
)

func main() {
	var (
		smokeTest bool
		addr      string
	)
	flag.BoolVar(&smokeTest, "smoke-test", false, "run the self-check and exit")
	flag.StringVar(&addr, "addr", ":8080", "HTTP listen address (used with the implicit server command)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  %s --smoke-test                 run self-check and exit\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s server --addr :8080          start the HTTP server\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	if smokeTest {
		if err := selfcheck.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "smoke-test FAILED: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("smoke-test PASSED")
		return
	}

	// Support an optional "server" subcommand for explicitness, while still
	// allowing bare invocation with flags only.
	args := flag.Args()
	if len(args) > 0 && args[0] == "server" {
		// addr already parsed via flag; remaining args ignored.
	} else if len(args) > 0 {
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", args[0])
		os.Exit(2)
	}

	srv := httpapi.New()
	hs := &http.Server{
		Addr:    addr,
		Handler: srv.Handler(),
	}
	log.Printf("task029-hll listening on %s", addr)
	if err := hs.ListenAndServe(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
