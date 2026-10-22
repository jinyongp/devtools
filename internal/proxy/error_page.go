package proxy

import (
	_ "embed"
	"fmt"
	"html/template"
	"mime"
	"net/http"
	"strconv"
	"strings"
)

//go:embed error_page.html
var proxyErrorHTML string

var proxyErrorTemplate = template.Must(template.New("proxy error").Parse(proxyErrorHTML))

type proxyErrorPage struct {
	Status         int
	Title          string
	Description    string
	Host           string
	Target         string
	Action         string
	CommandTitle   string
	CommandContext string
	Commands       string
	Note           string
}

var (
	routeNotFoundPage = proxyErrorPage{
		Status:         http.StatusNotFound,
		Title:          "Route not found",
		Description:    "This hostname is not available on this listener.",
		Action:         "Check the address and the routes configured for this listener. Use a hostname listed by devtools, then reload.",
		CommandTitle:   "Check routes and listeners",
		CommandContext: "Run on the devtools host.",
		Commands:       "devtools proxy list\ndevtools proxy status --all",
	}
	routeUnavailablePage = proxyErrorPage{
		Status:         http.StatusServiceUnavailable,
		Title:          "Route unavailable",
		Description:    "The proxy could not resolve a usable route for this hostname.",
		Action:         "Check for conflicting hostnames, missing port assignments, or invalid project configuration. Fix the reported route status, then reload.",
		CommandTitle:   "Check route status",
		CommandContext: "Run on the devtools host.",
		Commands:       "devtools proxy list\ndevtools proxy status --all",
	}
	routeNotSharedPage = proxyErrorPage{
		Status:         http.StatusForbidden,
		Title:          "Route is not shared",
		Description:    "This route is limited to local connections.",
		Action:         "Open the address on the devtools host. For access from another device, ask the owner to share the route and use its sharing URL.",
		CommandTitle:   "Check sharing",
		CommandContext: "Run on the devtools host.",
		Commands:       "devtools expose list",
	}
	exposureAddressMismatchPage = proxyErrorPage{
		Status:         http.StatusForbidden,
		Title:          "Sharing address mismatch",
		Description:    "This address is not valid for the shared route.",
		Action:         "Use the current sharing URL. The request hostname and TLS server name must match that address.",
		CommandTitle:   "Find the sharing URL",
		CommandContext: "Run on the devtools host.",
		Commands:       "devtools expose list",
	}
	exposureUnavailablePage = proxyErrorPage{
		Status:         http.StatusServiceUnavailable,
		Title:          "Shared route unavailable",
		Description:    "This shared route is inactive or no longer matches its source.",
		Action:         "Ask the owner to check the sharing session and route status on the devtools host. Restore the route or start a new sharing session as needed.",
		CommandTitle:   "Check shared routes",
		CommandContext: "Run on the devtools host.",
		Commands:       "devtools expose list\ndevtools proxy list",
	}
	backendUnavailablePage = proxyErrorPage{
		Status:         http.StatusBadGateway,
		Title:          "Backend unavailable",
		Description:    "No response from the backend.",
		Action:         "On the devtools host, start the server on the backend port. If it is already running, check its port and logs.",
		CommandTitle:   "Project commands",
		CommandContext: "Run from your project directory.",
		Commands:       "devtools command list\ndevtools run --proxy <command>",
		Note:           "Replace <command> with your server command.",
	}
)

func writeBackendUnavailable(response http.ResponseWriter, request *http.Request, target string) {
	page := backendUnavailablePage
	page.Target = target
	writeProxyError(response, request, page)
}

func writeProxyError(response http.ResponseWriter, request *http.Request, page proxyErrorPage) {
	page.Host = request.Host
	if page.Host == "" {
		page.Host = "(not provided)"
	}
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Add("Vary", "Accept")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	if !acceptsHTML(request) {
		var body strings.Builder
		fmt.Fprintf(&body, "%d %s\n\n%s\n\nHost: %s\n", page.Status, page.Title, page.Description, page.Host)
		if page.Target != "" {
			fmt.Fprintf(&body, "Backend (on devtools host): %s\n", page.Target)
		}
		fmt.Fprintf(&body, "\n%s\n\n%s\n%s\n%s\n", page.Action, page.CommandTitle, page.CommandContext, page.Commands)
		if page.Note != "" {
			fmt.Fprintln(&body, page.Note)
		}
		body.WriteString("\nRetry once the issue is resolved.")

		http.Error(response, body.String(), page.Status)
		return
	}
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	response.WriteHeader(page.Status)
	_ = proxyErrorTemplate.Execute(response, page)
}

func acceptsHTML(request *http.Request) bool {
	for _, header := range request.Header.Values("Accept") {
		for _, value := range strings.Split(header, ",") {
			mediaType, parameters, err := mime.ParseMediaType(value)
			if err != nil || mediaType != "text/html" {
				continue
			}
			if quality, ok := parameters["q"]; ok {
				q, err := strconv.ParseFloat(quality, 64)
				if err != nil || !(q > 0 && q <= 1) {
					continue
				}
			}
			return true
		}
	}
	return false
}
