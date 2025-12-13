package solver

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"time"

	http "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
	"github.com/iancoleman/orderedmap"
	fastgen "github.com/t14raptor/go-fast/generator"
	"github.com/t14raptor/go-fast/parser"

	"github.com/fxnatic/jsd-solver-go/utils"
	"github.com/fxnatic/jsd-solver-go/visitors"
)

type OneshotSolver struct {
	client     tls_client.HttpClient
	targetURL  string
	scriptURL  string
	debug      bool
	lzAlphabet string
}

type ChallengeParams struct {
	R       string
	T       string
	Sitekey string
	Path    string
}

// SolveData lets callers skip the initial homepage request.
//
// You provide the `r` and `t` values (from the `__CF$cv$params` block) and optionally the script URL.
// The solver will still fetch + deobfuscate the script to extract sitekey/path and the LZ alphabet.
type SolveData struct {
	R         string
	T         string
	ScriptURL string
	Cookies   []*http.Cookie
}

func NewSolver(targetURL string, debug bool) (*OneshotSolver, error) {
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
		return nil, fmt.Errorf("failed to create tls client: %w", err)
	}

	return &OneshotSolver{
		client:    client,
		targetURL: strings.TrimSuffix(targetURL, "/"),
		debug:     debug,
	}, nil
}

func NewSolverWithClient(client tls_client.HttpClient, targetURL string, debug bool) (*OneshotSolver, error) {
	return &OneshotSolver{
		client:    client,
		targetURL: strings.TrimSuffix(targetURL, "/"),
		debug:     debug,
	}, nil
}

func (s *OneshotSolver) Solve() (*SolveResult, error) {
	params, cookies, err := s.fetchChallengeParams()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch challenge params: %w", err)
	}

	if s.scriptURL == "" {
		origin := originFromURL(s.targetURL)
		s.scriptURL = fmt.Sprintf("%s/cdn-cgi/challenge-platform/scripts/jsd/main.js", origin)
	}

	err = s.parseScript(params)
	if err != nil {
		return nil, fmt.Errorf("failed to parse script: %w", err)
	}

	result, err := s.sendOneshot(params, cookies)
	if err != nil {
		return nil, fmt.Errorf("failed to send oneshot: %w", err)
	}

	return result, nil
}

// SolveFromData sends the JSD oneshot request using caller-provided `r`/`t` (and optional cookies),
// while still fetching/deobfuscating the JSD script to extract the remaining values.
func (s *OneshotSolver) SolveFromData(data SolveData) (*SolveResult, error) {
	if data.R == "" || data.T == "" {
		return nil, fmt.Errorf("missing required challenge data (need R and T)")
	}

	params := &ChallengeParams{
		R: data.R,
		T: data.T,
	}

	if data.ScriptURL != "" {
		s.scriptURL = data.ScriptURL
	}

	if s.scriptURL == "" {
		origin := originFromURL(s.targetURL)
		s.scriptURL = fmt.Sprintf("%s/cdn-cgi/challenge-platform/scripts/jsd/main.js", origin)
	}

	if err := s.parseScript(params); err != nil {
		return nil, fmt.Errorf("failed to parse script: %w", err)
	}

	return s.sendOneshot(params, data.Cookies)
}

type SolveResult struct {
	Success     bool
	StatusCode  int
	Body        string
	Cookies     []*http.Cookie
	CfClearance string
}

func (s *OneshotSolver) fetchChallengeParams() (*ChallengeParams, []*http.Cookie, error) {
	req, err := http.NewRequest("GET", s.targetURL, nil)
	if err != nil {
		return nil, nil, err
	}

	s.setHeaders(req)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}

	bodyStr := string(body)

	params := &ChallengeParams{}

	cfParamsRe := regexp.MustCompile(`__CF\$cv\$params\s*=\s*\{([^}]*(?:r\s*:\s*['"][^'"]*['"])[^}]*)\}`)
	cfMatch := cfParamsRe.FindStringSubmatch(bodyStr)

	var paramsBlock string
	if len(cfMatch) > 1 {
		paramsBlock = cfMatch[1]
	} else {
		lineRe := regexp.MustCompile(`__CF\$cv\$params\s*=\s*\{[^}]+\}`)
		lineMatch := lineRe.FindString(bodyStr)
		if lineMatch != "" {
			paramsBlock = lineMatch
		} else {
			return nil, nil, fmt.Errorf("could not find __CF$cv$params block in response")
		}
	}

	rRe := regexp.MustCompile(`\br\s*:\s*['"]([a-fA-F0-9]+)['"]`)
	if m := rRe.FindStringSubmatch(paramsBlock); len(m) > 1 {
		params.R = m[1]
	}

	tRe := regexp.MustCompile(`\bt\s*:\s*['"]([A-Za-z0-9+/=]+)['"]`)
	if m := tRe.FindStringSubmatch(paramsBlock); len(m) > 1 {
		params.T = m[1]
	}

	if params.R == "" || params.T == "" {
		return nil, nil, fmt.Errorf("could not find __CF$cv$params in response (r=%s, t=%s, block=%s)", params.R, params.T, paramsBlock)
	}

	return params, resp.Cookies(), nil
}

func (s *OneshotSolver) parseScript(params *ChallengeParams) error {
	req, err := http.NewRequest("GET", s.scriptURL, nil)
	if err != nil {
		return err
	}

	req.Header = http.Header{
		"sec-ch-ua-platform":        {`"Windows"`},
		"user-agent":                {"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/143.0.0.0 Safari/537.36"},
		"sec-ch-ua":                 {`"Google Chrome";v="143", "Chromium";v="143", "Not A(Brand";v="24"`},
		"sec-ch-ua-mobile":          {"?0"},
		"upgrade-insecure-requests": {"1"},
		"accept":                    {"*/*"},
		"sec-fetch-site":            {"same-origin"},
		"sec-fetch-mode":            {"no-cors"},
		"sec-fetch-dest":            {"script"},
		"accept-encoding":           {"gzip, deflate, br, zstd"},
		"accept-language":           {"en-US,en;q=0.9"},
		http.HeaderOrderKey: {
			"sec-ch-ua",
			"sec-ch-ua-mobile",
			"sec-ch-ua-platform",
			"upgrade-insecure-requests",
			"user-agent",
			"accept",
			"sec-fetch-site",
			"sec-fetch-mode",
			"sec-fetch-user",
			"sec-fetch-dest",
			"accept-encoding",
			"accept-language",
			"cookie",
		},
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read script body: %w", err)
	}

	// os.WriteFile("script.js", []byte(body), 0644)
	script := string(body)

	deobf, result, err := deobfuscateScript(script)
	if err != nil {
		return fmt.Errorf("deobfuscation failed: %w", err)
	}

	script = deobf
	s.lzAlphabet = result.LZAlphabet

	// os.WriteFile("deobf.js", []byte(deobf), 0644)

	sitekeyRe := regexp.MustCompile(`xkKZ4:\s*['"]([^'"]+)['"]`)
	if m := sitekeyRe.FindStringSubmatch(script); len(m) > 1 {
		params.Sitekey = m[1]
	}

	pathRe := regexp.MustCompile(`/jsd/oneshot/([^'",\)]+)`)
	if m := pathRe.FindStringSubmatch(script); len(m) > 1 {
		params.Path = m[1]
	}

	if params.Path == "" {
		tableRe := regexp.MustCompile(`['"]([^'"]{500,})['"]\.split\(['"],['"]`)
		if m := tableRe.FindStringSubmatch(script); len(m) > 1 {
			table := strings.Split(m[1], ",")
			for _, s := range table {
				if strings.HasPrefix(s, "/jsd/oneshot/") {
					params.Path = strings.TrimPrefix(s, "/jsd/oneshot/")
					break
				}
			}
		}
	}

	if params.Sitekey == "" {
		return fmt.Errorf("could not extract sitekey from script")
	}

	if params.Path == "" {
		return fmt.Errorf("could not extract oneshot path from script")
	}

	return nil
}

func (s *OneshotSolver) sendOneshot(params *ChallengeParams, cookies []*http.Cookie) (*SolveResult, error) {
	ts, err := decodeTimestamp(params.T)
	if err != nil {
		ts = time.Now().Unix()
	}

	fingerprint := generateFingerprint(s.targetURL)

	payload := orderedmap.New()
	payload.Set("t", ts)
	payload.Set("lhr", "about:blank")
	payload.Set("api", false)
	payload.Set("payload", fingerprint)

	jsonData, err := payload.MarshalJSON()
	if err != nil {
		return nil, err
	}

	lz := utils.NewLZString(s.lzAlphabet)
	compressed := lz.CompressToBase64(string(jsonData))

	endpoint := fmt.Sprintf("%s/cdn-cgi/challenge-platform/h/%s/jsd/oneshot/%s%s",
		originFromURL(s.targetURL), params.Sitekey, params.Path, params.R)

	req, err := http.NewRequest("POST", endpoint, strings.NewReader(compressed))
	if err != nil {
		return nil, err
	}

	for _, c := range cookies {
		req.AddCookie(c)
	}

	req.Header = http.Header{
		"sec-ch-ua-platform": {`"Windows"`},
		"user-agent":         {"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/143.0.0.0 Safari/537.36"},
		"sec-ch-ua":          {`"Google Chrome";v="143", "Chromium";v="143", "Not A(Brand";v="24"`},
		"content-type":       {"text/plain;charset=UTF-8"},
		"sec-ch-ua-mobile":   {"?0"},
		"accept":             {"*/*"},
		"origin":             {originFromURL(s.targetURL)},
		"sec-fetch-site":     {"same-origin"},
		"sec-fetch-mode":     {"cors"},
		"sec-fetch-dest":     {"empty"},
		"accept-encoding":    {"gzip, deflate, br, zstd"},
		"accept-language":    {"en-US,en;q=0.9"},
		"priority":           {"u=1, i"},
		http.HeaderOrderKey: {
			"content-length",
			"sec-ch-ua-platform",
			"user-agent",
			"sec-ch-ua",
			"content-type",
			"sec-ch-ua-mobile",
			"accept",
			"origin",
			"sec-fetch-site",
			"sec-fetch-mode",
			"sec-fetch-dest",
			"accept-encoding",
			"accept-language",
			"cookie",
			"priority",
		},
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))

	result := &SolveResult{
		StatusCode: resp.StatusCode,
		Body:       string(bodyBytes),
		Cookies:    resp.Cookies(),
		Success:    resp.StatusCode >= 200 && resp.StatusCode < 300,
	}

	for _, c := range resp.Cookies() {
		if c.Name == "cf_clearance" {
			result.CfClearance = c.Value
			result.Success = true
		}
	}

	return result, nil
}

func (s *OneshotSolver) setHeaders(req *http.Request) {
	req.Header = http.Header{
		"sec-ch-ua":                 {`"Google Chrome";v="143", "Chromium";v="143", "Not A(Brand";v="24"`},
		"sec-ch-ua-mobile":          {"?0"},
		"sec-ch-ua-platform":        {`"Windows"`},
		"upgrade-insecure-requests": {"1"},
		"user-agent":                {"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/143.0.0.0 Safari/537.36"},
		"accept":                    {"text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7"},
		"sec-fetch-site":            {"none"},
		"sec-fetch-mode":            {"navigate"},
		"sec-fetch-user":            {"?1"},
		"sec-fetch-dest":            {"document"},
		"accept-encoding":           {"gzip, deflate, br, zstd"},
		"accept-language":           {"en-US,en;q=0.9"},
		"priority":                  {"u=0, i"},
		http.HeaderOrderKey: {
			"sec-ch-ua",
			"sec-ch-ua-mobile",
			"sec-ch-ua-platform",
			"upgrade-insecure-requests",
			"user-agent",
			"accept",
			"sec-fetch-site",
			"sec-fetch-mode",
			"sec-fetch-user",
			"sec-fetch-dest",
			"accept-encoding",
			"accept-language",
			"cookie",
			"priority",
		},
	}
}

func decodeTimestamp(t string) (int64, error) {
	decoded, err := base64.StdEncoding.DecodeString(t)
	if err != nil {
		return 0, err
	}
	var ts int64
	fmt.Sscanf(string(decoded), "%d", &ts)
	return ts, nil
}

func originFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.Path = ""
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func deobfuscateScript(src string) (string, *visitors.DeobfuscateResult, error) {
	prog, err := parser.ParseFile(src)
	if err != nil {
		return "", nil, fmt.Errorf("parse error: %w", err)
	}

	result, err := visitors.DeobfuscateCf(prog)
	if err != nil {
		return "", nil, fmt.Errorf("deobfuscation error: %w", err)
	}

	code := fastgen.Generate(prog)
	return code, result, nil
}
