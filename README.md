# jsd-solver-go

## Install

```bash
go get github.com/fxnatic/jsd-solver-go
```

## Usage (full auto)

This flow:
- requests the target URL to extract `__CF$cv$params` (`r` and `t`)
- downloads the JSD script and deobfuscates it to extract the needed data
- posts the oneshot payload and returns cookies/body/status

```go
package main

import (
	"fmt"
	"log"

	"github.com/fxnatic/jsd-solver-go/solver"
)

func main() {
	s, err := solver.NewSolver("https://www.example.com", false)
	if err != nil {
		log.Fatal(err)
	}

	res, err := s.Solve()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("status:", res.StatusCode)
	fmt.Println("success:", res.Success)
	fmt.Println("cf_clearance:", res.CfClearance)
}
```

## Usage (skip homepage fetch; provide `r` and `t`)

If you don’t want this library to do the initial homepage fetch, you can provide the `r` and `t` values yourself.

You must supply:
- `R` and `T` (from the challenged page’s `__CF$cv$params`)
- optionally `ScriptURL` (otherwise the default `.../jsd/main.js` is used)
- optionally `Cookies`

```go
data := solver.SolveData{
	R:         "…",
	T:         "…",
	ScriptURL: "https://example.com/cdn-cgi/challenge-platform/scripts/jsd/main.js", // optional
	Cookies:   cookiesFromYourOwnFetch,                                            // optional
}

res, err := s.SolveFromData(data)
```

## Compatibility

Cloudflare changes the JSD script over time and may serve different variants per site. Because of that:
- **this cannot be guaranteed to work for all sites**
- when it breaks, it’s usually due to script changes requiring updates to the deobfuscation/extraction logic
