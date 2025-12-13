package tests

import (
	"fmt"
	"testing"

	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
	"github.com/fxnatic/jsd-solver-go/solver"
)

func TestFlytap(t *testing.T) {
	targetURL := "https://www.flytap.com/en-us"
	debug := true
	jar := tls_client.NewCookieJar()

	options := []tls_client.HttpClientOption{
		tls_client.WithTimeoutSeconds(30),
		tls_client.WithClientProfile(profiles.Chrome_133),
		tls_client.WithCookieJar(jar),
		tls_client.WithRandomTLSExtensionOrder(),
		tls_client.WithDisableHttp3(),
	}

	client, err := tls_client.NewHttpClient(tls_client.NewNoopLogger(), options...)
	if err != nil {
		t.Fatalf("Failed to create tls client: %v", err)
	}

	s, err := solver.NewSolverWithClient(client, targetURL, debug)
	if err != nil {
		t.Fatalf("Failed to create solver: %v", err)
	}

	result, err := s.Solve()
	if err != nil {
		t.Fatalf("Failed to solve challenge: %v", err)
	}

	if result.CfClearance != "" {
		fmt.Printf("\nGot cf_clearance cookie:\n%s\n", result.CfClearance)
	}

	if debug && result.Body != "" {
		fmt.Printf("\nResponse Body:\n%s\n", result.Body)
	}

	if !result.Success {
		t.Errorf("Challenge solve failed with status %d", result.StatusCode)
	}
}
